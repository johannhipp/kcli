package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type webAcceptanceRoundTripper func(*http.Request) (*http.Response, error)

func (f webAcceptanceRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// Exercise real argv, persisted state, the production web transport, website
// parsing, and normalized output. The HTTP boundary cannot leave this process.
func TestPublicWebCLIJourney(t *testing.T) {
	setPathEnvironment(t)
	form := `<astro-island component-url="/SearchForm.fixture.js" props='{"categories":[1,[[0,{"id":[0,"217"],"categoryName":[0,"Fahrräder"],"children":[1,[]]}]]],"searchUrl":[0,"/s-suchanfrage.html"]}'></astro-island>`
	filters := form + `<a href="/s-fahrraeder/c217+condition:used">Gebraucht</a>`
	results := `<astro-island component-url="/ImpressionTracker.fixture.js" props='{"resultAds":[1,[[0,{"organicAdPreview":[0,{"id":[0,"123456"],"title":[0,"City bike"],"price":[0,"120 €"],"seoLink":[0,"/s-anzeige/city-bike/123456-217-3331"],"userId":[0,"42"]}]}]]]}'></astro-island>`
	detail := `<h1 id="viewad-title">City bike</h1><h2 id="viewad-price">120 €</h2><span id="viewad-locality">10115 Berlin</span><p id="viewad-description-text">Public fixture description</p><div id="viewad-contact"><span class="userprofile-vip"><a href="/s-bestandsliste.html?userId=42">Example Seller</a></span></div><img itemprop="image" src="https://img.kleinanzeigen.de/fixture.jpg">`
	calls := 0
	original := http.DefaultTransport
	http.DefaultTransport = webAcceptanceRoundTripper(func(request *http.Request) (*http.Response, error) {
		calls++
		if request.Method != http.MethodGet || request.URL.Scheme != "https" || request.URL.Host != "www.kleinanzeigen.de" {
			t.Fatalf("unexpected public request: %s %s", request.Method, request.URL)
		}
		for _, header := range []string{"Authorization", "Cookie", "X-ECG-Authorization", "X-ECG-USER"} {
			if request.Header.Get(header) != "" {
				t.Fatalf("anonymous request included %s", header)
			}
		}
		if _, ok := request.Context().Deadline(); !ok {
			t.Fatal("public request has no deadline")
		}
		var body string
		switch request.URL.Path {
		case "/":
			body = form
		case "/s-ort-empfehlungen.json":
			if request.URL.Query().Get("query") != "Berlin" {
				t.Fatalf("wrong location query: %s", request.URL)
			}
			body = `{"_3331":"Berlin"}`
		case "/s-suchanfrage.html":
			if request.URL.Query().Get("keywords") == "bike" {
				if request.URL.Query().Get("categoryId") != "217" || request.URL.Query().Get("maxPrice") != "200" {
					t.Fatalf("search flags not translated: %s", request.URL)
				}
				body = results
			} else {
				body = filters
			}
		case "/s-anzeige/city-bike/123456-217-3331":
			body = detail
		default:
			t.Fatalf("unexpected website endpoint: %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = original })
	run := func(args ...string) map[string]any {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var stdout, stderr bytes.Buffer
		if code := Execute(ctx, args, strings.NewReader(""), &stdout, &stderr); code != 0 {
			t.Fatalf("%v: exit=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
		var result map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
			t.Fatalf("%v: invalid JSON: %v", args, err)
		}
		return result
	}
	categories := run("category", "list")["data"].([]any)
	if len(categories) != 1 || categories[0].(map[string]any)["id"] != "217" {
		t.Fatalf("category identities lost: %#v", categories)
	}
	locations := run("location", "resolve", "Berlin")["data"].([]any)
	if len(locations) != 1 || locations[0].(map[string]any)["id"] != "3331" {
		t.Fatalf("location identity lost: %#v", locations)
	}
	run("filter", "list", "--category", "217")
	before := calls
	cached := run("schema", "filters", "--category", "217")["data"].(map[string]any)
	encoded, _ := json.Marshal(cached)
	if calls != before || cached["overlay"] != "cached" || !strings.Contains(string(encoded), `"condition"`) || !strings.Contains(string(encoded), `"used"`) {
		t.Fatalf("filter schema did not use persisted website metadata: %s", encoded)
	}
	search := run("search", "bike", "--category", "217", "--max-price", "200", "--limit", "1")
	rows := search["data"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["id"] != "123456" || rows[0].(map[string]any)["source"] != "public-web" {
		t.Fatalf("website search normalization failed: %#v", search)
	}
	listing := run("listing", "get", "123456", "--raw")
	data := listing["data"].(map[string]any)
	if data["description"] != "Public fixture description" || listing["raw"] == nil {
		t.Fatalf("encountered ID lookup or raw detail failed: %#v", listing)
	}
	before = calls
	seller := run("seller", "get", "42")
	if calls != before || !strings.Contains(string(webAcceptanceJSON(t, seller)), "Example Seller") {
		t.Fatalf("seller detail did not survive a fresh CLI invocation: %#v", seller)
	}
	if calls != 5 {
		t.Fatalf("unexpected request count: got %d, want 5", calls)
	}
}

func webAcceptanceJSON(t *testing.T, value any) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
