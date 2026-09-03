package kleinanzeigen

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/state"
)

type fixtureTransport struct {
	bodies map[string][]byte
	calls  map[string]int
	status map[string]int
}

func (f *fixtureTransport) Do(request Request) (Response, error) {
	f.calls[request.Path]++
	status := f.status[request.Path]
	if status == 0 {
		status = 200
	}
	return Response{StatusCode: status, Body: append([]byte(nil), f.bodies[request.Path]...)}, nil
}

type fixedMetadataClock struct{ now time.Time }

func (c *fixedMetadataClock) Now() time.Time { return c.now }

func apiFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMetadataFixtureParsingAndClassification(t *testing.T) {
	categories, warnings, err := ParseCategories(apiFixture(t, "categories.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || len(categories) != 3 || categories[0].ID != "278" || categories[0].Label != "Fahrräder & Zubehör" || categories[1].Path != "Fahrräder & Zubehör/Fahrradteile" || !strings.Contains(string(categories[0].Raw), "unknown-category-field") {
		t.Fatalf("categories=%#v warnings=%#v", categories, warnings)
	}
	locations, warnings, err := ParseLocations(apiFixture(t, "locations.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || len(locations) != 3 || locations[0].ID != "10" || locations[1].Label != "Berlin-Mitte" {
		t.Fatalf("locations=%#v warnings=%#v", locations, warnings)
	}
	filters, warnings, err := ParseFilters("278", apiFixture(t, "search-metadata/278.json"))
	if err != nil {
		t.Fatal(err)
	}
	classes := map[string]string{}
	for _, filter := range filters {
		classes[filter.Key] = filter.Classification
	}
	if len(warnings) != 0 || classes["condition"] != "accepted" || classes["features"] != "provisional" || classes["legacy"] != "unsupported-upstream" {
		t.Fatalf("filters=%#v warnings=%#v", filters, warnings)
	}
	unknown, _, err := ParseFilters("999", apiFixture(t, "search-metadata/999.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(unknown) != 1 || unknown[0].Classification != "unsupported-client" || !strings.Contains(string(unknown[0].Raw), "future-field") {
		t.Fatalf("unknown filter=%#v", unknown)
	}
}

func TestMetadataParsingRejectsInvalidIDsAndRetainsPartialWarnings(t *testing.T) {
	if _, _, err := ParseFilters("../../secret", apiFixture(t, "search-metadata/278.json")); err == nil {
		t.Fatal("invalid category ID accepted")
	}
	input := []byte(`{"category":[{"id":"278","localized-name":"Valid"},{"localized-name":"Missing ID"}]}`)
	items, warnings, err := ParseCategories(input)
	if err != nil || len(items) != 1 || len(warnings) != 1 {
		t.Fatalf("partial parse items=%#v warnings=%#v err=%v", items, warnings, err)
	}
}

func TestMetadataCacheTTLAndExplicitRefresh(t *testing.T) {
	ctx := context.Background()
	database, err := state.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	transport := &fixtureTransport{bodies: map[string][]byte{
		"/api/categories.json":              apiFixture(t, "categories.json"),
		"/api/locations.json":               apiFixture(t, "locations.json"),
		"/api/ads/search-metadata/278.json": apiFixture(t, "search-metadata/278.json"),
	}, calls: map[string]int{}, status: map[string]int{}}
	clock := &fixedMetadataClock{now: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)}
	service := NewMetadataService(transport, database)
	service.clock = clock
	if _, err := service.Categories(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Categories(ctx, false); err != nil {
		t.Fatal(err)
	}
	if transport.calls["/api/categories.json"] != 1 {
		t.Fatalf("fresh categories refetched: %d", transport.calls["/api/categories.json"])
	}
	if _, err := service.Categories(ctx, true); err != nil {
		t.Fatal(err)
	}
	if transport.calls["/api/categories.json"] != 2 {
		t.Fatalf("explicit refresh ignored: %d", transport.calls["/api/categories.json"])
	}
	if _, err := service.Locations(ctx, "  BERLIN ", 2); err != nil {
		t.Fatal(err)
	}
	if result, err := service.Locations(ctx, "berlin", 2); err != nil || result.Source != "cache" {
		t.Fatalf("location cache miss: %#v %v", result, err)
	}
	if transport.calls["/api/locations.json"] != 1 {
		t.Fatalf("fresh location refetched: %d", transport.calls["/api/locations.json"])
	}
	if _, err := service.Filters(ctx, "278", false); err != nil {
		t.Fatal(err)
	}
	if result, err := service.Filters(ctx, "Fahrräder & Zubehör", false); err != nil || result.Source != "cache" {
		t.Fatalf("filter cache miss: %#v %v", result, err)
	}
	clock.now = clock.now.Add(8 * 24 * time.Hour)
	if _, err := service.Locations(ctx, "Berlin", 2); err != nil {
		t.Fatal(err)
	}
	if transport.calls["/api/locations.json"] != 2 {
		t.Fatalf("expired location not refreshed: %d", transport.calls["/api/locations.json"])
	}
	if _, err := service.Categories(ctx, false); err != nil {
		t.Fatal(err)
	}
	if transport.calls["/api/categories.json"] != 3 {
		t.Fatalf("expired categories not refreshed: %d", transport.calls["/api/categories.json"])
	}
}

func TestMobileLocationFailureHasNoFallback(t *testing.T) {
	ctx := context.Background()
	database, err := state.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	transport := &fixtureTransport{bodies: map[string][]byte{}, calls: map[string]int{}, status: map[string]int{"/api/locations.json": 503}}
	service := NewMetadataService(transport, database)
	_, err = service.Locations(ctx, "Berlin", 10)
	if err == nil || transport.calls["/api/locations.json"] != 1 || len(transport.calls) != 1 {
		t.Fatalf("unexpected fallback behavior: err=%v calls=%#v", err, transport.calls)
	}
}
