# CR-27: Shopanda pin `4bff865b` (UPG7)

- Opened: 2026-11-20
- Pin: `4bff865b…` (shopanda#644, PR-1593)
- Previous pin: `647c6a2a…` (shopanda#642)
- `upstream/main` tip at fetch: `8546b0c3` (shopanda#643). This pin is not an ancestor of that tip. Its tree differs. The SHA is on `upstream/dev` (merge of `pr-1593-jobsport-counts-by-type` onto `647c6a2a`).

Merge-base of this branch and the pin is `647c6a2a`.

## Ticket 00 — pin `4bff865b`

- Date: 2026-11-20

### Conflict resolution

| File | Kept |
| --- | --- |
| `AGENTS.md` | Metal Dogs. Upstream added `CountsByType` to its plugin-recipe table. That table is not in the product file. Build/test commands did not change. |
| `PLUGINS.md` | Upstream `JobsPort` row (`CountsByType`). Product GitHub blob links stay. |
| `docs/guides/SYNC_JOB_OBSERVABILITY.md` | Deleted (modify/delete). Stub `docs/guides/README.md` stays |
| `docs/phase-17-product-packaging/ROADMAP.md` | Deleted (modify/delete) |
| `docs/phase-17-product-packaging/prs/PR-1593.md` | Deleted (new upstream PR note) |

No conflict under `internal/`, `pkg/`, or `cmd/api`. Those paths match the pin except the allowlisted product files `cmd/api/register_products.go` and `cmd/api/register_plugins_metaldogs_test.go`.

`check-upstream-deviations.sh` allowlist is unchanged (17 paths).

`pluginsdk.JobsPort.CountsByType` is not called from Metal Dogs. Health still type-asserts the bound queue reader. Adoption is MIG3b.

### Upstream range `647c6a2a..4bff865b` touching files we use

Commits: `2155bb18` (expose), `ab0e4c31` (validate), `882a7b60` (empty-type tests), `19914286` (zero-count forward, BindJobsPort note), `4bff865b` (merge shopanda#644).

- `pkg/pluginsdk/jobs_port.go`: `JobsPort` gains `CountsByType(ctx, jobType)`. Empty or whitespace `jobType` returns an error before the Reader. The port forwards the Reader map and does not drop zero counts. Unscoped: any CapJobs holder may count any type.
- `pkg/pluginsdk/harness/stubs.go`: stub returns an empty map, or an error when the type is blank.
- `internal/platform/plugin/service_ports.go`: `BindJobsPort` comment only. Production still binds `pluginsdk.NewJobsPort`. A hand-rolled mock that misses a method satisfies the `any` argument and then fails the `Host.Jobs()` assert.
- `internal/platform/plugin/sync_job_test.go`: `conflictJobsPort` implements the new method and is asserted as `pluginsdk.JobsPort`. Not a Metal Dogs edit.
- `PLUGINS.md`: JobsPort row names `CountsByType`.

No change in this range to `plugin.App` registration, `plugin.Migrator`, cart, checkout, extension fields, media storage, events, or the shipment updater.
