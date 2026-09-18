package app

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/state"
)

type searchTestTransport struct {
	base    string
	client  *http.Client
	mu      sync.Mutex
	queries []url.Values
}

func (t *searchTestTransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	endpoint, err := url.Parse(t.base + request.Path)
	if err != nil {
		return kleinanzeigen.Response{}, err
	}
	values := endpoint.Query()
	for key, entries := range request.Query {
		for _, value := range entries {
			values.Add(key, value)
		}
	}
	endpoint.RawQuery = values.Encode()
	httpRequest, err := http.NewRequestWithContext(request.Context, request.Method, endpoint.String(), nil)
	if err != nil {
		return kleinanzeigen.Response{}, err
	}
	response, err := t.client.Do(httpRequest)
	if err != nil {
		return kleinanzeigen.Response{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return kleinanzeigen.Response{}, err
	}
	if request.Path == "/api/ads.json" {
		t.mu.Lock()
		t.queries = append(t.queries, values)
		t.mu.Unlock()
	}
	return kleinanzeigen.Response{StatusCode: response.StatusCode, Headers: map[string][]string(response.Header), Body: body}, nil
}

func TestSearchPaginatesDeduplicatesExcludesAndIndexesSellers(t *testing.T) {
	fixtures := filepath.Join("..", "..", "testdata", "api")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		var fixture string
		switch request.URL.Path {
		case "/api/categories.json":
			fixture = "categories.json"
		case "/api/locations.json":
			fixture = "locations.json"
		case "/api/ads/search-metadata/278.json":
			fixture = filepath.Join("search-metadata", "278.json")
		case "/api/ads.json":
			page, _ := strconv.Atoi(request.URL.Query().Get("page"))
			fixture = "search-page-" + strconv.Itoa(page) + ".redacted.json"
		default:
			http.NotFound(response, request)
			return
		}
		body, err := os.ReadFile(filepath.Join(fixtures, fixture))
		if err != nil {
			http.Error(response, err.Error(), http.StatusInternalServerError)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write(body)
	}))
	defer server.Close()

	statePath := filepath.Join(t.TempDir(), "state.sqlite")
	database, err := state.Open(context.Background(), statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	transport := &searchTestTransport{base: server.URL, client: server.Client()}
	application := New(Dependencies{Transport: transport, State: database})
	result, err := application.Search(context.Background(), domain.SearchInputV1{
		Query:           "ThinkPad",
		Category:        "Fahrräder & Zubehör",
		Location:        "Berlin",
		Radius:          25,
		MinPrice:        "10.50",
		MaxPrice:        "200",
		PictureRequired: true,
		Filters:         []domain.FilterValueV1{{Key: "condition", Value: "used"}, {Key: "features", Value: "light"}, {Key: "features", Value: "compact"}},
		Exclusions:      []string{"BROKEN"},
		PageSize:        2,
		Paginate:        true,
		Limit:           2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 2 || result.Data[0].ID != "100000000001" || result.Data[1].ID != "100000000003" {
		t.Fatalf("results = %#v", result.Data)
	}
	for _, listing := range result.Data {
		if listing.Status == "" || listing.Source != "mobile-api" || listing.Completeness != domain.CompletenessBestEffort || listing.ObservedAt.IsZero() {
			t.Fatalf("search listing metadata = %#v", listing)
		}
	}
	if result.Page == nil || result.Page.Fetched != 4 || result.Page.Returned != 2 {
		t.Fatalf("page = %#v", result.Page)
	}
	if len(transport.queries) != 2 {
		t.Fatalf("search requests = %d", len(transport.queries))
	}
	firstQuery := transport.queries[0]
	for key, want := range map[string]string{"page": "0", "size": "2", "categoryId": "278", "locationId": "10", "distance": "25", "minPrice": "10.50", "maxPrice": "200", "adType": "OFFERED", "pictureRequired": "true", "sortType": "DATE_DESCENDING", "condition": "used"} {
		if got := firstQuery.Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}
	if got := firstQuery["features"]; len(got) != 2 || got[0] != "light" || got[1] != "compact" {
		t.Errorf("features = %#v", got)
	}
	if got := searchStopReason(result.Warnings); got != "bound" {
		t.Fatalf("stop reason = %q", got)
	}

	inspection, err := sql.Open("sqlite", (&url.URL{Scheme: "file", Path: filepath.ToSlash(statePath)}).String())
	if err != nil {
		t.Fatal(err)
	}
	defer inspection.Close()
	var sellers, links int
	if err := inspection.QueryRow(`SELECT COUNT(*) FROM sellers`).Scan(&sellers); err != nil {
		t.Fatal(err)
	}
	if err := inspection.QueryRow(`SELECT COUNT(*) FROM seller_listings`).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if sellers != 3 || links != 3 {
		t.Fatalf("seller index counts = sellers:%d links:%d", sellers, links)
	}
}

func TestSearchValidationIsTypedAndPrecedesNetwork(t *testing.T) {
	transport := &searchCountingTransport{}
	application := New(Dependencies{Transport: transport})
	cases := []domain.SearchInputV1{
		{Page: -1},
		{PageSize: 26},
		{Paginate: true, Limit: 1001},
		{MinPrice: "2", MaxPrice: "1"},
		{Radius: 10},
		{Sort: "distance-asc"},
		{Exclusions: []string{"\n"}},
		{Filters: []domain.FilterValueV1{{Key: "minPrice", Value: "1"}}},
	}
	for _, input := range cases {
		_, err := application.Search(context.Background(), input)
		var typed *domain.Error
		if !errors.As(err, &typed) || typed.Code != domain.CodeInvalidInput || domain.ExitCode(err) != 2 {
			t.Errorf("input %#v: error = %#v", input, err)
		}
	}
	if transport.calls != 0 {
		t.Fatalf("network calls = %d", transport.calls)
	}
}

func TestSearchBuildQuerySerializesCommonFiltersAndSorts(t *testing.T) {
	sortCases := map[string]string{
		"date-desc":    "DATE_DESCENDING",
		"price-asc":    "PRICE_ASCENDING",
		"price-desc":   "PRICE_DESCENDING",
		"distance-asc": "DISTANCE_ASCENDING",
	}
	for inputSort, wireSort := range sortCases {
		input := domain.SearchInputV1{Query: "bike", Category: "278", Location: "10", Radius: 50, MinPrice: "1.25", MaxPrice: "99", AdType: "wanted", PictureRequired: true, Sort: inputSort, PageSize: 25, Limit: 25}
		query, _, err := searchBuildQuery(context.Background(), nil, &input)
		if err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{"q": "bike", "categoryId": "278", "locationId": "10", "distance": "50", "minPrice": "1.25", "maxPrice": "99", "adType": "WANTED", "pictureRequired": "true", "sortType": wireSort, "size": "25"} {
			if got := query[key]; len(got) != 1 || got[0] != want {
				t.Errorf("sort %s query %s = %#v, want %q", inputSort, key, got, want)
			}
		}
	}
}

func TestSearchRejectsUnsupportedMetadataBeforeAdsRequest(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", "search-metadata", "999.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/ads/search-metadata/999.json" {
			http.NotFound(response, request)
			return
		}
		_, _ = response.Write(fixture)
	}))
	defer server.Close()
	database, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.ReplaceCategories(context.Background(), []state.CategorySnapshot{{ID: "999", Path: "Future", Label: "Future", RawJSON: []byte(`{"id":"999"}`), ObservedAt: New(Dependencies{}).Clock.Now()}}); err != nil {
		t.Fatal(err)
	}
	transport := &searchTestTransport{base: server.URL, client: server.Client()}
	application := New(Dependencies{Transport: transport, State: database})
	_, err = application.Search(context.Background(), domain.SearchInputV1{Category: "999", Filters: []domain.FilterValueV1{{Key: "future-shape", Value: "x"}}})
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeInvalidInput || domain.ExitCode(err) != 2 {
		t.Fatalf("error = %#v", err)
	}
	if len(transport.queries) != 0 {
		t.Fatalf("ads requests = %d", len(transport.queries))
	}
}

