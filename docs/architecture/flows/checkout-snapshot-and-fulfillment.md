# Flow: approval, checkout, order snapshot, fulfillment

Covers [ADR-0005](../../adr/0005-immutable-order-design-snapshot.md) (immutable snapshot) and track P. Shopanda owns cart, checkout, payment, and the order; the plugin attaches the design and reacts to the order.

## Swimlane

```mermaid
flowchart TB
    subgraph customizer[metaldogs customizer]
        a1[Approve: freeze DesignVersion with checksum]
        a2[Enqueue prepare_production_artwork — C6 stub fails pending_spec; Requeue until G5]
        a3[Add to cart: verify approved and owned]
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
        e1[On order.created: enqueue snapshot job idempotent on order_line_id]
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

    UI->>API: POST /designs/{id}/add-to-cart {variant_sku}
    API->>Shop: AddLine(product, variant, ext: metaldogs.design_id)
    Shop->>API: cart hook validate(design_id, cart session)
    API-->>Shop: ok (approved, owned)
    Shop-->>UI: cart updated
    UI->>Shop: redirect /cart → /checkout (SSR, guest)
    Shop->>API: checkout step validate designs
    API-->>Shop: ok
    Shop->>Shop: create order (extension values copied to order line)
    Shop->>Ev: order.created
    Ev->>API: handler
    API->>Q: enqueue snapshot_order_design(order_id) idempotent
    W->>W: for each line with design_id: upsert snapshot by order_line_id
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
| One snapshot per order line | Unique index on `order_line_id`; handler is an upsert |
| Snapshot eventually exists even if the event is lost | Scheduler reconciliation sweep queries Shopanda orders with `metaldogs.design_id` lines lacking snapshots and re-enqueues |
| No double POD orders | `submit_fulfillment` idempotent on `snapshot_id`; provider idempotency key = snapshot id where supported; status `submitting` lock |
| Operator can always fulfil by hand | P1 manual path and runbook independent of P2 automation |

## Open items to verify (tracked in ROADMAP §1)

- Extension fields are available at **cart line** granularity and are copied to the order line at checkout.
- Event names `order.created` / `payment.confirmed` and whether handlers run synchronously within the order transaction (affects whether an outbox is needed).
- Shopanda API for updating shipment/tracking from a plugin.
