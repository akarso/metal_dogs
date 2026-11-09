# Runbook: adding, changing, publishing, and retiring templates

- **Introduced by slice:** `G2` (format and loader), `X1` (authoring at scale)
- **Last exercised:** never (catalog stays draft until C1)
- **Severity when triggered:** low (routine) / medium (regression after a change)
- **Owner:** template author + backend maintainer

## Purpose

Templates are declarative definitions (see `docs/architecture/domain-model.md`) loaded from files into `metaldogs_templates` with a content hash and version. Publishing makes a template visible to customers; changing a published template creates a **new version**; in-flight designs keep their pinned version (ADR-0005).

## Procedure: add a template

1. Author the definition file under `plugins/metaldogs/templates/<slug>.yaml` with: genre, style, optional reference/thumbnail relative paths, prompt instructions + parameters, photo requirements, text fields, output config.
2. Upload the reference pattern and thumbnail assets when the asset ingest path exists (FKs stay null until then).
3. Validate:

```bash
./app metaldogs:templates validate -file plugins/metaldogs/templates/<slug>.yaml
```

4. Render test outputs with the fake provider to check composition and print area:

```bash
./app metaldogs:eval \
  -template plugins/metaldogs/templates/<slug>.yaml \
  -photos /path/to/photos -out /tmp/md-eval -provider fake \
  -dog-name Burek
```

5. Optional, paid: run the real provider on the standard photo set and score against ADR-0008 criteria (`-provider openai`).
6. Load as `draft` (invisible to the API):

```bash
./app metaldogs:templates load -file plugins/metaldogs/templates/<slug>.yaml
```

7. Publish when ready for C1:

```bash
./app metaldogs:templates publish -slug <slug>
# or: ./app metaldogs:templates publish -slug <slug> -version N
```

## Procedure: change a published template

1. Edit the file; content hash changes.
2. Validate and test as above.
3. `metaldogs:templates load` → creates version N+1 as `draft`; version N stays `published`.
4. Publish N+1 → N becomes `retired` for new designs; existing designs pinned to N continue to work.

## Procedure: retire a template

```bash
./app metaldogs:templates retire -slug <slug>
```

Existing designs and orders are unaffected.

## Regression after a change

Symptom: `validation_failed` or low quality clustered on one `template_slug` after publish.

1. Confirm with `jobs:list --type=integration.sync.metaldogs.generate --status=failed` filtered by template (`TBD`: filter support).
2. Re-publish a prior version:

```bash
./app metaldogs:templates publish -slug <slug> -version N
```

Versions are immutable; this retires the bad published row and restores N.
3. Fix the definition offline; repeat the change procedure.

## Verification

- `GET /api/customizer/templates` (C1) shows the expected set; a draft never appears; prompts never appear in the response.
- Until C1: confirm rows with SQL / load CLI output (`status=draft` for the G2 authored set).

## Prevention / follow-up

- Standard photo set and scoring rubric kept with the eval harness (no customer photos).
- `X1` adds bulk validation and preview rendering in CI for all templates.
