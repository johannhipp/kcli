package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/johannhipp/kcli/internal/app"
)

// TestDocumentedCommandsCoverCatalog guards against CLI commands drifting from
// the documented contract in docs/command-syntax.md. Every operation in the
// derived catalog must be documented as a `kcli <command>` invocation, so a
// newly added or renamed command fails this test until the doc is updated.
func TestDocumentedCommandsCoverCatalog(t *testing.T) {
	root := &Root{}
	parser, err := kong.New(root, kong.Name("kcli"))
	if err != nil {
		t.Fatalf("parse CLI model: %v", err)
	}
	catalog, err := app.BuildCatalog(parser.Model)
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	text, err := os.ReadFile("../../docs/command-syntax.md")
	if err != nil {
		t.Fatalf("read command-syntax.md: %v", err)
	}
	documented := string(text)
	var missing []string
	for _, op := range catalog.Operations() {
		if !strings.Contains(documented, "kcli "+op.Path) {
			missing = append(missing, op.Path)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("commands not documented in docs/command-syntax.md: %v", missing)
	}
}
