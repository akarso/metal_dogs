# AGENTS.md

## Project context

Metal Dogs is a headless store for personalized, metal-themed dog apparel. The storefront guides a customer through a constrained design flow, while Shopanda remains responsible for standard commerce concerns such as catalog, cart, checkout, orders, and payments.

Read `docs/PROJECT_DESCRIPTION.md` before making architectural or product decisions. Treat it as the current source of truth, but verify assumptions about Shopanda, external AI APIs, and print-on-demand providers against their actual capabilities before implementing integrations.

Plan work from `docs/ROADMAP.md` (tracks, vertical slices, per-slice tests and safe-delivery rules) and, when closing a change-request round, run the documentation checklist in `docs/cr-rounds/README.md`.

### Remotes, branches, and upstream pin

| Remote | Repository | Role |
| --- | --- | --- |
| `origin` | `git@github.com:akarso/metal_dogs.git` | Product repo (push here) |
| `upstream` | `https://github.com/akarso/shopanda.git` | Shopanda; fetch and merge on planned upgrade slices only |

**Branch policy:** Metal Dogs is **main-only**. Do not maintain a Shopanda-style `dev` branch or run `scripts/promote-dev-to-main.sh` in this repo. Upstream may keep `dev` for its own working history; we only consume published `upstream/main` (or an explicit SHA) on upgrade slices.

Pinned commit and upgrade rules: `docs/UPSTREAM_PIN.md`, `docs/runbooks/shopanda-upgrade.md`. Never merge from a local Shopanda working tree. Ensure the clone is not shallow before upgrading (`git fetch --unshallow upstream` if needed).

Documentation model: product docs under `docs/` (roadmap, ADRs, CR rounds). Shopanda `docs/phase-*`, promote, and living-guide workflow apply **upstream only**; this tree keeps `docs/GOVULN_BASELINE.md` + stub `docs/guides` / `docs/diagrams` for CI and link honesty.

### Build and test commands

Commands match the pinned Shopanda tree (`go 1.26.0` in `go.mod`):

```bash
go build -o app ./cmd/api
./app setup          # DB connectivity, migrations, seed
./app dev            # HTTP + embedded worker + scheduler
./app help           # live CLI list

go test ./plugins/ -run ImportBoundary -count=1
go test ./plugins/metaldogs/... -count=1
bash .github/scripts/check-product-docs-links.sh
```

DSN-gated feature tests (skip when unset; required in CI `integration` job):

```bash
docker compose up -d postgres
# Default publish is 127.0.0.1:5432; if busy: SHOPANDA_DATABASE_HOST_PORT=5433 docker compose up -d postgres
export SHOPANDA_TEST_DSN='postgres://shopanda:changeme@127.0.0.1:5432/shopanda?sslmode=disable'
go test ./plugins/metaldogs/... -count=1
# Or the CI package set + canaries (needs jq):
bash .github/scripts/run-integration-tests.sh
```

Coverage (CI artifacts: no-DSN unit packages vs integration for adapter SQL):

```bash
go test ./plugins/metaldogs/ ./plugins/metaldogs/httpapi/ ./plugins/metaldogs/ports/ ./plugins/metaldogs/testutil/ \
  -count=1 -covermode=atomic -coverprofile=coverage-metaldogs-nodsn.out
go tool cover -func=coverage-metaldogs-nodsn.out
COVERPROFILE=coverage-integration.out bash .github/scripts/run-integration-tests.sh
```

Enable Metal Dogs (default off): `SHOPANDA_PLUGINS_METALDOGS_ENABLED=true` or `plugins.metaldogs.enabled: true`. Then `GET /api/customizer/enabled` (not `/healthz`).

Harness helpers: `plugins/metaldogs/testutil` (`OpenIsolated` pins `search_path` on every pool conn, `FakeClock`, `NewSeqGenerator`). Product `Clock` / `IDGenerator` are in `ports/`.

## Engineering principles

