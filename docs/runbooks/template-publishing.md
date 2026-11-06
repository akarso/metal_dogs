# Runbook: adding, changing, publishing, and retiring templates

- **Introduced by slice:** `G2` (format and loader), `X1` (authoring at scale)
- **Last exercised:** never
- **Severity when triggered:** low (routine) / medium (regression after a change)
- **Owner:** template author + backend maintainer

## Purpose

Templates are declarative definitions (see `docs/architecture/domain-model.md`) loaded from files into `metaldogs_templates` with a content hash and version. Publishing makes a template visible to customers; changing a published template creates a **new version**; in-flight designs keep their pinned version (ADR-0005).

## Procedure: add a template

1. Author the definition file under `plugins/metaldogs/templates/<slug>.yaml` (`TBD (G2)` exact path/format) with: genre, style, reference pattern asset, prompt instructions + parameters, photo requirements, text fields, output config.
2. Upload the reference pattern and thumbnail assets (`TBD (G2)` command).
3. Validate: `TBD (G2)` command `metaldogs:templates validate`.
4. Render test outputs with the fake provider to check composition and print area (`metaldogs:eval` from `G0`).
5. Optional, paid: run the real provider on the standard photo set (`METALDOGS_SMOKE=1`) and score against ADR-0008 criteria.
6. Load as `draft`: `TBD (G2)` command `metaldogs:templates load`. Drafts are invisible to the API.
7. Publish: `TBD (G2)` command `metaldogs:templates publish <slug>`; version becomes 1.

## Procedure: change a published template

1. Edit the file; content hash changes.
2. Validate and test as above.
3. Load → creates version N+1 as `draft`; version N stays `published`.
4. Publish N+1 → N becomes `retired` for new designs; existing designs pinned to N continue to work (they can still regenerate with N's prompt).

## Procedure: retire a template

- `TBD (G2)` command `metaldogs:templates retire <slug>`. Existing designs and orders are unaffected.

## Regression after a change

Symptom: `validation_failed` or low quality clustered on one `template_slug` after publish.

1. Confirm with `jobs:list --type=integration.sync.metaldogs.generate --status=failed` filtered by template (`TBD`: filter support).
2. Re-publish the previous version (`TBD (G2)` command `metaldogs:templates publish <slug> --version=N`). This is safe: versions are immutable.
3. Fix the definition offline; repeat the change procedure.

## Verification

- `GET /api/customizer/templates` shows the expected set; a draft never appears; prompts never appear in the response.

## Prevention / follow-up

- Standard photo set and scoring rubric kept with the eval harness (no customer photos).
- `X1` adds bulk validation and preview rendering in CI for all templates.
