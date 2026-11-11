# 03: Price parsing rules and validate before write

**What to build:** The catalog command validates all input files before writing anything and applies the full price-format rules. A human can write prices as "80,49", "80.49", "1.234,56" or "1,234.56" and get the correct amount; anything ambiguous or malformed stops the import with a message that names the file, row and column, and the database stays unchanged.

**Blocked by:** 02.

**Status:** ready-for-agent

- [ ] "80,49" and "80.49" both store the same net amount
- [ ] "1.234,56" and "1,234.56" both store 1234,56 (last separator is decimal, the other is thousands)
- [ ] Rejected with a clear error: more than two decimal places ("80,499"), spaces, currency text or symbols ("80 zł", "80 PLN"), negative, zero, empty, non-numeric ("abc")
- [ ] Error message names the file, row and column, and the command exits non-zero
- [ ] A file with one bad price among good rows writes nothing: product, variants, prices and stock are all unchanged
- [ ] No silent rounding anywhere
- [ ] All cases are exercised through the command and observed through the public storefront API or the command result, with no separate parser unit tests
