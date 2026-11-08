# Metal Dogs quality entrypoints.
# No git hooks are installed by this Makefile — run checks explicitly.
#
#   make check          # unit contract (scripts/ci/unit.sh)
#   make integration    # requires SHOPANDA_TEST_DSN
#   make govuln         # requires govulncheck on PATH
#   make docs-policy    # Metal Dogs hardened product-docs checker (same as CI docs-policy link step)

.PHONY: check unit integration govuln docs-policy product-init-test phase17-scripts-test sync-seed

check unit:
	bash scripts/ci/unit.sh

integration:
	bash scripts/ci/integration.sh

govuln:
	bash scripts/ci/govuln.sh

# Prefer the Metal Dogs–hardened scanner. Do not set SHOPANDA_PLATFORM=1 here
# (that encodes upstream platform promote semantics on a main-only product tree).
docs-policy:
	bash .github/scripts/check-product-docs-links.sh
	bash .github/scripts/check-upstream-deviations.sh

sync-seed:
	bash scripts/product-init/sync-seed.sh

product-init-test:
	bash scripts/product-init/product-init_test.sh

phase17-scripts-test: product-init-test
	bash scripts/ci/docs-policy_test.sh
