# G1 spike template set

Five representative Metal Dogs templates for the paid OpenAI spike. Prompts are
placeholders — tune after the first smoke batch. Photos are **not** vendored;
point `metaldogs:eval -photos` at a local folder with ≥20 varied dog photos.

```bash
export METALDOGS_OPENAI_API_KEY=sk-...
./app metaldogs:eval \
  -template plugins/metaldogs/generation/eval/testdata/spike/01-head-front-blackmetal.json \
  -photos /path/to/spike-photos \
  -out /tmp/md-spike-01 \
  -provider openai
```

Score with the rubric in [`docs/architecture/generation-quality.md`](../../../../../../docs/architecture/generation-quality.md).
Fill `scorecard.json` next to `report.json` (see `scorecard.example.json`).
