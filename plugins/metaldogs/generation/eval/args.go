package eval

import (
	"flag"
	"fmt"
	"os"
)

// ParseArgs parses metaldogs:eval flags from args (excluding the command name).
func ParseArgs(args []string) (Options, error) {
	fs := flag.NewFlagSet("metaldogs:eval", flag.ContinueOnError)
	fs.SetOutput(flag.CommandLine.Output())

	var opts Options
	fs.StringVar(&opts.TemplatePath, "template", "", "path to template JSON/YAML definition")
	fs.StringVar(&opts.PhotosDir, "photos", "", "directory of local photos (.jpg/.jpeg/.png)")
	fs.StringVar(&opts.OutDir, "out", "", "output directory for PNGs + report.json")
	fs.StringVar(&opts.ProviderName, "provider", "fake", "image provider name (fake|openai)")
	fs.StringVar(&opts.OpenAIAPIKey, "openai-api-key", "", "OpenAI API key (or METALDOGS_OPENAI_API_KEY)")
	fs.StringVar(&opts.OpenAIModel, "openai-model", "", "OpenAI image model (default gpt-image-2.5-sunburst; template model wins when set)")
	fs.StringVar(&opts.OpenAIBase, "openai-base", "", "OpenAI API base URL (default https://api.openai.com/v1)")

	if err := fs.Parse(args); err != nil {
		return Options{}, err
	}
	if fs.NArg() != 0 {
		return Options{}, fmt.Errorf("eval: unexpected positional args %v", fs.Args())
	}
	if opts.OpenAIAPIKey == "" {
		opts.OpenAIAPIKey = os.Getenv("METALDOGS_OPENAI_API_KEY")
	}
	return opts, nil
}
