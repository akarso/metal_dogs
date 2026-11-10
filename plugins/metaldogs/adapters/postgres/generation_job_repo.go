package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/akarso/shopanda/plugins/metaldogs/domain"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// GenerationJobRepo is a Postgres adapter for ports.GenerationJobRepository.
type GenerationJobRepo struct {
	db *sql.DB
}

// NewGenerationJobRepo returns a GenerationJobRepo backed by db.
func NewGenerationJobRepo(db *sql.DB) *GenerationJobRepo {
	return &GenerationJobRepo{db: db}
}

var _ ports.GenerationJobRepository = (*GenerationJobRepo)(nil)

const generationJobColumns = `
	id, design_id, attempt, status, idempotency_key, provider, model, prompt_version,
	parameters, artwork_asset_id, mockup_asset_id, validation_result, error_code, terminal_reason,
	cost, cost_currency, latency_ms, correlation_id, queued_at, started_at, finished_at,
	source_asset_id, template_version, texts_digest, claim_token
`

func (r *GenerationJobRepo) Insert(ctx context.Context, j domain.GenerationJob) error {
	return insertGenerationJob(ctx, r.db, j)
}

func (r *GenerationJobRepo) GetByID(ctx context.Context, id string) (domain.GenerationJob, bool, error) {
	if id == "" {
		return domain.GenerationJob{}, false, nil
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT `+generationJobColumns+`
		FROM metaldogs_generation_jobs WHERE id = $1
	`, id)
	return scanGenerationJob(row)
}

func (r *GenerationJobRepo) GetByIdempotency(ctx context.Context, designID, key string) (domain.GenerationJob, bool, error) {
	if designID == "" || key == "" {
		return domain.GenerationJob{}, false, nil
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT `+generationJobColumns+`
		FROM metaldogs_generation_jobs
		WHERE design_id = $1 AND idempotency_key = $2
	`, designID, key)
	return scanGenerationJob(row)
}

