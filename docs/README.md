# Metal Dogs documentation

This directory is the source of truth for product scope, architecture, decisions, and operations. Code follows these documents; when they diverge, fix the document in the same change.

## Map

| Path | What it holds | When it changes |
| --- | --- | --- |
| [`PROJECT_DESCRIPTION.md`](PROJECT_DESCRIPTION.md) | Product concept, principles, domain concepts, open questions | Product decisions |
| [`ROADMAP.md`](ROADMAP.md) | Tracks, vertical slices, milestones, dependency graph, definition of done | Every CR round |
| [`UPSTREAM_PIN.md`](UPSTREAM_PIN.md) | Pinned Shopanda commit + remotes | Every Shopanda upgrade slice |
| [`architecture/`](architecture/) | Context/container/module diagrams, domain model, state machines, swimlane flows | Any change to boundaries, contracts, or status transitions |
| [`adr/`](adr/) | Architecture Decision Records | A decision is taken, revisited, or superseded |
| [`runbooks/`](runbooks/) | Operational procedures for known failure modes and routine tasks | A new async boundary, external dependency, or incident class appears |
| [`cr-rounds/`](cr-rounds/) | Log of closed change-request rounds and the documentation checklist run at closure | Every CR round |

## Vocabulary

- **Track** — a logical stream of work owned by one capability (foundation, generation, customizer, storefront, commerce, fulfillment, trust/ops, expansion). Tracks run in parallel where dependencies allow.
- **Slice** — a vertical, independently deliverable increment. A slice must be mergeable and deployable without breaking the live site: it ships dark (flag off / route hidden / template unpublished) or it is additive.
- **CR round** — a closed change-request round: one or more slices reviewed, merged, and deployed together. Closing a round triggers the [documentation checklist](cr-rounds/README.md#closure-checklist).
- **Milestone** — a user-visible capability level reached when a defined set of slices is closed (see `ROADMAP.md`).

## Conventions

- Diagrams are Mermaid, inline in Markdown, so they diff and review with the text. Swimlanes use `sequenceDiagram` participants or `flowchart` subgraphs.
- ADRs are numbered `NNNN-kebab-title.md`, immutable once `Accepted`; supersede rather than edit.
- Runbooks follow [`runbooks/TEMPLATE.md`](runbooks/TEMPLATE.md): symptom, impact, diagnosis, remediation, verification, prevention.
- Status names in documentation must match the enums in code exactly (`snake_case`).
- Do not document commands that do not exist yet. `AGENTS.md` and the local-development runbook are updated when tooling lands.
- No secrets, signed URLs, customer photos, or provider payloads in any document.
