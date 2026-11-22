# Flow: approval, checkout, order snapshot, fulfillment

Covers [ADR-0005](../../adr/0005-immutable-order-design-snapshot.md) (immutable snapshot) and track P. Shopanda owns cart, checkout, payment, and the order; the plugin attaches the design and reacts to the order.

## Swimlane

```mermaid
flowchart TB
    subgraph customizer[metaldogs customizer]
        a1[Approve: freeze DesignVersion with checksum]
        a2[Enqueue prepare_production_artwork — C6 stub fails pending_spec; Requeue until G5]
        a3[Add to cart: session cart, no cart id, trusted plugin write]
    end

    subgraph worker[Worker]
        w1[prepare_production_artwork: render to POD spec, checksum, store]
        w2[snapshot_order_design: copy version data per order line]
        w3[submit_fulfillment: send artwork + order to POD]
        w4[reconciliation sweep: orders with design lines and no snapshot]
    end

    subgraph shopanda[Shopanda core]
        s1[Cart line + extension metaldogs.design_id]
        s2[Cart hook: validate design]
        s3[Checkout step: re-validate designs]
        s4[Payment via Stripe]
        s5[Order created, immutable]
        s6[Event order.created]
        s7[Event payment.confirmed]
        s8[Shipment status and tracking]
    end

    subgraph commerce[metaldogs commerce integration]
        e1[On order.created: enqueue one snapshot job per event]
        e2[Design → ordered, retention switched to keep]
        e3[On payment.confirmed: fulfilment pending]
    end

    subgraph fulfillment[metaldogs fulfillment]
        f1{Production artwork ready?}
        f2{Manual gate?}
        f3[Operator approves in admin or CLI]
        f4[submitted with external order id]
        f5[Status sync: webhook or poll]
        f6[in_production → shipped]
    end

    subgraph pod[POD provider]
        p1[Accept order]
        p2[Print and ship]
        p3[Status webhooks]
    end

    a1 --> a2 --> w1
    a1 --> a3 --> s1 --> s2 --> s3 --> s4 --> s5 --> s6 --> e1 --> w2 --> e2
    s5 --> s7 --> e3 --> f1
    w1 --> f1
    f1 -- no --> f1
    f1 -- yes --> f2
    f2 -- yes --> f3 --> w3
    f2 -- no --> w3
    w3 --> p1 --> f4 --> p2 --> p3 --> f5 --> f6 --> s8
    w4 -. repairs .-> e1
```

## Sequence

```mermaid
sequenceDiagram
    autonumber
    participant UI as Customizer app
    participant API as metaldogs HTTP
    participant Q as Job queue
    participant W as Worker
    participant S3 as Private storage
    participant Shop as Shopanda core
    participant Ev as Shopanda event bus
    participant Op as Operator
    participant POD as POD provider

    UI->>API: POST /designs/{id}/approve
    API->>API: DesignVersion{artwork, checksum, template v, texts, provider, model, prompt v}
    API->>Q: enqueue prepare_production_artwork(version_id)
    W->>S3: read approved artwork
    W->>W: render to POD spec, validate, checksum
    W->>S3: put production asset
    W->>API: version.production_status = ready

    UI->>API: POST /api/customizer/designs/{id}/add-to-cart {variant_id} (Bearer, session cookie)
    Note over API: Response is cart_url /cart. The cart id stays in the cookie
    API->>Shop: trusted plugin write of metaldogs.design_id on AddItem only
    Shop->>Shop: cart.validate via validateDesignLine. No ownership check
    API-->>UI: 200 cart_url
    UI->>Shop: redirect /cart → /checkout (SSR, guest session cart)
    Shop->>API: metaldogs_design_sale before reserve_inventory
    API-->>Shop: ok, or 422 plugin.metaldogs.design_unavailable with the failing lines and no order
    Shop->>Shop: create order (extension values copied to order line)
    Shop->>Ev: order.created
    Ev->>Q: dispatch_snapshot_order (new id per event)
    W->>Q: enqueue snapshot_order_design once (UUID v5 of the order id)
    W->>W: for each line with design_id: insert snapshot by (order_id, variant_id)
    W->>API: design → ordered, assets retention = keep
    Shop->>Ev: payment.confirmed
    Ev->>API: handler → fulfilment pending
    alt production artwork not ready
        API->>API: awaiting_artwork (retry when ready)
    end
    alt manual gate on
        Op->>API: approve submission (permission metaldogs.fulfillment.approve)
    end
    API->>Q: enqueue submit_fulfillment(snapshot_id) idempotent
    W->>S3: signed URL or bytes for production asset
    W->>POD: create order {sku, qty, artwork, shipping address from order}
    POD-->>W: external_order_id
    W->>API: submitted
    POD->>API: webhook status (signature verified)
    API->>API: map → in_production / shipped(tracking)
    API->>Shop: update shipment / tracking
    Shop->>Shop: notify customer via mailer
```

## Guarantees

| Guarantee | Mechanism |
| --- | --- |
| Order always references the exact approved artwork | `DesignVersion` frozen at approval with checksum; snapshot copies version fields; assets referenced by a snapshot are excluded from retention until fulfilment completes |
| Later template or prompt changes do not affect orders | Designs pin `template_version`; templates are versioned and immutable once published; snapshots copy the version number |
| One snapshot per order line | Unique `(order_id, variant_id)`; insert is `ON CONFLICT DO NOTHING`. A repeated `order.created` is a second dispatcher job and the same snapshot job (see Event delivery) |
| Snapshot eventually exists even if the event is lost | Scheduler reconciliation sweep queries Shopanda orders with `metaldogs.design_id` lines lacking snapshots and re-enqueues |
| No double POD orders | `submit_fulfillment` idempotent on `snapshot_id`; provider idempotency key = snapshot id where supported; status `submitting` lock |
| Operator can always fulfil by hand | P1 manual path and runbook independent of P2 automation |

## Event delivery

`RegisterOnEvent` cannot set a job id. `NewSyncJobEnvelope` uses `id.New()`, and `pluginsdk.WithJobID` applies only to `SyncJobs.Enqueue`. `snapshot_order_design` is registered with `RegisterManual`. `dispatch_snapshot_order` is `RegisterOnEvent` on `order.created` and only calls `EnqueueSnapshotOrder`, which enqueues with `WithJobID` set to the UUID v5 of the order id. Two events insert two dispatcher jobs and one snapshot job. The snapshot write is one row: `INSERT … ON CONFLICT (order_id, variant_id) DO NOTHING`. `approved → ordered` does nothing when the design is already `ordered`.

`plugins.metaldogs.order_snapshot.enabled` stays false until an `OrderSource` adapter is bound. With the flag on and no source bound, the snapshot handler returns an error. The job retries and ends `failed`, visible in `jobs:list --type=integration.sync.metaldogs.snapshot_order_design --status=failed`. E4a does not bind a source. Tests inject the in-memory fake through `SetOrderSource`.

## Open items to verify (tracked in ROADMAP §1)

- Cart-line `metaldogs.design_id` is contract-tested in E2 (CR-19). E3 contract-tests `POST /api/v1/checkout` with manual payment (CR-30), including `plugin.metaldogs.design_unavailable` and a retryable `503`. Snapshot mode copies the field onto the order line in core; E4 consumes that copy. SSR confirm exists upstream (PR-1598). Guest Stripe test mode is not recorded.
- Event names `order.created` / `payment.confirmed` and whether handlers run synchronously within the order transaction (affects whether an outbox is needed).
- Shopanda API for updating shipment/tracking from a plugin.
