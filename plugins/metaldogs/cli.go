package metaldogs

import (
	"database/sql"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/akarso/shopanda/internal/platform/cli"
	"github.com/akarso/shopanda/internal/platform/config"
	"github.com/akarso/shopanda/internal/platform/db"
	"github.com/akarso/shopanda/internal/platform/plugin"
	"github.com/akarso/shopanda/plugins/metaldogs/adapters/postgres"
	"github.com/akarso/shopanda/plugins/metaldogs/application"
	"github.com/akarso/shopanda/plugins/metaldogs/generation/eval"
	"github.com/akarso/shopanda/plugins/metaldogs/ports"
)

// Command names registered by the Metal Dogs plugin.
const (
	CommandEval      = "metaldogs:eval"
	CommandTemplates = "metaldogs:templates"
)

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

	// RequiresDB false so `validate` works offline; load/publish/retire open DB themselves.
	app.RegisterCommand(cli.Command{
		Name:        CommandTemplates,
		Description: "Template catalog: validate | load | publish | retire",
		RequiresDB:  false,
		Run:         runTemplates,
	})
}

func openCLIDatabase(ctx cli.Context) (*sql.DB, func(), error) {
	if ctx.DB != nil {
		return ctx.DB, func() {}, nil
	}
	if ctx.Config == nil {
		return nil, nil, fmt.Errorf("metaldogs:templates: config required for database commands")
	}
	conn, err := db.Open(config.DatabaseDSN(ctx.Config))
	if err != nil {
		return nil, nil, fmt.Errorf("metaldogs:templates: database: %w", err)
	}
	return conn, func() { _ = conn.Close() }, nil
}

func runTemplates(ctx cli.Context, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("metaldogs:templates: subcommand required (validate|load|publish|retire)")
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "validate":
		return runTemplatesValidate(ctx, rest)
	case "load":
		return runTemplatesLoad(ctx, rest)
	case "publish":
		return runTemplatesPublish(ctx, rest)
	case "retire":
		return runTemplatesRetire(ctx, rest)
	default:
		return fmt.Errorf("metaldogs:templates: unknown subcommand %q (want validate|load|publish|retire)", sub)
	}
}

func runTemplatesValidate(_ cli.Context, args []string) error {
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

func runTemplatesLoad(ctx cli.Context, args []string) error {
	path, err := parseFileFlag("metaldogs:templates load", args)
	if err != nil {
		return err
	}
	conn, closeFn, err := openCLIDatabase(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
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

func runTemplatesPublish(ctx cli.Context, args []string) error {
	slug, version, err := parseSlugVersion("metaldogs:templates publish", args)
	if err != nil {
		return err
	}
	conn, closeFn, err := openCLIDatabase(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
	cat := &application.TemplateCatalog{Store: postgres.NewTemplateStore(conn)}
	t, err := cat.Publish(ctx.Ctx, slug, version)
	if err != nil {
		return err
	}
	fmt.Printf("published slug=%s version=%d\n", t.Slug, t.Version)
	return nil
}

func runTemplatesRetire(ctx cli.Context, args []string) error {
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
	conn, closeFn, err := openCLIDatabase(ctx)
	if err != nil {
		return err
	}
	defer closeFn()
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
