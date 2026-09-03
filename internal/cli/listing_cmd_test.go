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

type listingCLITransport struct{ body []byte }

func (t *listingCLITransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: append([]byte(nil), t.body...)}, nil
}

func listingCLIRuntime(t *testing.T) (*Runtime, *bytes.Buffer, *state.DB) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", "listing.json"))
	if err != nil {
		t.Fatal(err)
	}
	database, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	stdout := &bytes.Buffer{}
	runtime := &Runtime{Context: context.Background(), Stdout: stdout, Stderr: &bytes.Buffer{}, RequestID: "req-listing-cli", Core: app.New(app.Dependencies{Transport: &listingCLITransport{body: body}, State: database}), Encoder: output.Encoder{Format: output.FormatJSON}}
	return runtime, stdout, database
}

func TestListingCommandsRunAgainstFixtureTransport(t *testing.T) {
	runtime, stdout, database := listingCLIRuntime(t)
	defer database.Close()
	if err := (&ListingGetCmd{IDOrURL: "1234567890", Raw: true}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope["schema"] != "kcli.listing/v1" || envelope["raw"] == nil {
		t.Fatalf("listing output=%s err=%v", stdout.String(), err)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok || data["category"] == nil || data["location"] == nil || data["status"] != "ACTIVE" || data["attributes"] == nil || data["media"] == nil || data["seller"] == nil {
		t.Fatalf("listing typed data is incomplete: %#v", envelope["data"])
	}
	for _, secret := range []string{"fixture-token-must-not-appear", "fixture.user@example.invalid", "Bearer "} {
		if bytes.Contains(stdout.Bytes(), []byte(secret)) {
			t.Fatalf("listing output leaked %q: %s", secret, stdout.String())
		}
	}
	stdout.Reset()
	if err := (&ListingGetCmd{IDOrURL: "1234567890"}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	envelope = make(map[string]any)
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope["raw"] != nil {
		t.Fatalf("raw was not opt-in: %s err=%v", stdout.String(), err)
	}
	stdout.Reset()
	if err := (&ListingImagesCmd{IDOrURL: "1234567890", MaxBytes: 25 << 20}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	envelope = make(map[string]any)
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil || envelope["schema"] != "kcli.listing-images/v1" {
		t.Fatalf("images output=%s err=%v", stdout.String(), err)
	}
	items, ok := envelope["data"].([]any)
	if !ok || len(items) != 3 {
		t.Fatalf("image variants=%#v", envelope["data"])
	}
}
