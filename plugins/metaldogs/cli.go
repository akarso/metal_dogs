package metaldogs

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/pkg/pluginsdk"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/catalog"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/eval"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// Command names registered by the Metal Dogs plugin.
const (
	CommandEval        = "metaldogs:eval"
	CommandTemplates   = "metaldogs:templates"
	CommandCatalogSeed = "metaldogs:catalog-seed"
	CommandSnapshots   = "metaldogs:snapshots"
)

func registerCLICommands(h pluginsdk.Host, p *Plugin) {
	reg := pluginsdk.NewHost(h, PluginName).CLI()
	reg.RegisterCommand(pluginsdk.CLICommand{
		Name:        CommandEval,
		Description: "Offline generation eval (template + photos → PNGs + report.json); providers fake|openai",
		RequiresDB:  false,
		Run: func(ctx pluginsdk.CLIContext, args []string) error {
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

	// RequiresDB so the composition root opens the pool before Run. validate
	// ignores it; load/publish/retire read Host.BootstrapDB (CapDB).
	reg.RegisterCommand(pluginsdk.CLICommand{
		Name:        CommandTemplates,
		Description: "Template catalog: validate | load | publish | retire",
		RequiresDB:  true,
		Run: func(ctx pluginsdk.CLIContext, args []string) error {
			return runTemplates(p, ctx, args)
		},
	})

	reg.RegisterCommand(pluginsdk.CLICommand{
		Name:        CommandCatalogSeed,
		Description: "Load the Metal Dogs catalog CSV (draft product, idempotent by SKU)",
		RequiresDB:  true,
		Run:         p.runCatalogSeed,
	})

	reg.RegisterCommand(pluginsdk.CLICommand{
		Name:        CommandSnapshots,
		Description: "Report design lines with no snapshot: reconcile [--order=<id>] [--fix]. Report only; --fix enqueues snapshot_order_design. Production fill runs on the reconcile_snapshots cron.",
		RequiresDB:  true,
		Run:         p.runSnapshots,
	})
}

func (p *Plugin) runCatalogSeed(ctx pluginsdk.CLIContext, args []string) error {
	fs := flag.NewFlagSet(CommandCatalogSeed, flag.ContinueOnError)
	fs.SetOutput(flag.CommandLine.Output())
	dir := fs.String("dir", "plugins/metaldogs/catalog", "directory with products.csv, prices.csv, and stock.csv")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c := ctx.Ctx
	if c == nil {
		c = context.Background()
	}
	if p == nil || p.host == nil {
		return fmt.Errorf("%s: catalog seed port is not configured", CommandCatalogSeed)
	}
	seed, ok := p.host.CatalogSeed()
	if !ok || seed == nil {
		return fmt.Errorf("%s: catalog seed port is not configured", CommandCatalogSeed)
	}
	if err := catalog.Seed(c, seed, *dir); err != nil {
		return err
	}
	fmt.Println("metaldogs:catalog-seed: ok")
	return nil
}

func openCLIDatabase(p *Plugin) (*sql.DB, error) {
	if p == nil || p.host == nil {
		return nil, fmt.Errorf("metaldogs:templates: database is not configured")
	}
	conn, ok := p.host.BootstrapDB()
	if !ok || conn == nil {
		return nil, fmt.Errorf("metaldogs:templates: database is not configured")
	}
	return conn, nil
}

func runTemplates(p *Plugin, ctx pluginsdk.CLIContext, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("metaldogs:templates: subcommand required (validate|load|publish|retire)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "validate":
		return runTemplatesValidate(rest)
	case "load":
		return runTemplatesLoad(p, ctx, rest)
	case "publish":
		return runTemplatesPublish(p, ctx, rest)
	case "retire":
		return runTemplatesRetire(p, ctx, rest)
	default:
		return fmt.Errorf("metaldogs:templates: unknown subcommand %q (want validate|load|publish|retire)", sub)
	}
}

func runTemplatesValidate(args []string) error {
	path, err := parseFileFlag("metaldogs:templates validate", args)
	if err != nil {
		return err
	}
	t, err := application.ValidatePath(path)
	if err != nil {
		return err
	}
	fmt.Printf("ok slug=%s hash=%s\n", t.Slug, t.ContentHash)
	return nil
}

func runTemplatesLoad(p *Plugin, ctx pluginsdk.CLIContext, args []string) error {
	path, err := parseFileFlag("metaldogs:templates load", args)
	if err != nil {
		return err
	}
	conn, err := openCLIDatabase(p)
	if err != nil {
		return err
	}
	cat := &application.TemplateCatalog{
		Store: postgres.NewTemplateStore(conn),
		IDs:   ports.UUIDGenerator{},
	}
	res, err := cat.LoadDraft(ctx.Ctx, path)
	if err != nil {
		return err
	}
	action := "unchanged"
	if res.Created {
		action = "created"
	}
	fmt.Printf("%s slug=%s version=%d status=%s hash=%s\n",
		action, res.Template.Slug, res.Template.Version, res.Template.Status, res.Template.ContentHash)
	return nil
}

func runTemplatesPublish(p *Plugin, ctx pluginsdk.CLIContext, args []string) error {
	slug, version, err := parseSlugVersion("metaldogs:templates publish", args)
	if err != nil {
		return err
	}
	conn, err := openCLIDatabase(p)
	if err != nil {
		return err
	}
	cat := &application.TemplateCatalog{Store: postgres.NewTemplateStore(conn)}
	t, err := cat.Publish(ctx.Ctx, slug, version)
	if err != nil {
		return err
	}
	fmt.Printf("published slug=%s version=%d\n", t.Slug, t.Version)
	return nil
}

func runTemplatesRetire(p *Plugin, ctx pluginsdk.CLIContext, args []string) error {
	fs := flag.NewFlagSet("metaldogs:templates retire", flag.ContinueOnError)
	fs.SetOutput(flag.CommandLine.Output())
	var slug string
	fs.StringVar(&slug, "slug", "", "template slug")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if slug == "" && fs.NArg() == 1 {
		slug = fs.Arg(0)
	}
	if strings.TrimSpace(slug) == "" {
		return fmt.Errorf("metaldogs:templates retire: -slug is required")
	}
	conn, err := openCLIDatabase(p)
	if err != nil {
		return err
	}
	cat := &application.TemplateCatalog{Store: postgres.NewTemplateStore(conn)}
	t, err := cat.Retire(ctx.Ctx, slug)
	if err != nil {
		return err
	}
	fmt.Printf("retired slug=%s version=%d\n", t.Slug, t.Version)
	return nil
}

func parseFileFlag(name string, args []string) (string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(flag.CommandLine.Output())
	var path string
	fs.StringVar(&path, "file", "", "path to Template v1 YAML/JSON")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if path == "" && fs.NArg() == 1 {
		path = fs.Arg(0)
	}
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("%s: -file is required", name)
	}
	return path, nil
}

func parseSlugVersion(name string, args []string) (slug string, version int, err error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(flag.CommandLine.Output())
	var versionStr string
	fs.StringVar(&slug, "slug", "", "template slug")
	fs.StringVar(&versionStr, "version", "", "optional version number (default: latest draft)")
	if err := fs.Parse(args); err != nil {
		return "", 0, err
	}
	if slug == "" && fs.NArg() >= 1 {
		slug = fs.Arg(0)
	}
	if strings.TrimSpace(slug) == "" {
		return "", 0, fmt.Errorf("%s: -slug is required", name)
	}
	if versionStr != "" {
		version, err = strconv.Atoi(versionStr)
		if err != nil || version < 1 {
			return "", 0, fmt.Errorf("%s: invalid -version %q", name, versionStr)
		}
	}
	return slug, version, nil
}
