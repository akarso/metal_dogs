# 02: `metaldogs:catalog-seed` command, happy path

**What to build:** A manual plugin command that loads the "Metal Dogs custom tee" from files in the repository: one configurable product with five size variants, a net PLN price and zero stock each. After one run on a fresh database the product exists as a draft and is invisible to customers; running again changes nothing. The price column accepts simple decimals with either a comma or a dot (full parsing rules come in ticket 03).

**Blocked by:** 01, 00 (pin bump exposing public price and stock).

**Status:** ready-for-agent

- [ ] Product "Metal Dogs custom tee", parent SKU MDOGS-TEE, type configurable, status draft
- [ ] Five variants with SKUs MDOGS-TEE-S, -M, -L, -XL, -XXL, each with attribute `size` set to its size
- [ ] One net price per variant in PLN, placeholder 80,49, clearly marked as placeholder in the data file
- [ ] Stock 0 on every variant (observed as `in_stock: false`)
- [ ] Standard tax class applies through product type; the command writes no tax setting
- [ ] SKUs that already exist are skipped before the core importer runs (it conflicts on existing SKUs)
- [ ] Second run produces no new rows and no changes (idempotent by SKU), including leaving an already `active` product's status untouched
- [ ] Through the public storefront API only: draft product is absent; after status is set to active it is present with all five variants, correct SKUs, `size` attribute, net `offer.amount` (requested in PLN) and `in_stock` false
- [ ] The command is not run by migrations or at startup; the Shopanda core is not modified
- [ ] Gross computed from the stored net price and the standard rate equals 99,00 for the placeholder
- [ ] Comments in new code follow the AGENTS.md "Code comments" rules
