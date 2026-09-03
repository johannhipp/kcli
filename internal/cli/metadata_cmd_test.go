package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/johannhipp/kcli/internal/app"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/output"
	"github.com/johannhipp/kcli/internal/state"
)

type cliFixtureTransport struct {
	t     *testing.T
	calls []kleinanzeigen.Request
}

func (f *cliFixtureTransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	f.calls = append(f.calls, request)
	var fixture string
	switch request.Path {
	case "/api/categories.json":
		fixture = "categories.json"
	case "/api/locations.json":
		if request.Query["q"][0] != "Berlin" {
			f.t.Fatalf("unexpected location query: %#v", request.Query)
		}
		fixture = "locations.json"
	case "/api/ads/search-metadata/278.json":
		fixture = filepath.Join("search-metadata", "278.json")
	default:
		f.t.Fatalf("unexpected mobile request: %s", request.Path)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", fixture))
	if err != nil {
		f.t.Fatal(err)
	}
	return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: body}, nil
}

func TestMetadataCommandsEmitNormalizedAndRawEvidence(t *testing.T) {
	ctx := context.Background()
	database, err := state.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	transport := &cliFixtureTransport{t: t}
	var stdout, stderr bytes.Buffer
	runtime := &Runtime{Context: ctx, Stdout: &stdout, Stderr: &stderr, RequestID: "req_fixture", Core: app.New(app.Dependencies{Transport: transport, State: database}), Encoder: output.Encoder{Format: output.FormatJSON}}
	tests := []struct {
		name, schema string
		run          func() error
	}{
		{name: "category list", schema: "kcli.categories/v1", run: func() error { return (&CategoryListCmd{Refresh: true}).Run(runtime) }},
		{name: "category get", schema: "kcli.category/v1", run: func() error { return (&CategoryGetCmd{IDOrPath: "278"}).Run(runtime) }},
		{name: "category search", schema: "kcli.categories/v1", run: func() error { return (&CategorySearchCmd{Text: "fahrrad"}).Run(runtime) }},
		{name: "location resolve", schema: "kcli.locations/v1", run: func() error { return (&LocationResolveCmd{Text: "Berlin", Limit: 2}).Run(runtime) }},
		{name: "filter list", schema: "kcli.filters/v1", run: func() error { return (&FilterListCmd{Category: "278", Refresh: true}).Run(runtime) }},
		{name: "filter get", schema: "kcli.filter/v1", run: func() error { return (&FilterGetCmd{Category: "278", Key: "condition"}).Run(runtime) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			if err := test.run(); err != nil {
				t.Fatal(err)
			}
			var envelope map[string]any
			if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
				t.Fatalf("invalid single JSON envelope: %v\n%s", err, stdout.String())
			}
			if envelope["schema"] != test.schema || envelope["data"] == nil || envelope["raw"] == nil {
				t.Fatalf("missing normalized/raw evidence: %#v", envelope)
			}
			if stderr.Len() != 0 {
				t.Fatalf("primary command wrote diagnostic without cause: %q", stderr.String())
			}
		})
	}
}

func TestMetadataNonTTYDefaultIsJSON(t *testing.T) {
	if got := defaultFormat(&bytes.Buffer{}); got != output.FormatJSON {
		t.Fatalf("non-TTY default=%s", got)
	}
}