func (r *GenerationJobRepo) GetActiveByDesign(ctx context.Context, designID string) (domain.GenerationJob, bool, error) {
	if designID == "" {
		return domain.GenerationJob{}, false, nil
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT `+generationJobColumns+`
		FROM metaldogs_generation_jobs
		WHERE design_id = $1 AND status IN ('queued', 'running', 'validating')
		LIMIT 1
	`, designID)
	return scanGenerationJob(row)
}

func (r *GenerationJobRepo) BeginGenerating(ctx context.Context, j domain.GenerationJob, d domain.Design, sourceDeleteAfter time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metaldogs generation_jobs begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertGenerationJob(ctx, tx, j); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE metaldogs_designs
		SET status = $2, generation_attempts = $3, latest_job_id = $4,
		    updated_at = $5, expires_at = $6
		WHERE id = $1
		  AND status IN ('draft', 'generation_failed', 'preview_ready')
	`, d.ID, d.Status, d.GenerationAttempts, d.LatestJobID, d.UpdatedAt.UTC(), d.ExpiresAt.UTC())
	if err != nil {
		return fmt.Errorf("metaldogs designs mark generating: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ports.ErrDesignConflict
	}
	// Slide attached source retention on generate activity; missing source aborts.
	if d.SourceAssetID != "" && !sourceDeleteAfter.IsZero() {
		res, err := tx.ExecContext(ctx, `
			UPDATE metaldogs_assets SET delete_after = $2
			WHERE id = $1 AND deleted_at IS NULL AND type = 'source'
		`, d.SourceAssetID, sourceDeleteAfter.UTC())
		if err != nil {
			return fmt.Errorf("metaldogs source delete_after: %w", err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ports.ErrDesignConflict
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("metaldogs generation_jobs commit: %w", err)
	}
	return nil
}

func (r *GenerationJobRepo) ClaimForWork(ctx context.Context, id string, lease time.Duration) (domain.GenerationJob, error) {
	if id == "" {
		return domain.GenerationJob{}, ports.ErrGenerationJobStale
	}
	if lease <= 0 {
		lease = domain.DefaultClaimLease
	}
	secs := int64(lease.Seconds())
	if secs < 1 {
		secs = 1
	}
	// started_at and lease expiry both use database NOW() — one clock.
	row := r.db.QueryRowContext(ctx, `
		UPDATE metaldogs_generation_jobs
		SET status = 'running',
		    started_at = NOW(),
		    claim_token = claim_token + 1
		WHERE id = $1 AND (
			status IN ('queued', 'validating')
			OR (status = 'running' AND (started_at IS NULL OR started_at < NOW() - make_interval(secs => $2)))
		)
		RETURNING `+generationJobColumns+`
	`, id, secs)
	j, ok, err := scanGenerationJob(row)
	if err != nil {
		return domain.GenerationJob{}, err
	}
	if !ok {
		return domain.GenerationJob{}, ports.ErrGenerationJobStale
	}
	return j, nil
}

func (r *GenerationJobRepo) ClaimLeaseExpired(ctx context.Context, id string, lease time.Duration) (bool, error) {
	if id == "" {
		return false, nil
	}
	if lease <= 0 {
		lease = domain.DefaultClaimLease
	}
	secs := int64(lease.Seconds())
	if secs < 1 {
		secs = 1
	}
	var expired bool
	err := r.db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM metaldogs_generation_jobs
			WHERE id = $1 AND status = 'running'
			  AND (started_at IS NULL OR started_at < NOW() - make_interval(secs => $2))
		)
	`, id, secs).Scan(&expired)
	if err != nil {
		return false, fmt.Errorf("metaldogs claim lease expired: %w", err)
	}
	return expired, nil
}

func (r *GenerationJobRepo) InvalidateClaim(ctx context.Context, id string) error {
	return InvalidateClaimExec(ctx, r.db, id)
}

// claimFenceExec is *sql.DB / *sql.Tx for InvalidateClaimExec.
type claimFenceExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// InvalidateClaimExec bumps claim_token and clears started_at using db/tx.
// Used from the platform job-queue OnRequeued hook inside the re-pend transaction.
// Returns an error when an active product row exists but the UPDATE matched nothing.
func InvalidateClaimExec(ctx context.Context, db claimFenceExec, id string) error {
	if id == "" || db == nil {
		return nil
	}
	res, err := db.ExecContext(ctx, `
		UPDATE metaldogs_generation_jobs
		SET claim_token = claim_token + 1, started_at = NULL
		WHERE id = $1 AND status IN ('queued', 'running', 'validating')
	`, id)
	if err != nil {
		return fmt.Errorf("metaldogs invalidate claim: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("metaldogs invalidate claim rows: %w", err)
	}
	if n == 1 {
		return nil
	}
	var status string
	err = db.QueryRowContext(ctx, `
		SELECT status FROM metaldogs_generation_jobs WHERE id = $1
	`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		// Platform-only row (or product job already gone) — nothing to fence.
		return nil
	}
	if err != nil {
		return fmt.Errorf("metaldogs invalidate claim lookup: %w", err)
	}
	switch status {
	case "queued", "running", "validating":
		return fmt.Errorf("metaldogs invalidate claim: active job %s status=%s unmatched", id, status)
	default:
		return nil
	}
}

func (r *GenerationJobRepo) Update(ctx context.Context, j domain.GenerationJob, expectedStatus string) error {
	return updateGenerationJob(ctx, r.db, j, expectedStatus)
}

func (r *GenerationJobRepo) CompleteSuccess(ctx context.Context, j domain.GenerationJob, artwork domain.Asset, design domain.Design) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metaldogs generation complete begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Soft-delete prior live artwork for this design (unique owner_ref+type).
	if artwork.OwnerRef != "" {
		_, err := tx.ExecContext(ctx, `
			UPDATE metaldogs_assets SET deleted_at = $2
			WHERE owner_ref = $1 AND type = 'artwork' AND deleted_at IS NULL
		`, artwork.OwnerRef, design.UpdatedAt.UTC())
		if err != nil {
			return fmt.Errorf("metaldogs soft-delete prior artwork: %w", err)
		}
	}
	if err := insertAssetTx(ctx, tx, artwork); err != nil {
		return err
	}
	// Active-only: never overwrite a committed terminal job.
	if err := updateGenerationJobActiveOnly(ctx, tx, j); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE metaldogs_designs
		SET status = $2, updated_at = $3
		WHERE id = $1 AND status = 'generating'
	`, design.ID, design.Status, design.UpdatedAt.UTC())
	if err != nil {
		return fmt.Errorf("metaldogs designs preview_ready: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ports.ErrDesignConflict
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("metaldogs generation complete commit: %w", err)
	}
	return nil
}

func (r *GenerationJobRepo) FinalizeSuccess(ctx context.Context, j domain.GenerationJob, design domain.Design) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metaldogs generation finalize begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := updateGenerationJobActiveOnly(ctx, tx, j); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `
		UPDATE metaldogs_designs
		SET status = $2, updated_at = $3
		WHERE id = $1 AND status IN ('generating', 'preview_ready')
	`, design.ID, domain.DesignPreviewReady, design.UpdatedAt.UTC())
	if err != nil {
		return fmt.Errorf("metaldogs designs preview_ready: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ports.ErrDesignConflict
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("metaldogs generation finalize commit: %w", err)
	}
	return nil
}

func (r *GenerationJobRepo) CompleteFailure(ctx context.Context, j domain.GenerationJob, design domain.Design) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metaldogs generation fail begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Active-only: never overwrite a committed succeeded/cancelled row.
	if err := updateGenerationJobActiveOnly(ctx, tx, j); err != nil {
		return err
	}
	if design.ID != "" {
		_, err := tx.ExecContext(ctx, `
			UPDATE metaldogs_designs
			SET status = $2, updated_at = $3
			WHERE id = $1 AND status = 'generating'
		`, design.ID, domain.DesignGenerationFailed, design.UpdatedAt.UTC())
		if err != nil {
			return fmt.Errorf("metaldogs designs generation_failed: %w", err)
		}
		// Zero rows OK when design already left generating.
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("metaldogs generation fail commit: %w", err)
	}
	return nil
}

