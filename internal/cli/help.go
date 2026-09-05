package cli

import (
	"fmt"

	"github.com/alecthomas/kong"
	"github.com/johannhipp/kcli/internal/app"
)

// examplesHelpPrinter renders Kong's default help and then appends the
// operation's documented examples for the selected command, so `--help` is
// self-sufficient for agents instead of only surfacing examples through
// `kcli schema show`.
func examplesHelpPrinter(options kong.HelpOptions, ctx *kong.Context) error {
	if err := kong.DefaultHelpPrinter(options, ctx); err != nil {
		return err
	}
	selected := ctx.Selected()
	if selected == nil {
		return nil
	}
	path := selectedPath(selected)
	if path == "" {
		return nil
	}
	catalog, err := app.BuildCatalog(ctx.Model)
	if err != nil {
		return nil
	}
	meta, ok := catalog.Find(path)
	if !ok || len(meta.Examples) == 0 {
		return nil
	}
	_, _ = fmt.Fprintf(ctx.Stdout, "\nExamples:\n")
	for _, example := range meta.Examples {
		_, _ = fmt.Fprintf(ctx.Stdout, "  %s\n", example)
	}
	return nil
}
