# CR-00: roadmap bootstrap

- Closed: 2026-10-15
- Slices: none (documentation only)
- Milestone progress: M0 → 0/6 slices

## What shipped

The initial documentation set: roadmap with tracks and vertical slices, architecture overview and domain model with state machines, swimlane flows for the three main processes, nine Proposed ADRs, runbook skeletons for every known async boundary and external dependency, and this CR-round process.

## Verification results

Verified against the Shopanda repository (README and plugin authoring guide):

- In-module compile-time plugin model; registration in `cmd/api/register_plugins.go`.
- Plugin capabilities: owned tables, public HTTP routes, events, cart hooks, checkout steps, extension fields (cart → order snapshot), permissions, admin config, CLI commands.
- Import boundary enforced by test.
- PostgreSQL job queue with admin API and `jobs:*` CLI; cron scheduler; runtime modes `serve`/`worker`/`scheduler`/`dev`.
- Guest checkout, Stripe, S3 core plugins; media storage port; SSR storefront with UI slots.

Still to verify (owned by `F1`, `E2`): custom job type registration by plugins; plugin migration mechanism; cart-**line** granularity of extension fields; event names and synchronicity for order creation and payment confirmation; shipment update API for plugins.

## Tests added

None (no code). Test expectations are defined per slice in `ROADMAP.md §6` and per layer in `§7`.

## Flags / migrations / rollback

Not applicable.

## Documentation updated

- [x] `docs/README.md` created.
- [x] `docs/ROADMAP.md` created.
- [x] `docs/architecture/overview.md`, `domain-model.md`, `flows/design-to-order.md`, `flows/generation-pipeline.md`, `flows/checkout-snapshot-and-fulfillment.md` created.
- [x] `docs/adr/` index, template, ADR-0001…0009 created (all Proposed).
- [x] `docs/runbooks/` index, template, and ten runbooks created with `TBD (slice)` placeholders for commands that do not exist yet.
- [x] `docs/cr-rounds/` process created.
- [ ] `AGENTS.md` build/test commands — not applicable until `F0`/`F2`.

## Follow-ups filed

- `F0`: accept ADR-0001; confirm licensing implications of the fork; pin upstream tag.
- `F1`: resolve the verify-items above; fill the Verification section of ADR-0003.
- `G1`: fill ADR-0008 provider table; produce the evaluation report.
- `P0`: fill ADR-0009 provider/spec table; write `architecture/production-spec.md`.
- `C1`: create `architecture/api.md`.