func TestSearchPaginationStopReasons(t *testing.T) {
	cases := []struct {
		name       string
		bodies     [][]byte
		wantReason string
		wantCalls  int
	}{
		{name: "known total", bodies: [][]byte{[]byte(`{"ads":{"paging":{"numFound":"1"},"ad":{"id":"1","title":"one"}}}`)}, wantReason: "known_total", wantCalls: 1},
		{name: "empty page", bodies: [][]byte{[]byte(`{"ads":{"paging":{}}}`)}, wantReason: "empty_page", wantCalls: 1},
		{name: "two duplicate pages", bodies: [][]byte{
			[]byte(`{"ads":{"paging":{},"ad":{"id":"1","title":"one"}}}`),
			[]byte(`{"ads":{"paging":{},"ad":{"id":"1","title":"one"}}}`),
			[]byte(`{"ads":{"paging":{},"ad":{"id":"1","title":"one"}}}`),
		}, wantReason: "two_pages_no_new_ids", wantCalls: 3},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			database, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "state.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			transport := &searchScriptedTransport{bodies: testCase.bodies}
			application := New(Dependencies{Transport: transport, State: database})
			result, err := application.Search(context.Background(), domain.SearchInputV1{Paginate: true, PageSize: 1, Limit: 2})
			if err != nil {
				t.Fatal(err)
			}
			if got := searchStopReason(result.Warnings); got != testCase.wantReason {
				t.Fatalf("stop reason = %q, want %q", got, testCase.wantReason)
			}
			if transport.calls != testCase.wantCalls {
				t.Fatalf("calls = %d, want %d", transport.calls, testCase.wantCalls)
			}
		})
	}
}

type searchScriptedTransport struct {
	bodies [][]byte
	calls  int
}

func (t *searchScriptedTransport) Do(kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	if t.calls >= len(t.bodies) {
		return kleinanzeigen.Response{}, errors.New("unexpected extra request")
	}
	body := t.bodies[t.calls]
	t.calls++
	return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: body}, nil
}

type searchCountingTransport struct{ calls int }

func (t *searchCountingTransport) Do(kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	t.calls++
	return kleinanzeigen.Response{}, errors.New("unexpected network call")
}

func searchStopReason(warnings []domain.WarningV1) string {
	for _, warning := range warnings {
		if warning.Code == "search_metadata" {
			value, _ := warning.Details["stop_reason"].(string)
			return value
		}
	}
	return ""
}

func TestNumericLocationReferenceDoesNotQueryTextSuggestions(t *testing.T) {
	result, err := searchResolveLocation(context.Background(), nil, "3331")
	if err != nil || result.Data.ID != "3331" {
		t.Fatalf("ID was not preserved: %#v, %v", result, err)
	}
}
