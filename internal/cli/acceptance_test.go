package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johannhipp/kcli/internal/app"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
)

type acceptanceTransport struct {
	t     *testing.T
	calls []kleinanzeigen.Request
}

func (f *acceptanceTransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	f.calls = append(f.calls, request)
	if request.Method != http.MethodGet {
		f.t.Fatalf("unexpected mutation: %s", request.Method)
	}
	var fixture string
	switch request.Path {
	case "/api/categories.json":
		fixture = "categories.json"
	case "/api/locations.json":
		fixture = "locations.json"
	case "/api/ads/search-metadata/278.json":
		fixture = "search-metadata/278.json"
	case "/api/ads.json":
		fixture = "search-page-" + request.Query["page"][0] + ".redacted.json"
	case "/api/ads/1234567890.json":
		fixture = "listing.json"
	case "/api/ads/9999999999.json":
		return kleinanzeigen.Response{StatusCode: http.StatusNotFound}, nil
	default:
		f.t.Fatalf("unexpected request: %s", request.Path)
	}
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", fixture))
	if err != nil {
		f.t.Fatal(err)
	}
	return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: body}, nil
}

// Each call parses real argv and reopens the profile database, just like a new
// CLI invocation. Only the network boundary is replaced with redacted fixtures.
func TestAnonymousCLIJourney(t *testing.T) {
	setPathEnvironment(t)
	transport := &acceptanceTransport{t: t}
	run := func(wantCode int, args ...string) map[string]any {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := execute(context.Background(), args, strings.NewReader(""), &stdout, &stderr, func(deps *app.Dependencies) { deps.Transport = transport })
		if code != wantCode {
			t.Fatalf("%v: exit %d, want %d: %s", args, code, wantCode, stderr.String())
		}
		var result map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("%v: not one JSON value: %v", args, err)
		}
		if bytes.Contains(stdout.Bytes(), []byte("fixture-token-must-not-appear")) || bytes.Contains(stdout.Bytes(), []byte("fixture.user@example.invalid")) {
			t.Fatal("raw output leaked fixture secrets")
		}
		return result
	}
	result := run(0, "search", "bike", "--category", "278", "--location", "Berlin", "--radius", "25", "--min-price", "10.50", "--max-price", "200", "--picture-required", "--filter", "condition=used", "--exclude", "broken", "--page-size", "2", "--paginate", "--limit", "2")
	rows := result["data"].([]any)
	if len(rows) != 2 || rows[0].(map[string]any)["id"] != "100000000001" || rows[1].(map[string]any)["id"] != "100000000003" {
		t.Fatalf("unexpected search results: %#v", rows)
	}
	var pages []kleinanzeigen.Request
	for _, request := range transport.calls {
		if request.Path == "/api/ads.json" {
			pages = append(pages, request)
		}
	}
	if len(pages) != 2 || pages[0].Query["minPrice"][0] != "10.50" || pages[0].Query["condition"][0] != "used" {
		t.Fatalf("unexpected search wire requests: %#v", pages)
	}
	for _, args := range [][]string{{"category", "get", "278"}, {"category", "search", "Fahrr"}, {"filter", "get", "--category", "278", "condition"}} {
		before := len(transport.calls)
		run(0, args...)
		if len(transport.calls) != before {
			t.Fatalf("%v missed the persisted cache", args)
		}
	}
	beforeSchema := len(transport.calls)
	schema := run(0, "schema", "filters", "--category", "278")["data"].(map[string]any)
	if schema["overlay"] != "cached" {
		t.Fatal("schema filters ignored cached metadata")
	}
	if len(transport.calls) != beforeSchema {
		t.Fatal("cached schema accessed the network")
	}
	encodedSchema, _ := json.Marshal(schema)
	if !bytes.Contains(encodedSchema, []byte(`"condition"`)) || !bytes.Contains(encodedSchema, []byte(`"used"`)) {
		t.Fatal("filter schema lost keys or enum values")
	}
	detail := run(0, "listing", "get", "1234567890", "--raw")
	if detail["raw"] == nil || detail["data"].(map[string]any)["description"] == "" {
		t.Fatal("detail/raw missing")
	}
	byURL := run(0, "listing", "get", "https://www.kleinanzeigen.de/s-anzeige/vintage-lampe/1234567890-82-1234")
	if byURL["data"].(map[string]any)["id"] != detail["data"].(map[string]any)["id"] {
		t.Fatal("URL and ID differ")
	}
	images := run(0, "listing", "images", "1234567890")
	if len(images["data"].([]any)) != 3 {
		t.Fatal("lost image variant")
	}
	run(0, "seller", "get", "--listing", "1234567890")
	before := len(transport.calls)
	for _, args := range [][]string{{"seller", "get", "987654321"}, {"seller", "get", "https://www.kleinanzeigen.de/s-bestandsliste.html?userId=987654321"}, {"seller", "search", "HÄNDLER ÄNNE", "--match", "exact"}, {"seller", "search", "änne"}, {"seller", "listings", "987654321"}} {
		value := run(0, args...)
		if value["completeness"] == "complete" {
			t.Fatalf("%v falsely claims complete seller coverage", args)
		}
	}
	if len(transport.calls) != before {
		t.Fatal("local seller lookup used network")
	}
	missing := run(4, "seller", "get", "9999999999")
	if missing["details"].(map[string]any)["reason"] != "not_in_local_index" {
		t.Fatal("seller miss claims global absence")
	}
	before = len(transport.calls)
	run(4, "listing", "get", "9999999999")
	if len(transport.calls) != before+1 {
		t.Fatal("volatile 404 was retried")
	}
	before = len(transport.calls)
	run(2, "search", "--radius", "10")
	run(2, "listing", "get", "../123")
	run(2, "seller", "get", "https://www.kleinanzeigen.de/s-bestandsliste.html?userId=987654321&extra=1")
	run(2, "seller", "get", "https://example.invalid/s-bestandsliste.html?userId=987654321")
	if len(transport.calls) != before {
		t.Fatal("invalid input reached network")
	}
}
