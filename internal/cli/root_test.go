package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/johannhipp/kcli/internal/app"
	schemacatalog "github.com/johannhipp/kcli/internal/schema"
	"github.com/rogpeppe/go-internal/testscript"
)

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){"kcli": func() { os.Exit(Execute(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }})
}
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{Dir: filepath.Join("..", "..", "testdata", "script")})
}

func TestCatalogAndSchema(t *testing.T) {
	parser, err := kong.New(&Root{}, kong.Name("kcli"))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := app.BuildCatalog(parser.Model)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(catalog.Operations()); got != 33 {
		t.Fatalf("catalog has %d operations, want 33", got)
	}
	schemas := schemacatalog.New(catalog, nil)
	if err := schemas.Validate("search", []byte(`{"query":"bike"}`)); err != nil {
		t.Fatalf("valid search input: %v", err)
	}
	if err := schemas.Validate("search", []byte(`{"unknown":true}`)); err == nil {
		t.Fatal("unknown input field was accepted")
	}
	document, err := schemas.Show("search")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(document.Input.Properties["ad_type"].Enum); got != 2 {
		t.Fatalf("search ad_type enum has %d values", got)
	}
	for _, operation := range catalog.Operations() {
		if _, err := schemas.Show(operation.Path); err != nil {
			t.Errorf("schema %q: %v", operation.Path, err)
		}
	}
	for _, operation := range catalog.Operations() {
		var stdout, stderr bytes.Buffer
		args := append(strings.Fields(operation.Path), "--help")
		if code := Execute(context.Background(), args, bytes.NewReader(nil), &stdout, &stderr); code != 0 || stdout.Len() == 0 {
			t.Errorf("%s --help: exit=%d stdout=%q stderr=%q", operation.Path, code, stdout.String(), stderr.String())
		}
	}
}
func TestStdioSeparationAndResourceExit(t *testing.T) {
	setPathEnvironment(t)
	var stdout, stderr bytes.Buffer
	code := Execute(context.Background(), []string{"search", "x"}, bytes.NewReader(nil), &stdout, &stderr)
	if code != 4 {
		t.Fatalf("exit = %d, want 4", code)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("search state is unavailable")) {
		t.Fatalf("missing diagnostic: %q", stderr.String())
	}
	decoder := json.NewDecoder(&stdout)
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("stdout is not JSON: %v: %q", err, stdout.String())
	}
	if value["schema"] != "kcli.error/v1" || value["code"] != "unavailable" {
		t.Fatalf("unexpected error envelope: %#v", value)
	}
	if err := decoder.Decode(&value); err != io.EOF {
		t.Fatalf("stdout contains more than one JSON value: %v", err)
	}
}
func TestCompletionDoesNotReadOrCreateConfig(t *testing.T) {
	base := setPathEnvironment(t)
	configPath := filepath.Join(base, "config", "kcli", "config.json")
	var stdout, stderr bytes.Buffer
	if code := Execute(context.Background(), []string{"completion", "bash"}, bytes.NewReader(nil), &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("completion touched config: %v", err)
	}
	if !bytes.HasPrefix(stdout.Bytes(), []byte("complete ")) {
		t.Fatalf("unexpected completion: %q", stdout.String())
	}
}
func setPathEnvironment(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	t.Setenv("KCLI_CONFIG_HOME", filepath.Join(base, "config"))
	t.Setenv("KCLI_STATE_HOME", filepath.Join(base, "state"))
	t.Setenv("KCLI_CACHE_HOME", filepath.Join(base, "cache"))
	return base
}
