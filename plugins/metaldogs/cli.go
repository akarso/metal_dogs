package metaldogs

import (
	"fmt"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/eval"
)

// CommandEval is the offline generation evaluation CLI (G0 / ADR-0008).
const CommandEval = "metaldogs:eval"

func registerCLICommands(app *plugin.App) {
	app.RegisterCommand(cli.Command{
		Name:        CommandEval,
		Description: "Offline generation eval (template + photos → PNGs + report.json); providers fake|openai",
		RequiresDB:  false,
		Run: func(ctx cli.Context, args []string) error {
			opts, err := eval.ParseArgs(args)
			if err != nil {
				return err
			}
			report, err := eval.Run(ctx.Ctx, opts)
			if err != nil {
				return err
			}
			ok, failed := 0, 0
			for _, s := range report.Samples {
				if s.Error == "" {
					ok++
				} else {
					failed++
				}
			}
			fmt.Printf("metaldogs:eval wrote %d ok / %d failed → %s/report.json\n", ok, failed, opts.OutDir)
			if ctx.Logger != nil {
				ctx.Logger.Info("metaldogs.eval", map[string]interface{}{
					"template_id": report.TemplateID,
					"provider":    report.Provider,
					"ok":          ok,
					"failed":      failed,
					"out_dir":     opts.OutDir,
				})
			}
			if failed > 0 {
				return fmt.Errorf("metaldogs:eval: %d sample(s) failed", failed)
			}
			return nil
		},
	})
}
