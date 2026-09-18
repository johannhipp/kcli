package kleinanzeigen

import (
	"context"
	"encoding/json"
	"html"
	"io"
	"net/http"
	"path/filepath"

	"github.com/johannhipp/kcli/internal/state"
	"strings"
	"testing"
	"time"
)

func webTestIsland(component string, props map[string]any) string {
	var tag func(any) any
	tag = func(value any) any {
		switch v := value.(type) {
		case map[string]any:
			out := map[string]any{}
			for key, item := range v {
				out[key] = tag(item)
			}
			return []any{0, out}
		case []any:
			out := []any{}
			for _, item := range v {
				out = append(out, tag(item))
			}
			return []any{1, out}
		default:
			return []any{0, value}
		}
	}
	tagged := map[string]any{}
	for key, value := range props {
		tagged[key] = tag(value)
	}
	raw, _ := json.Marshal(tagged)
	return `<astro-island component-url="/` + component + `.js" props="` + html.EscapeString(string(raw)) + `"></astro-island>`
}
func webTestSearchPage() string {
	ad := map[string]any{"id": 12345, "title": "Fixture bicycle", "description": "Public description", "price": "1.250,50 € VB", "seoLink": "/s-anzeige/fixture/12345-217-3331", "userId": 987}
	return webTestIsland("ImpressionTracker.fixture", map[string]any{"resultAds": []any{map[string]any{"organicAdPreview": ad}, map[string]any{"organicAdPreview": ad}}}) + `<a href="/s-fahrrad/seite:2/k0">2</a>`
}
func TestWebSearchPublicMappingAndPagination(t *testing.T) {
	raw, pages, err := parseWebSearch([]byte(webTestSearchPage()))
	if err != nil {
		t.Fatal(err)
	}
	page, err := SearchParsePage(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Listings) != 1 || page.Listings[0].Summary.Price != "1250.50" || page.Listings[0].Summary.Status != "unknown" || page.TotalKnown {
		t.Fatalf("page=%+v", page)
	}
	if pages[1] != publicWebOrigin+"/s-fahrrad/seite:2/k0" {
		t.Fatal(pages)
	}
	if _, _, err := parseWebSearch([]byte(`<html>changed contract</html>`)); err == nil {
		t.Fatal("accepted missing search contract")
	}
	empty := webTestIsland("ImpressionTracker.fixture", map[string]any{"resultAds": []any{}})
	raw, _, err = parseWebSearch([]byte(empty))
	if err != nil {
		t.Fatal(err)
	}
	page, err = SearchParsePage(raw)
	if err != nil || len(page.Listings) != 0 {
		t.Fatal(page, err)
	}
}
func TestWebMetadataMapping(t *testing.T) {
	body := webTestIsland("SearchForm.fixture", map[string]any{"searchUrl": publicWebOrigin + "/s-suchanfrage.html", "categories": []any{map[string]any{"id": 210, "categoryName": "Auto & Rad", "children": []any{map[string]any{"id": 217, "categoryName": "Fahrräder", "children": []any{}}}}}})
	body += webTestIsland("Range.fixture", map[string]any{"rangeItem": map[string]any{"attributeId": "autos.km_i", "attributeType": "INT"}, "attributeName": "Kilometer"})
	body += `<a href="/s-fahrraeder/c217+fahrraeder.type_s:city">City</a><input name="clickableOptions" type="checkbox" value="autos.navi_b">`
	raw, err := parseWebCategories([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	categories, _, err := ParseCategories(raw)
	if err != nil || len(categories) != 2 || categories[1].ParentID != "210" {
		t.Fatal(categories, err)
	}
	raw, err = parseWebFilters([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	filters, _, err := ParseFilters("217", raw)
	if err != nil || len(filters) != 3 {
		t.Fatal(filters, err)
	}
	for _, filter := range filters {
		if filter.Classification != "accepted" || filter.Proof != "public-web-advertised" {
			t.Fatal(filter)
		}
	}
	raw, err = parseWebLocations([]byte(`{"_3331":"Berlin","0":"Deutschland"}`))
	if err != nil {
		t.Fatal(err)
	}
	locations, _, err := ParseLocations(raw)
	if err != nil || len(locations) != 2 {
		t.Fatal(locations, err)
	}
	raw, err = parseWebLocations([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	locations, _, err = ParseLocations(raw)
	if err != nil || len(locations) != 0 {
		t.Fatal(locations, err)
	}
}

type webRoundTrip func(*http.Request) (*http.Response, error)

func (f webRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type webClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *webClock) Now() time.Time { return c.now }
func (c *webClock) Sleep(_ context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}
func TestWebTransportPacesRedirectsAndStopsOnChallenge(t *testing.T) {
	transport := NewWebTransport(nil)
	clock := &webClock{now: time.Unix(1000, 0)}
	transport.clock = clock
	requests := 0
	transport.client.Transport = webRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		for _, header := range []string{"Authorization", "Cookie", "X-EBAYK-APP"} {
			if r.Header.Get(header) != "" {
				t.Fatal("credential header", header)
			}
		}
		if requests == 1 {
			return &http.Response{StatusCode: 302, Header: http.Header{"Location": {"/s-fahrrad/k0"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("verify you are human"))}, nil
	})
	_, _, err := transport.fetch(context.Background(), publicWebOrigin+"/")
	if err == nil || requests != 2 || len(clock.sleeps) != 1 || clock.sleeps[0] < 2500*time.Millisecond {
		t.Fatal(err, requests, clock.sleeps)
	}
	requests = 0
	transport.client.Transport = webRoundTrip(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 429, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("rate limited"))}, nil
	})
	_, _, err = transport.fetch(context.Background(), publicWebOrigin+"/")
	if err == nil || requests != 1 {
		t.Fatal("retried rate limit", requests, err)
	}
}
func TestWebTransportRejectsUntrustedRoutes(t *testing.T) {
	transport := NewWebTransport(nil)
	for _, raw := range []string{"https://evil.example/", "https://www.kleinanzeigen.de.evil.example/", "http://www.kleinanzeigen.de/", "https://x@www.kleinanzeigen.de/"} {
		if _, err := webURL(raw); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	if _, err := transport.Do(Request{Method: "POST", Host: HostMain, Path: "/api/ads.json"}); err == nil {
		t.Fatal("accepted mutation")
	}
	if _, err := transport.Do(Request{Method: "GET", Host: HostMain, Path: "/api/ads/123.json"}); err == nil {
		t.Fatal("invented listing URL")
	}
}

func TestWebLegacyInventoryAndExplicitEmpty(t *testing.T) {
	body := `<ul id="page-searchresults-adtable"><li><article class="aditem" data-adid="12345" data-href="/s-anzeige/fixture/12345-217-3331"><h2>Fixture bicycle</h2><p class="aditem-main--middle--description">Description</p><p class="aditem-main--middle--price-shipping--price">120 € VB</p></article></li></ul>`
	raw, _, err := parseWebSearch([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	page, err := SearchParsePage(raw)
	if err != nil || len(page.Listings) != 1 || page.Listings[0].Summary.Price != "120" {
		t.Fatal(page, err)
	}
	raw, _, err = parseWebSearch([]byte(`<section id="saved-search-empty-result">Keine Ergebnisse</section>`))
	if err != nil {
		t.Fatal(err)
	}
	page, err = SearchParsePage(raw)
	if err != nil || len(page.Listings) != 0 {
		t.Fatal(page, err)
	}
}

func TestWebSearchFormVerifiedFilters(t *testing.T) {
	ctx := context.Background()
	database, err := state.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	fixtures := []state.FilterSnapshot{
		{CategoryID: "216", Key: "autos.km_i", RawJSON: json.RawMessage(`{"web-kind":"range","web-value-type":"INT"}`), ObservedAt: time.Now()},
		{CategoryID: "216", Key: "autos.navi_b", RawJSON: json.RawMessage(`{"web-kind":"boolean"}`), ObservedAt: time.Now()},
		{CategoryID: "216", Key: "autos.marke_s", RawJSON: json.RawMessage(`{"web-kind":"enum","supported-value":[{"value":"bmw"}]}`), ObservedAt: time.Now()},
	}
	if err := database.ReplaceFilters(ctx, "216", fixtures); err != nil {
		t.Fatal(err)
	}
	transport := NewWebTransport(database)
	query := map[string][]string{"q": {"car"}, "categoryId": {"216"}, "autos.km_i": {"10000,50000"}, "autos.navi_b": {"true"}, "autos.marke_s": {"bmw"}, "adType": {"OFFERED"}, "sortType": {"PRICE_ASCENDING"}}
	form, suffixes, err := transport.searchForm(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	if values := form["attributeMap[autos.km_i]"]; len(values) != 2 || values[0] != "10000" || values[1] != "50000" {
		t.Fatal(form)
	}
	if form.Get("clickableOptions") != "autos.navi_b" || form.Get("sortingField") != "PRICE_AMOUNT" || form.Get("adType") != "OFFER" || len(suffixes) != 1 || suffixes[0] != "+autos.marke_s:bmw" {
		t.Fatal(form, suffixes)
	}
	for _, invalid := range []string{"50000,10000", "1.5,3", "10000", "1,2,3", ","} {
		query["autos.km_i"] = []string{invalid}
		if _, _, err := transport.searchForm(ctx, query); err == nil {
			t.Fatal("accepted invalid range", invalid)
		}
	}
	query["autos.km_i"] = []string{"10000,"}
	query["autos.marke_s"] = []string{"unadvertised"}
	if _, _, err := transport.searchForm(ctx, query); err == nil {
		t.Fatal("accepted unadvertised enum")
	}
}

type webRateRecorder struct {
	until time.Time
	host  string
}

func (r *webRateRecorder) ReserveRateSlot(context.Context, string, time.Time, time.Duration, time.Duration) (time.Duration, error) {
	return 0, nil
}
func (r *webRateRecorder) MoveRateSlot(_ context.Context, host string, until time.Time) error {
	r.host = host
	r.until = until
	return nil
}
func TestWebRateCooldownAndWebsiteDiagnostics(t *testing.T) {
	for _, media := range []bool{false, true} {
		transport := NewWebTransport(nil)
		clock := &webClock{now: time.Unix(1000, 0)}
		transport.clock = clock
		rate := &webRateRecorder{}
		transport.rate = rate
		transport.client.Transport = webRoundTrip(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 429, Header: http.Header{"Retry-After": {"120"}}, Body: io.NopCloser(strings.NewReader("private page body"))}, nil
		})
		var err error
		if media {
			raw := "https://img.kleinanzeigen.de/fixture.jpg"
			if e := transport.AllowMediaURL(raw); e != nil {
				t.Fatal(e)
			}
			_, err = transport.OpenMedia(Request{Host: HostMedia, Method: "GET", AbsoluteURL: raw})
		} else {
			_, _, err = transport.fetch(context.Background(), publicWebOrigin+"/")
		}
		if err == nil || strings.Contains(err.Error(), "mobile") || !strings.Contains(err.Error(), "website") || rate.host != "www.kleinanzeigen.de" || rate.until.Sub(clock.now) != 120*time.Second {
			t.Fatal(err, rate)
		}
	}
}
func TestWebSearchRequestAndEndOfPagination(t *testing.T) {
	for _, pageNumber := range []string{"0", "1"} {
		transport := NewWebTransport(nil)
		transport.clock = &webClock{now: time.Unix(1000, 0)}
		calls := 0
		transport.client.Transport = webRoundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			body := `<section id="saved-search-empty-result">Keine Ergebnisse</section>`
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})
		response, err := transport.Do(Request{Host: HostMain, Method: "GET", Path: "/api/ads.json", Query: map[string][]string{"page": {pageNumber}}})
		if err != nil || calls != 1 {
			t.Fatal(pageNumber, calls, err)
		}
		page, err := SearchParsePage(response.Body)
		if err != nil || len(page.Listings) != 0 {
			t.Fatal(page, err)
		}
	}
	malformed := webTestIsland("ImpressionTracker.fixture", map[string]any{"resultAds": map[string]any{}})
	if _, _, err := parseWebSearch([]byte(malformed)); err == nil {
		t.Fatal("accepted malformed collection")
	}
}
func TestWebListingCanonicalizesBareHost(t *testing.T) {
	transport := NewWebTransport(nil)
	transport.client.Transport = webRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "www.kleinanzeigen.de" {
			t.Fatal(r.URL.Host)
		}
		return &http.Response{StatusCode: 404, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	response, err := transport.Do(Request{Host: HostMain, Method: "GET", Path: "/api/ads/12345.json", AbsoluteURL: "https://kleinanzeigen.de/s-anzeige/fixture/12345-217-3331"})
	if err != nil || response.StatusCode != 404 {
		t.Fatal(response.StatusCode, err)
	}
}

func TestWebFilterStateRejectsSilentlyDroppedFilters(t *testing.T) {
	body := webTestIsland("SearchForm.fixture", map[string]any{"searchUrl": publicWebOrigin + "/s-suchanfrage.html", "attributeMap": map[string]any{"autos.km_i": "10000,50000", "autos.tuevy_i": "2027"}, "nonRangeAttributeMap": map[string]any{"autos.marke_s": "bmw"}, "globalFilters": map[string]any{}, "clickableOptions": []any{"autos.navi_b"}})
	query := map[string][]string{"categoryId": {"216"}, "autos.km_i": {"10000,50000"}, "autos.tuevy_i": {"2027,"}, "autos.marke_s": {"bmw"}, "autos.navi_b": {"true"}}
	if err := validateWebFilterState([]byte(body), query); err != nil {
		t.Fatal(err)
	}
	query["autos.marke_s"] = []string{"audi"}
	if err := validateWebFilterState([]byte(body), query); err == nil {
		t.Fatal("accepted silently changed filter")
	}
}

func TestWebFilterStateObservedConditionAlias(t *testing.T) {
	body := webTestIsland("SearchForm.fixture", map[string]any{"searchUrl": publicWebOrigin + "/s-suchanfrage.html", "nonRangeAttributeMap": map[string]any{"fahrraeder.type_s": "city"}, "globalFilters": map[string]any{"global.condition": "ok"}})
	query := map[string][]string{"fahrraeder.type_s": {"city"}, "global.zustand": {"ok"}}
	if err := validateWebFilterState([]byte(body), query); err != nil {
		t.Fatal(err)
	}
	query["global.zustand"] = []string{"new"}
	if err := validateWebFilterState([]byte(body), query); err == nil {
		t.Fatal("accepted mismatched condition alias value")
	}
	delete(query, "global.zustand")
	query["global.unknown"] = []string{"ok"}
	if err := validateWebFilterState([]byte(body), query); err == nil {
		t.Fatal("inferred an unverified alias")
	}
}
