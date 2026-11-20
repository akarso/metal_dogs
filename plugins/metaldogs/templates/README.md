# Metal Dogs templates (G2 v1)

Declarative YAML definitions loaded into `metaldogs_templates` as **draft** via
`metaldogs:templates load`. See [`docs/runbooks/template-publishing.md`](../../../docs/runbooks/template-publishing.md).

Spike set (G1 quality gate / eval):

| File | Pose / stress |
| --- | --- |
| `head-front-blackmetal.yaml` | head front |
| `head-angle-thrash.yaml` | head angle |
| `profile-deathmetal.yaml` | full-body profile |
| `long-name-polish.yaml` | long Polish name typography |
| `short-name-punk.yaml` | short name typography |

`validate` does not query, but it opens the configured DSN (`RequiresDB`). `./app metaldogs:eval` is rejected on the process CLI until MIG3 ([CR-22](../../../docs/cr-rounds/CR-22-mig2-pluginsdk-group-a.md), UPSTREAM-CANDIDATES #18).

```bash
./app metaldogs:templates validate -file plugins/metaldogs/templates/head-front-blackmetal.yaml
./app metaldogs:eval \
  -template plugins/metaldogs/templates/head-front-blackmetal.yaml \
  -photos /path/to/photos -out /tmp/md-eval -provider fake \
  -dog-name Burek
```
