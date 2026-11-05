# ADR-0006: Separate status machines for `Design`, `GenerationJob`, and fulfilment

- **Status:** Proposed (to be Accepted at close of `C3`)
- **Date:** 2026-10-15
- **Related slices:** `C3`, `C4`, `C6`, `E4`, `P1`–`P4`
- **Related ADRs:** [ADR-0005](0005-immutable-order-design-snapshot.md)

## Context

The project description proposes one list of statuses (`draft`, `upload_received`, `queued`, `generating`, `validating`, `preview_ready`, `approved`, `failed`, `ordered`, `production_ready`) and asks the roadmap to refine it. That list mixes three lifecycles: what the customer's design is, what one generation attempt is doing, and what happens to an ordered artwork. Mixing them makes regeneration, retries, and fulfilment hard to model and test.

## Decision

Three independent enums, each enforced by domain code, each documented as a state diagram in [`../architecture/domain-model.md`](../architecture/domain-model.md):

| Aggregate | Statuses | Notes |
| --- | --- | --- |
| `Design` | `draft`, `generating`, `generation_failed`, `preview_ready`, `approved`, `ordered`, `expired` | Customer-facing. `upload_received` is not a status: the design simply has a `source_asset_id`. Readiness to generate is a predicate (`texts valid && photo attached && attempts < limit`), not a state. |
| `GenerationJob` | `queued`, `running`, `validating`, `succeeded`, `failed`, `cancelled` | One per attempt. `failed` carries `terminal_reason`. `validating` is kept as a distinct state because validation is a separate stage with its own failure codes. |
| `DesignVersion.production_status` | `pending`, `preparing`, `ready`, `failed` | Production artwork preparation for the approved version. Replaces `production_ready` on the design. |
| `FulfillmentRecord` | `pending`, `awaiting_artwork`, `awaiting_approval`, `submitting`, `submitted`, `submitted_manually`, `submission_failed`, `in_production`, `shipped`, `cancelled` | Per order snapshot. |

Rules:

1. Transitions are methods on the aggregate returning a domain error on an invalid transition; HTTP maps it to `409 invalid_transition`.
2. The `Design` status is updated in the same transaction as the job's terminal transition.
3. Status strings are stable API contract once a slice ships; renames require a new ADR and an expand/contract migration.
4. The customer-facing UI derives its copy from `Design.status` plus the latest job's `status`/`error_code`; it never sees job internals beyond that.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Single flat status as proposed | Cannot express "preview ready, regeneration in progress" or "ordered, production failed" without ambiguity |
| Event-sourced design | Excluded by `AGENTS.md` by default; status columns plus append-only versions and jobs give the needed history |

## Consequences

- Positive: each lifecycle is small and fully testable in isolation; regeneration is just another job; fulfilment can evolve without touching the design.
- Negative: three enums to keep in sync with docs. Mitigation: documentation checklist at CR-round closure; a test asserts the enum values match a fixture file that the docs reference.

## Compliance

- Table-driven unit tests over every `(from, command) → to | error` pair for all three machines.
- `golden` test comparing enum values against `docs/architecture/domain-model.md` fixture list.
