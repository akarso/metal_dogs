# Architecture Decision Records

One file per decision, numbered sequentially. Statuses: `Proposed` → `Accepted` → (`Deprecated` | `Superseded by ADR-NNNN`). Accepted ADRs are not edited except to change status; write a new ADR to change the decision.

New ADRs are written when a slice closes with a decision (see "Docs" lines in [`../ROADMAP.md`](../ROADMAP.md)) and reviewed at the close of each CR round.

| ADR | Title | Status | Decided in slice |
| --- | --- | --- | --- |
| [0001](0001-metal-dogs-as-shopanda-external-plugin.md) | Metal Dogs backend is a Shopanda external plugin in an upstream-tracking fork | Accepted | F0 |
| [0002](0002-storefront-customizer-and-shopanda-checkout.md) | Custom vanilla-JS customizer; Shopanda SSR for cart and checkout | Proposed | S1 / E3 |
| [0003](0003-async-generation-pg-job-queue-polling.md) | Asynchronous generation on Shopanda's PostgreSQL job queue with HTTP polling | Proposed | F1 / C4 |
| [0004](0004-private-object-storage-signed-urls.md) | Private S3-compatible storage with short-lived signed URLs | Accepted | F4 |
| [0005](0005-immutable-order-design-snapshot.md) | Approval freezes a `DesignVersion`; orders carry an `OrderDesignSnapshot` | Proposed | C6 / E4 |
| [0006](0006-design-and-job-status-model.md) | Separate status machines for `Design`, `GenerationJob`, and fulfilment | Proposed | C3 |
| [0007](0007-guest-design-ownership-tokens.md) | Guest designs are protected by per-design capability tokens | Proposed | C3 |
| [0008](0008-image-generation-provider.md) | Single image-generation provider behind a port | Proposed (OpenAI candidate; exit gate open) | G1 |
| [0009](0009-pod-provider-and-catalog-modelling.md) | POD provider, production file specification, and catalog modelling | Proposed (provider TBD) | P0 / E1 |

Template: [`0000-template.md`](0000-template.md).
