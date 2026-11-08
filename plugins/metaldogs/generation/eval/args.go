package eval

import (
	"flag"
	"fmt"
)

// ParseArgs parses metaldogs:eval flags from args (excluding the command name).
func ParseArgs(args []string) (Options, error) {
	fs := flag.NewFlagSet("metaldogs:eval", flag.ContinueOnError)
	fs.SetOutput(flag.CommandLine.Output())

	var opts Options
	fs.StringVar(&opts.TemplatePath, "template", "", "path to template JSON/YAML definition")
	fs.StringVar(&opts.PhotosDir, "photos", "", "directory of local photos (.jpg/.jpeg/.png)")
	fs.StringVar(&opts.OutDir, "out", "", "output directory for PNGs + report.json")
	fs.StringVar(&opts.ProviderName, "provider", "fake", "image provider name (G0: fake)")

	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}
	if fs.NArg() != 0 {
		return Options{}, fmt.Errorf("eval: unexpected positional args %v", fs.Args())
	}
	return opts, nil
}