func (r *GenerationJobRepo) FailActive(ctx context.Context, id, designID, terminalReason, errorCode string, claimToken int64, at time.Time) error {
	if id == "" {
		return ports.ErrGenerationJobStale
	}
	now := at.UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("metaldogs generation fail-active begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `
		UPDATE metaldogs_generation_jobs SET
			status = 'failed',
			error_code = $2,
			terminal_reason = $3,
			finished_at = $4
		WHERE id = $1
		  AND status IN ('queued', 'running', 'validating')
		  AND artwork_asset_id IS NULL
		  AND claim_token = $5
	`, id, nullIfEmpty(errorCode), nullIfEmpty(terminalReason), now, claimToken)
	if err != nil {
		return fmt.Errorf("metaldogs generation fail-active job: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		var hasArt bool
		_ = tx.QueryRowContext(ctx, `
			SELECT artwork_asset_id IS NOT NULL FROM metaldogs_generation_jobs WHERE id = $1
		`, id).Scan(&hasArt)
		if hasArt {
			return ports.ErrGenerationJobHasArtwork
		}
		return ports.ErrGenerationJobStale
	}
	if designID != "" {
		_, err := tx.ExecContext(ctx, `
			UPDATE metaldogs_designs
			SET status = $2, updated_at = $3
			WHERE id = $1 AND status = 'generating'
		`, designID, domain.DesignGenerationFailed, now)
		if err != nil {
			return fmt.Errorf("metaldogs designs generation_failed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("metaldogs generation fail-active commit: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

type jobExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func insertGenerationJob(ctx context.Context, db jobExecer, j domain.GenerationJob) error {
	if j.ID == "" || j.DesignID == "" || j.Status == "" || j.Provider == "" || j.Attempt < 1 || j.QueuedAt.IsZero() {
		return fmt.Errorf("metaldogs generation_jobs: required fields missing")
	}
	params := j.ParametersJSON
	if len(params) == 0 {
		params = []byte("{}")
	}
	var idem any
	if j.IdempotencyKey != "" {
		idem = j.IdempotencyKey
	}
	var artwork, mockup, errCode, term, corr, currency, source, textsDigest any
	if j.ArtworkAssetID != "" {
		artwork = j.ArtworkAssetID
	}
	if j.MockupAssetID != "" {
		mockup = j.MockupAssetID
	}
	if j.ErrorCode != "" {
		errCode = j.ErrorCode
	}
	if j.TerminalReason != "" {
		term = j.TerminalReason
	}
	if j.CorrelationID != "" {
		corr = j.CorrelationID
	}
	if j.CostCurrency != "" {
		currency = j.CostCurrency
	}
	if j.SourceAssetID != "" {
		source = j.SourceAssetID
	}
	if j.TextsDigest != "" {
		textsDigest = j.TextsDigest
	}
	var validation any
	if len(j.ValidationJSON) > 0 {
		validation = j.ValidationJSON
	}
	var cost any
	if j.Cost != nil {
		cost = *j.Cost
	}
	var latency any
	if j.LatencyMS != nil {
		latency = *j.LatencyMS
	}
	var started, finished any
	if j.StartedAt != nil {
		started = j.StartedAt.UTC()
	}
	if j.FinishedAt != nil {
		finished = j.FinishedAt.UTC()
	}
	var tplVer any
	if j.TemplateVersion > 0 {
		tplVer = j.TemplateVersion
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO metaldogs_generation_jobs (
			id, design_id, attempt, status, idempotency_key, provider, model, prompt_version,
			parameters, artwork_asset_id, mockup_asset_id, validation_result, error_code, terminal_reason,
			cost, cost_currency, latency_ms, correlation_id, queued_at, started_at, finished_at,
			source_asset_id, template_version, texts_digest, claim_token
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,
			$9::jsonb,$10,$11,$12::jsonb,$13,$14,
			$15,$16,$17,$18,$19,$20,$21,
			$22,$23,$24,$25
		)
	`, j.ID, j.DesignID, j.Attempt, j.Status, idem, j.Provider, j.Model, j.PromptVersion,
		params, artwork, mockup, validation, errCode, term,
		cost, currency, latency, corr, j.QueuedAt.UTC(), started, finished,
		source, tplVer, textsDigest, j.ClaimToken,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ports.ErrGenerationJobConflict
		}
		return fmt.Errorf("metaldogs generation_jobs insert: %w", err)
	}
	return nil
}

