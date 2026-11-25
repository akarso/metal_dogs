# Architecture Decision Records

One file per decision, numbered sequentially. Statuses: `Proposed` → `Accepted` → (`Deprecated` | `Superseded by ADR-NNNN`). Accepted ADRs are not edited except to change status; write a new ADR to change the decision.

New ADRs are written when a slice closes with a decision (see "Docs" lines in [`../ROADMAP.md`](../ROADMAP.md)) and reviewed at the close of each CR round.

| ADR | Title | Status | Decided in slice |
| --- | --- | --- | --- |
| [0001](0001-metal-dogs-as-shopanda-external-plugin.md) | Metal Dogs backend is a Shopanda external plugin in an upstream-tracking fork | Accepted | F0 |
| [0002](0002-storefront-customizer-and-shopanda-checkout.md) | Custom vanilla-JS customizer; Shopanda SSR for cart and checkout | Proposed | S1 / E3 |
| [0003](0003-async-generation-pg-job-queue-polling.md) | Asynchronous generation on Shopanda's PostgreSQL job queue with HTTP polling | Accepted | C4 |
| [0004](0004-private-object-storage-signed-urls.md) | Private S3-compatible storage with short-lived signed URLs | Accepted. E5 ([CR-35](../cr-rounds/CR-35-e5-artwork-access.md)) signs order artwork for 300s behind `order_snapshot.enabled` and audits each URL. E6a ([CR-37](../cr-rounds/CR-37-e6a-order-thumbnail.md)) serves thumbnail bytes at a stable HMAC URL; that route is the exception to the 15-minute rule. E6b ([CR-42](../cr-rounds/CR-42-e6b-order-thumbnail-display.md)) appends that URL on `order.line.display` when `public_base_url` is https | F4 / E5 / E6a / E6b |
| [0005](0005-immutable-order-design-snapshot.md) | Approval freezes a `DesignVersion`; orders carry an `OrderDesignSnapshot` | Accepted (DesignVersion / C6). Snapshot implemented in E4a; E4b binds `Host.Orders()` when the flag is on (flag still defaults off). E6a stores `mockup_asset_id` and `thumbnail_asset_id` on insert ([CR-37](../cr-rounds/CR-37-e6a-order-thumbnail.md)) | C6 / E4a / E4b / E6a |
| [0006](0006-design-and-job-status-model.md) | Separate status machines for `Design`, `GenerationJob`, and fulfilment | Accepted. G5a stores `production_failure_reason` on a failed version ([CR-36](../cr-rounds/CR-36-g5a-production-artwork.md)) | C3 / G5a |
| [0007](0007-guest-design-ownership-tokens.md) | Guest designs are protected by per-design capability tokens | Accepted | C3 |
| [0008](0008-image-generation-provider.md) | Single image-generation provider behind a port | Proposed (OpenAI candidate; exit gate open) | G1 |
| [0009](0009-pod-provider-and-catalog-modelling.md) | POD provider, production file specification, and catalog modelling | Accepted (catalog, E1 / CR-18); Proposed (provider until P0). G5a reads `production_spec` from config until those values land ([CR-36](../cr-rounds/CR-36-g5a-production-artwork.md)) | P0 / E1 / G5a |

Template: [`0000-template.md`](0000-template.md).
