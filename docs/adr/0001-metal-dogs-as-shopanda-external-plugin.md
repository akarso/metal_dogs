# ADR-0001: Metal Dogs backend is a Shopanda external plugin in an upstream-tracking fork

- **Status:** Accepted
- **Date:** 2026-10-15
- **Accepted:** 2026-11-05 (slice `F0`)
- **Related slices:** `F0`, `F1`
- **Related ADRs:** [ADR-0002](0002-storefront-customizer-and-shopanda-checkout.md), [ADR-0003](0003-async-generation-pg-job-queue-polling.md)
- **Upstream pin:** see [`../UPSTREAM_PIN.md`](../UPSTREAM_PIN.md)

## Context

The project description names Shopanda as the commerce backend in headless mode and asks that commerce features not be rebuilt. Shopanda's plugin guide (verified at the F0 pin) states:

- extensions are **in-process Go plugins in the same Go module**, registered at **compile time** in `cmd/api/register_plugins.go`; there is no `.so` loading, marketplace, or hot reload;
- external plugins may own tables (scoped, via `Bootstrap.DB` / `plugin.Migrator`), register public HTTP routes, event handlers, cart hooks, checkout steps, extension fields, permissions, admin config, and CLI commands;
- external plugins must not import `internal/infrastructure` or `internal/interfaces` (enforced by a test), nor modify core schema or core logic;
- plugins import `internal/domain`, `internal/application`, `internal/platform` — Go's `internal` rule means that code must live under the Shopanda module tree.

Therefore a separate Go module cannot be a Shopanda plugin without either Shopanda publishing a public SDK module or a module-path trick that depends on an implementation detail of the Go toolchain.

## Decision

1. This repository (`github.com/akarso/metal_dogs`, git remote `origin`) is a **product fork of Shopanda**. The Shopanda repository is configured as the git remote `upstream` (`https://github.com/akarso/shopanda.git`) and pinned by **commit SHA** until upstream publishes tags (see [`UPSTREAM_PIN.md`](../UPSTREAM_PIN.md)). Upstream is merged deliberately, as its own slice, never automatically. Daily Shopanda work happens in the separate Shopanda clone; Metal Dogs never uses that working tree as a merge source.
2. The Go module path remains `github.com/akarso/shopanda` so plugins can import Shopanda `internal/...` packages. Product documentation lives only under this repo's `docs/` (Metal Dogs–owned). Upstream Shopanda docs are **not** vendored; read them at the pinned commit on GitHub.
3. All Metal Dogs backend code lives in `plugins/metaldogs/`. The only change to upstream files is the registration line in `cmd/api/register_plugins.go` (gated by `plugins.metaldogs.enabled`) and, if unavoidable, additions documented in `docs/architecture/overview.md` under "Upstream deviations".
4. The plugin is hexagonal per business module (`customizer`, `generation`, `commerce`, `fulfillment`) and consumes Shopanda only through `plugin.App` / `plugin.Bootstrap` and allowed import paths (`pkg/`, `pkg/pluginsdk`, `pkg/extapi`, `internal/domain|application|platform`).
5. The customizer storefront lives in `storefront/` in the same repository (see ADR-0002).
6. **Licensing:** the fork inherits Shopanda's **GPL-3.0** licence. Metal Dogs is operated as a self-hosted deployment of that GPL codebase plus product-specific plugin/UI; distribution of binaries or source to third parties must comply with GPL-3.0.

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Separate Go module, plugin imported via `replace` directive | Go forbids importing another module's `internal/` packages; blocked by the toolchain |
| Nested module with path `github.com/akarso/shopanda/plugins/metaldogs` to satisfy the `internal` path rule | Works by accident of the path-based rule, still requires a fork for `register_plugins.go`, and confuses tooling; rejected |
| Separate Go service talking to Shopanda over REST/GraphQL | Loses cart hooks, checkout steps, extension fields, the job queue, and transactional access to order events; adds a second deployable; contradicts "modular monolith" |
| Contribute Metal Dogs into Shopanda core | Product-specific; upstream would not accept and it would couple release cycles |
| Fork without `upstream` tracking | Loses security and compatibility fixes |
| Copy from a local Shopanda working tree | Couples Metal Dogs to an in-progress WIP checkout; breaks the third-party pin model |

## Consequences

- Positive: full access to Shopanda extension points and infrastructure; one binary to deploy; one database; tests run on the real platform.
- Negative: upstream upgrades require merges. Mitigation: minimal footprint outside `plugins/metaldogs`, contract tests on every extension point used, `runbooks/shopanda-upgrade.md`.
- Negative: GPL-3.0 obligations apply to the combined work when distributed.
- Follow-up (`F1` / ADR-0003): wire generation through Shopanda Phase 13 APIs — `plugin.Migrator` + namespaced migrations, and `RegisterManual` / `Enqueue` job types `integration.sync.metaldogs.*` (free-form `metaldogs.*` job types remain out of scope upstream).

## Compliance

- `go test ./plugins/ -run ImportBoundary` passes on the pinned commit.
- Deviations outside `plugins/metaldogs/`, `storefront/`, `docs/`, and allowlisted upstream files are listed in `docs/architecture/overview.md` and reviewed on every upstream upgrade.
