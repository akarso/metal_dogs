# CR-28: Shopanda pin `d9ccc21c` (UPG8)

- Opened: 2026-11-21
- Pin: `d9ccc21c…` (shopanda#646, PR-1595)
- Previous pin: `4bff865b…` (shopanda#644)
- `upstream/main` tip at fetch: `79c3b59a` (shopanda#647). This pin is an ancestor of that tip. Their trees are equal.

Merge-base of this branch and the pin is `4bff865b`.

## Ticket 00 — pin `d9ccc21c`

- Date: 2026-11-21

### Conflict resolution

| File | Kept |
| --- | --- |
| `AGENTS.md` | Metal Dogs. Upstream updated two rows in its plugin-recipe table (CLI skip, WithJobID race). That table is not in the product file. Build/test commands did not change. |
| `PLUGINS.md` | Upstream sync-job wording (reclaiming, one-shot requeue race). Observability link stays a GitHub blob |
| `docs/guides/EXTENSION_POINTS.md` | Deleted (modify/delete). Stub `docs/guides/README.md` stays |
| `docs/phase-17-product-packaging/ROADMAP.md` | Deleted (modify/delete) |
| `docs/phase-17-product-packaging/prs/PR-1594.md` | Deleted (new upstream PR note) |
| `docs/phase-17-product-packaging/prs/PR-1595.md` | Deleted (new upstream PR note) |

No conflict under `internal/`, `pkg/`, or `cmd/api`. Those paths match the pin except the allowlisted product files `cmd/api/register_products.go` and `cmd/api/register_plugins_metaldogs_test.go`.

`check-upstream-deviations.sh` allowlist is unchanged (17 paths).

`WithJobID` is not called from Metal Dogs. `pluginsdk.JobsPort.CountsByType` is not called. Plugin CLI skip of `CapJobs` and `CapMedia` is the pin's `cmd/api/plugin_boot.go`. Adoption of `WithJobID` stays MIG3b.

### Upstream range `4bff865b..d9ccc21c` touching files we use

Commits: `f0b84270` (CLI skip), `eb081c72` (merge shopanda#645), `e6a07350` (requeue retry), `5619a26a` (godoc and pending observe), `d9ccc21c` (merge shopanda#646).

- `cmd/api/plugin_boot.go`: `pluginCLINeedsSkip` always adds `CapJobs` and `CapMedia`. Plugin CLI does not bind those ports; `Host.Jobs` and `Host.Media` stay unset there. `cmd/api/cli_plugins_test.go` covers the skip.
- `internal/platform/plugin/sync_job.go`: `WithJobID` Conflict resolve. In-flight is pending, processing, or reclaiming (success, payload discarded). Terminal requeue: `ErrRequeueNotTerminal` is success; `ErrRequeueWrongAttempt` reloads once and retries if still terminal, or succeeds if now in flight. A second `WrongAttempt` is a hard error. Not a Metal Dogs edit.
- `pkg/pluginsdk/sync.go`: `WithJobID` godoc matches that race handling. The option signature is unchanged.
- `PLUGINS.md`: sync-job row names reclaiming and the one-shot retry.

No change in this range to `plugin.App` registration, `plugin.Migrator`, cart, checkout, extension fields, media storage, events, or the shipment updater.
