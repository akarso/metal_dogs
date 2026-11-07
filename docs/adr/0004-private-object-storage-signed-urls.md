# ADR-0004: Private S3-compatible storage with short-lived signed URLs

- **Status:** Accepted (F4 / CR-05, 2026-11-07; platform absorb shopanda#570)
- **Date:** 2026-10-15
- **Related slices:** `F4`, `C2`, `C5`, `E5`, `E6`, `O2`, `O6`
- **Related ADRs:** [ADR-0007](0007-guest-design-ownership-tokens.md)

## Context

Uploaded photos are private personal data. The project description requires private storage, short-lived signed URLs, EXIF stripping, no personal data in public addresses, and a retention policy. Shopanda ships an S3-compatible storage core plugin and a `media.Storage` port; its default is local filesystem. Platform `media.SignedURLStorage` covers Head + short-lived SignedURL for **both** S3 and local (`GET /api/v1/media/local-signed/{token}`), plus `PrivateBlobsPrefix` / `IsPrivatePath` and `BindMediaStorage` ([shopanda#570](https://github.com/akarso/shopanda/pull/570)).

## Decision

1. All Metal Dogs assets (source photos, artwork, mockups, production files, template reference patterns) are stored in a **private** S3-compatible bucket with no public ACLs and server-side encryption where available. Template thumbnails may be served through a public CDN path if they contain no customer data. Production uses core `storages3` + private bucket config (no second MD registry).
2. Object keys are opaque under the platform private blob prefix: `private/blobs/metaldogs/{asset_type}/{yyyy}/{mm}/{asset_id}.{ext}`. Keys never contain dog names, customer identifiers, or original file names.
3. Assets are delivered to clients only through **signed GET URLs** with TTLs: previews ≤ 15 minutes, admin/production downloads ≤ 10 minutes, order-confirmation thumbnails ≤ 7 days (or inlined). URLs are never logged or persisted.
4. The plugin accesses storage through its own `AssetStore` port (`Put`, `Head`, `SignedGetURL`, `Delete`). The only adapter is a thin wrap of Shopanda `media.SignedURLStorage` (registered / bound media from core storagelocal or storages3). No product HMAC route.
5. Every stored object will have a row in `metaldogs_assets` with checksum, dimensions, MIME, and `delete_after`; the storage object is never the source of truth for metadata. (**Deferred to C2+** — F4 wires the port only; nothing writes yet.)
6. Uploads are re-encoded server-side (EXIF and other metadata removed) before being written. (**Deferred to C2+.**)

## Alternatives considered

| Alternative | Why not |
| --- | --- |
| Public bucket with unguessable keys | Still public; violates the project's privacy requirement |
| Serve bytes through the API | Doubles bandwidth through Go; signed URLs offload to storage |
| Store in PostgreSQL | Large binaries in the database complicate backups and retention |
| Local filesystem in production | Not durable across hosts; no signing |
| Product-side `/api/customizer/dev-assets` HMAC shim | Superseded by platform local SignedURLStorage (#570) |

## Consequences

- Positive: a leaked URL expires; storage can be rotated independently; retention is a key-delete.
- Negative: signed URL generation needs credentials in the API process. Mitigation: scoped IAM keys, never exposed to the frontend.
- Local/dev: share `SHOPANDA_MEDIA_LOCAL_SIGN_SECRET` (≥32 bytes) across replicas, or use `SHOPANDA_DEV_MODE` for an ephemeral per-process key.
- F4 ships the port + mediastore wrap with **no Put call sites**; metadata table and EXIF strip land with upload slices.

## Compliance

- Feature test: requesting an asset of a design you do not own returns `404`; expired URL returns `403` from storage. (**Deferred to C2+** ownership/auth.)
- Grep check: no `http` logging of URLs containing `X-Amz-Signature` or local-signed tokens. (Ongoing.)
- Unit test: uploaded JPEG with GPS EXIF → stored object has no EXIF segment. (**Deferred to C2+.**)
- F4: mediastore wrap + `ObjectKey` under `private/blobs/`; platform localfs/S3 contract tests cover signed GET.