- Prefer a modular monolith. Do not introduce microservices without a concrete operational or scaling reason.
- Use hexagonal architecture where it protects domain logic from frameworks, persistence, queues, object storage, AI providers, and fulfillment providers.
- Keep the design as small as the current requirements allow. Avoid speculative abstractions, generic frameworks, and infrastructure added only for possible future use.
- Model business behavior explicitly. Use domain language such as `Template`, `Design`, `GenerationJob`, `Asset`, and `OrderDesignSnapshot`.
- Preserve approved and ordered designs as immutable snapshots. Later template or prompt changes must not affect an existing order.
- Keep preview assets separate from production-ready artwork.
- Reuse Shopanda capabilities instead of rebuilding commerce features locally.

## Module and dependency boundaries

Organize code by business capability, not by technical layer across the whole application. Likely modules include:

- `customizer`
- `generation`
- `assets`
- Shopanda integration
- fulfillment/POD integration

Each module should own its domain model and expose a small public API. Internal packages must remain internal to their module.

Within a module, dependencies should point inward:

```text
HTTP / CLI / worker adapters
            |
       application
            |
          domain
            ^
            |
database / queue / storage / provider adapters
```

- Domain code must not import HTTP frameworks, database drivers, queue clients, cloud SDKs, or provider SDKs.
- Application services coordinate use cases, transactions, ports, and domain objects.
- Define ports at the boundary that needs them; keep interfaces small and use-case-oriented.
- Adapters translate external models and errors into application concepts.
- Do not leak provider payloads, database records, or transport DTOs into the domain.
- Avoid a shared “common” dumping ground. Share only stable primitives with clear ownership.
- Cross-module writes should go through the owning module's public application API.

## Event-driven behavior

Use events when they represent meaningful facts and reduce coupling, especially for the asynchronous generation pipeline. Examples include:

- generation requested;
- source asset accepted;
- generation completed or failed;
- preview prepared;
- design approved;
- production artwork prepared;
- order design snapshotted.

Events are not mandatory for simple synchronous calls.

- Name events in the past tense and make them immutable.
- Distinguish domain events from integration events.
- Keep handlers idempotent because delivery may be repeated.
- Include stable identifiers, event version, occurrence time, and correlation/causation identifiers where useful.
- Do not put secrets, raw personal data, or image contents in event payloads.
- Publish external events reliably after the state change commits; use a transactional outbox when database and broker atomicity matters.
- Retry only transient failures, with bounded attempts and backoff. Route permanent failures to an observable terminal state.
- Preserve per-design ordering where state transitions require it.
- Do not introduce event sourcing or a message broker by default. In-process events and a database-backed job queue are valid first implementations.

Long-running image generation must not block an HTTP request. Create a job, return its identifier, and expose status. Polling is the default; add SSE or WebSockets only when justified.

## Domain and data rules

- Enforce valid status transitions in domain/application code, not only in handlers or UI.
- Use stable IDs and explicit value objects for concepts with validation or units.
- Store template, prompt, provider/model, and generation configuration versions needed to reproduce or diagnose a result.
- Make commands that can be retried idempotent, especially generation requests, approval, cart attachment, and fulfillment submission.
- Keep transaction boundaries explicit and as small as practical.
- Persist the exact approved artwork, relevant configuration, and checksum with the order snapshot.
- Treat object storage as private by default and expose assets through short-lived signed URLs where appropriate.
- Use UTC internally; format dates and times only at system boundaries.

## API conventions

- Keep HTTP handlers thin: parse, authenticate/authorize, validate transport input, invoke a use case, and map the result.
- Validate all input server-side even when the frontend also validates it.
- Return stable machine-readable error codes without exposing provider internals or sensitive data.
- Use explicit request/response DTOs. Do not serialize domain entities or persistence models directly.
- Apply upload limits and verify actual MIME type, dimensions, and file format rather than trusting names or extensions.
- Support safe retries with idempotency keys where duplicate requests could create cost or inconsistent state.
- Never expose AI, storage, payment, or fulfillment credentials to the frontend.

