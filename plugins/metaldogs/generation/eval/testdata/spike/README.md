# G1 spike scorecard kit

Template definitions moved to Template v1 YAML under
[`plugins/metaldogs/templates/`](../../../templates/). Use those files with
`metaldogs:eval` and fill `scorecard.json` from `scorecard.example.json`.

```bash
export METALDOGS_OPENAI_API_KEY=sk-...
./app metaldogs:eval \
  -template plugins/metaldogs/templates/head-front-blackmetal.yaml \
  -photos /path/to/spike-photos \
  -out /tmp/md-spike-01 \
  -provider openai \
  -dog-name Burek
```

Score with the rubric in [`docs/architecture/generation-quality.md`](../../../../../../docs/architecture/generation-quality.md).
