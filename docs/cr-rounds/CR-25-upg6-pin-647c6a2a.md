# CR-25: Shopanda pin `647c6a2a` (UPG6)

- Opened: 2026-11-20
- Pin: `647c6a2a78257743db42b25019fd02fdf376618c` (shopanda#642, PR-1592)
- Previous pin: `ad14bd3a…` (shopanda#640)
- `upstream/main` tip at fetch: `8546b0c3` (shopanda#643). Its tree equals this pin (second parent is `647c6a2a`).

Merge-base of this branch and the pin is `ad14bd3a`.

## Ticket 00 — pin `647c6a2a`

- Date: 2026-11-20

### Conflict resolution

| File | Kept |
| --- | --- |
| `AGENTS.md` | Metal Dogs. Upstream added one row to its plugin table (`WithJobID`). That table is not in the product file. Build/test commands did not change. |
| `PLUGINS.md` | Upstream sync-job wording; observability link stays a GitHub blob |
| `docs/guides/SYNC_JOB_OBSERVABILITY.md` | Deleted (modify/delete). Stub `docs/guides/README.md` stays |
| `docs/phase-17-product-packaging/ROADMAP.md` | Deleted (modify/delete) |
| `docs/phase-17-product-packaging/prs/PR-1592.md` | Deleted (new upstream PR note) |

No conflict under `internal/`, `pkg/`, or `cmd/api`. Those paths match the pin except the allowlisted product files `cmd/api/register_products.go` and `cmd/api/register_plugins_metaldogs_test.go`.

`check-upstream-deviations.sh` allowlist is unchanged (17 paths).

`WithJobID` is not called from Metal Dogs. Generate and prepare still build `plugin.NewSyncJobEnvelope` and call `queue.Enqueue`. Adoption is MIG3b.

### Upstream range `ad14bd3a..647c6a2a` touching files we use

Commits: `c044b1a6` (add), `11fbbb75` (Conflict resolve), `647c6a2a` (merge shopanda#642).

- `pkg/pluginsdk/sync.go`: `SyncJobs.Enqueue` takes optional `pluginsdk.WithJobID`. Existing three-argument calls still compile. On Conflict, Enqueue uses a bound `JobsPort` (same sync type: pending/processing is a no-op, terminal requeues, wrong type errors). Without `JobsPort`, Conflict stays `IsConflict`.
- `internal/platform/plugin/sync_job.go`: that resolve. Not a Metal Dogs edit.
- `internal/infrastructure/redis/job_queue.go`: duplicate enqueue returns Conflict. Product queue is Postgres; this file matches the pin.
- `PLUGINS.md`: sync-job row and the Host/Adapt symbol map mention `WithJobID`.

No change in this range to `plugin.App` registration, `plugin.Migrator`, cart, checkout, extension fields, media storage, events, or the shipment updater.