// updateGenerationJobActiveOnly force-writes only while the row is still queued/running/validating.
func updateGenerationJobActiveOnly(ctx context.Context, db jobExecer, j domain.GenerationJob) error {
	params := j.ParametersJSON
	if len(params) == 0 {
		params = []byte("{}")
	}
	var artwork, mockup, errCode, term, currency any
	if j.ArtworkAssetID != "" {
		artwork = j.ArtworkAssetID
	}
	if j.MockupAssetID != "" {
		mockup = j.MockupAssetID
	}
	if j.ErrorCode != "" {
		errCode = j.ErrorCode
	}
	if j.TerminalReason != "" {
		term = j.TerminalReason
	}
	if j.CostCurrency != "" {
		currency = j.CostCurrency
	}
	var validation any
	if len(j.ValidationJSON) > 0 {
		validation = j.ValidationJSON
	}
	var cost any
	if j.Cost != nil {
		cost = *j.Cost
	}
	var latency any
	if j.LatencyMS != nil {
		latency = *j.LatencyMS
	}
	var finished any
	if j.FinishedAt != nil {
		finished = j.FinishedAt.UTC()
	}
	// Do not write started_at here — claim sets it via NOW(); rewriting a Go clock
	// value would skew the DB lease.
	res, err := db.ExecContext(ctx, `
		UPDATE metaldogs_generation_jobs SET
			status = $2, provider = $3, model = $4, prompt_version = $5, parameters = $6::jsonb,
			artwork_asset_id = $7, mockup_asset_id = $8, validation_result = $9::jsonb,
			error_code = $10, terminal_reason = $11, cost = $12, cost_currency = $13,
			latency_ms = $14, finished_at = $15
		WHERE id = $1
		  AND status IN ('queued', 'running', 'validating')
		  AND claim_token = $16
	`, j.ID, j.Status, j.Provider, j.Model, j.PromptVersion, params,
		artwork, mockup, validation, errCode, term, cost, currency, latency, finished,
		j.ClaimToken,
	)
	if err != nil {
		return fmt.Errorf("metaldogs generation_jobs active update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ports.ErrGenerationJobStale
	}
	return nil
}

// updateGenerationJob writes fields when expectedStatus and claim_token still match.
func updateGenerationJob(ctx context.Context, db jobExecer, j domain.GenerationJob, expectedStatus string) error {
	params := j.ParametersJSON
	if len(params) == 0 {
		params = []byte("{}")
	}
	var artwork, mockup, errCode, term, currency any
	if j.ArtworkAssetID != "" {
		artwork = j.ArtworkAssetID
	}
	if j.MockupAssetID != "" {
		mockup = j.MockupAssetID
	}
	if j.ErrorCode != "" {
		errCode = j.ErrorCode
	}
	if j.TerminalReason != "" {
		term = j.TerminalReason
	}
	if j.CostCurrency != "" {
		currency = j.CostCurrency
	}
	var validation any
	if len(j.ValidationJSON) > 0 {
		validation = j.ValidationJSON
	}
	var cost any
	if j.Cost != nil {
		cost = *j.Cost
	}
	var latency any
	if j.LatencyMS != nil {
		latency = *j.LatencyMS
	}
	var finished any
	if j.FinishedAt != nil {
		finished = j.FinishedAt.UTC()
	}

	if expectedStatus == "" {
		return updateGenerationJobActiveOnly(ctx, db, j)
	}
	res, err := db.ExecContext(ctx, `
		UPDATE metaldogs_generation_jobs SET
			status = $2, provider = $3, model = $4, prompt_version = $5, parameters = $6::jsonb,
			artwork_asset_id = $7, mockup_asset_id = $8, validation_result = $9::jsonb,
			error_code = $10, terminal_reason = $11, cost = $12, cost_currency = $13,
			latency_ms = $14, finished_at = $15
		WHERE id = $1 AND status = $16 AND claim_token = $17
	`, j.ID, j.Status, j.Provider, j.Model, j.PromptVersion, params,
		artwork, mockup, validation, errCode, term, cost, currency, latency, finished,
		expectedStatus, j.ClaimToken,
	)
	if err != nil {
		return fmt.Errorf("metaldogs generation_jobs update: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ports.ErrGenerationJobStale
	}
	return nil
}

func insertAssetTx(ctx context.Context, db jobExecer, a domain.Asset) error {
	if a.ID == "" || a.Type == "" || a.StorageKey == "" || a.MimeType == "" || a.Checksum == "" {
		return fmt.Errorf("metaldogs assets: id, type, storage_key, mime_type, checksum required")
	}
	if a.SizeBytes < 0 || a.Width <= 0 || a.Height <= 0 || a.DeleteAfter.IsZero() {
		return fmt.Errorf("metaldogs assets: size_bytes, width, height, delete_after required")
	}
	var owner any
	if a.OwnerRef != "" {
		owner = a.OwnerRef
	}
	_, err := db.ExecContext(ctx, `
		INSERT INTO metaldogs_assets (
			id, type, storage_key, mime_type, size_bytes, width, height, checksum, owner_ref,
			created_at, delete_after, deleted_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,
			COALESCE($10, now()), $11, NULL
		)
	`, a.ID, a.Type, a.StorageKey, a.MimeType, a.SizeBytes, a.Width, a.Height, a.Checksum, owner,
		nullTime(a.CreatedAt), a.DeleteAfter.UTC(),
	)
	if err != nil {
		return fmt.Errorf("metaldogs assets insert: %w", err)
	}
	return nil
}

type jobScanner interface {
	Scan(dest ...any) error
}

func scanGenerationJob(row jobScanner) (domain.GenerationJob, bool, error) {
	var j domain.GenerationJob
	var idem, model, prompt, artwork, mockup, errCode, term, currency, corr, source, textsDigest sql.NullString
	var params, validation []byte
	var cost sql.NullFloat64
	var latency, tplVer, claimToken sql.NullInt64
	var started, finished sql.NullTime
	err := row.Scan(
		&j.ID, &j.DesignID, &j.Attempt, &j.Status, &idem, &j.Provider, &model, &prompt,
		&params, &artwork, &mockup, &validation, &errCode, &term,
		&cost, &currency, &latency, &corr, &j.QueuedAt, &started, &finished,
		&source, &tplVer, &textsDigest, &claimToken,
	)
	if err == sql.ErrNoRows {
		return domain.GenerationJob{}, false, nil
	}
	if err != nil {
		return domain.GenerationJob{}, false, fmt.Errorf("metaldogs generation_jobs scan: %w", err)
	}
	if idem.Valid {
		j.IdempotencyKey = idem.String
	}
	if model.Valid {
		j.Model = model.String
	}
	if prompt.Valid {
		j.PromptVersion = prompt.String
	}
	if len(params) > 0 {
		j.ParametersJSON = append([]byte(nil), params...)
	}
	if artwork.Valid {
		j.ArtworkAssetID = artwork.String
	}
	if mockup.Valid {
		j.MockupAssetID = mockup.String
	}
	if len(validation) > 0 {
		j.ValidationJSON = append([]byte(nil), validation...)
	}
	if errCode.Valid {
		j.ErrorCode = errCode.String
	}
	if term.Valid {
		j.TerminalReason = term.String
	}
	if cost.Valid {
		v := cost.Float64
		j.Cost = &v
	}
	if currency.Valid {
		j.CostCurrency = currency.String
	}
	if latency.Valid {
		v := int(latency.Int64)
		j.LatencyMS = &v
	}
	if corr.Valid {
		j.CorrelationID = corr.String
	}
	if source.Valid {
		j.SourceAssetID = source.String
	}
	if tplVer.Valid {
		j.TemplateVersion = int(tplVer.Int64)
	}
	if textsDigest.Valid {
		j.TextsDigest = textsDigest.String
	}
	if claimToken.Valid {
		j.ClaimToken = claimToken.Int64
	}
	j.QueuedAt = j.QueuedAt.UTC()
	if started.Valid {
		t := started.Time.UTC()
		j.StartedAt = &t
	}
	if finished.Valid {
		t := finished.Time.UTC()
		j.FinishedAt = &t
	}
	return j, true, nil
}