## Go conventions

When the Go application is introduced:

- Follow standard Go layout and idioms; prefer simple packages and composition.
- Accept `context.Context` as the first argument for I/O-bound operations and propagate cancellation.
- Wrap errors with useful context while preserving causes for `errors.Is`/`errors.As`.
- Avoid panics for expected runtime failures.
- Inject time, ID generation, and external dependencies when determinism matters.
- Keep constructors honest: required dependencies must be explicit.
- Prefer concrete types at call sites and interfaces at dependency boundaries.
- Run the repository's formatter, vet/linter, and test commands before completing a change.

## Testing

Every behavior change should be covered at the lowest useful level and at important system boundaries.

### Unit tests

- Test domain invariants, state transitions, value objects, policies, and application orchestration.
- Keep domain tests fast, deterministic, and free of network, filesystem, database, and wall-clock dependencies.
- Prefer table-driven tests where cases genuinely share behavior.
- Test observable behavior rather than private implementation details.
- Include unhappy paths, duplicate delivery, retries, cancellation, and invalid transitions where relevant.

### Feature tests

- Test complete use cases through a public boundary, usually HTTP or a worker entry point.
- Cover request validation, authorization, persistence, transaction behavior, response contracts, and event/job creation.
- Use real infrastructure compatible with production when behavior depends on it, especially PostgreSQL. Use fakes only at genuinely external boundaries such as AI, POD, and payment providers.
- Assert externally visible outcomes rather than internal call sequences.

### Contract and integration tests

- Add contract tests for Shopanda and provider adapters using sanitized fixtures captured from documented API behavior.
- Never call paid AI or fulfillment APIs in the default test suite.
- Keep a small, explicitly opt-in smoke suite for real external services if it becomes necessary.
- A bug fix should include a regression test that fails before the fix.

Do not weaken or delete a valid test merely to make a change pass.

## Security, privacy, and cost controls

- Treat uploaded photos as private user data.
- Strip EXIF metadata unless it is explicitly required.
- Define and enforce retention/deletion rules for source images and generated assets.
- Never log image contents, credentials, signed URLs, full provider payloads, or unnecessary personal data.
- Authorize access to every design, job, and asset, including guest-owned resources.
- Bound file sizes, image dimensions, processing time, concurrency, and regeneration count.
- Track generation attempts, latency, and cost when the provider makes that information available.
- Prevent duplicate paid generations caused by retries or concurrent requests.
- Do not claim that output is production-ready until provider-specific print requirements have been validated.

## Frontend

- Start with semantic HTML, CSS, and vanilla JavaScript as described in the project document.
- Keep the flow constrained and mobile-first; this is not a general-purpose graphics editor.
- Use progressive enhancement for camera capture and always retain gallery upload.
- Clearly distinguish source photo, generated artwork, product mockup, approved design, and production file.
- Add a frontend framework or canvas library only when a demonstrated interaction requires it.
- Meet baseline accessibility: keyboard operation, visible focus, labels, useful errors, and sufficient contrast.

## Change discipline

- Make focused changes that preserve module boundaries.
- Update documentation when behavior, API contracts, status transitions, architecture, or operational commands change.
- Add migrations for schema changes; never rely on manual production edits.
- Keep migrations backward-compatible when asynchronous workers or rolling deployments may run mixed versions.
- Add observability around asynchronous boundaries: structured logs, correlation IDs, job duration, attempts, and terminal failure reasons.
- Do not perform unrelated refactors in a feature or bug-fix change.
- Before adding a dependency, confirm that the standard library or existing dependency cannot reasonably solve the problem.

## Before considering work complete

- Relevant unit and feature tests pass.
- Formatting and static checks pass.
- Failure, retry, idempotency, privacy, and cost implications have been considered.
- Public contracts and documentation are updated.
- No secrets, generated artifacts, private photos, or local IDE files are included.
