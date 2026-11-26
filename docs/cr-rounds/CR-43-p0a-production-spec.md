# CR-43: P0 production-spec document

- Status: document written; P0 not closed
- Closed: not closed (sample not ordered; no provider chosen)
- Slices: `P0` (document only)
- Milestone progress: P0 in progress

## What shipped

`docs/architecture/production-spec.md`: selection requirements, dated candidate comparison, empty `production_spec` mapping, sample plan with pass/fail checks and an empty result table, vendor questions, and a P2 identifier note. Gelato and Printful go to sample on Bella+Canvas 3001. Prodigi and Printify are excluded with reasons from the 2026-11-25 read. ADR-0009 fulfilment stays Proposed. No code. No printer numbers.

## Verification results

| Item | Result |
| --- | --- |
| `bash .github/scripts/check-product-docs-links.sh` | Exit 0 on 2026-11-26 |
| `SHOPANDA_PLATFORM=1 bash scripts/ci/docs-policy.sh` | Exit 0 on 2026-11-26 (`docs-policy OK`; product-owned manifest OK; metaldogs docs and reserved-slug checks OK) |

## Tests added

None. Docs only.

## Flags / migrations / rollback

None. Removing the document returns P0 to the previous gap. `production_spec` behaviour is unchanged.

## Documentation updated

- [x] `docs/ROADMAP.md`: P0 status line. Slice not closed.
- [x] `docs/architecture/production-spec.md` added.
- [x] ADR-0009: link under the fulfilment table and in the G5a section. Status stays Proposed. `docs/adr/README.md` unchanged.
- [ ] Architecture diagrams, API contract, runbooks, READMEs, `AGENTS.md`: not touched. No boundary, command, or flag changed.
- [x] Index row in this file's parent README.

## Follow-ups

- Order the sample. Fill the result table. Only then fill `production_spec` and the ADR-0009 fulfilment rows.
- Confirm every 2026-11-25 cell in the provider dashboard or with support. Source hosts were not in the research read, except `api.sandbox.prodigi.com`.
