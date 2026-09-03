package state

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestSearchUpsertSellersIndexesAndUpdatesListing(t *testing.T) {
	database, err := Open(context.Background(), filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	record := SearchSellerRecord{SellerID: "seller-1", DisplayName: "Ｓeller Name", PublicJSON: json.RawMessage(`{"user-id":"seller-1"}`), Source: "search", Completeness: "best-effort", ListingID: "listing-1", ListingTitle: "First", ListingStatus: "available", ListingURL: "https://www.kleinanzeigen.de/s-anzeige/redacted/listing-1", ObservedAt: now}
	if err := database.SearchUpsertSellers(context.Background(), []SearchSellerRecord{record}); err != nil {
		t.Fatal(err)
	}
	record.ListingTitle = "Updated"
	if err := database.SearchUpsertSellers(context.Background(), []SearchSellerRecord{record}); err != nil {
		t.Fatal(err)
	}
	var folded, title string
	if err := database.sql.QueryRow(`SELECT folded_name FROM sellers WHERE seller_id=?`, record.SellerID).Scan(&folded); err != nil {
		t.Fatal(err)
	}
	if err := database.sql.QueryRow(`SELECT title FROM seller_listings WHERE seller_id=? AND listing_id=?`, record.SellerID, record.ListingID).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if folded != "seller name" || title != "Updated" {
		t.Fatalf("folded=%q title=%q", folded, title)
	}
}
