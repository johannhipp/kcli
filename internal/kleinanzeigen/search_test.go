package kleinanzeigen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSearchParsePageRedactedFixture(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", "search-page-0.redacted.json"))
	if err != nil {
		t.Fatal(err)
	}
	page, err := SearchParsePage(body)
	if err != nil {
		t.Fatal(err)
	}
	if !page.TotalKnown || page.Total != 4 {
		t.Fatalf("total = %d, known=%v", page.Total, page.TotalKnown)
	}
	if len(page.Listings) != 2 {
		t.Fatalf("listings = %d", len(page.Listings))
	}
	first := page.Listings[0]
	if first.Summary.Title != "ThinkPad & dock" || first.Summary.Price != "120.50" {
		t.Fatalf("first listing = %#v", first)
	}
	if first.Summary.PriceCents == nil || *first.Summary.PriceCents != 12050 {
		t.Fatalf("price cents = %v", first.Summary.PriceCents)
	}
	if first.Seller.ID != "200000000001" || first.Seller.Name != "Seller One" {
		t.Fatalf("seller = %#v", first.Seller)
	}
	if string(first.Seller.Raw) == "" || string(first.Seller.Raw) == "null" {
		t.Fatal("seller public snapshot was not retained")
	}
}

func TestSearchParsePageAcceptsSingletonAd(t *testing.T) {
	page, err := SearchParsePage([]byte(`{"ads":{"value":{"paging":{"numFound":"1"},"ad":{"id":"1","title":"one"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Listings) != 1 || page.Listings[0].Summary.ID != "1" {
		t.Fatalf("listings = %#v", page.Listings)
	}
}
