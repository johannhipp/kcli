package state

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestSellerIndexUpsertSearchAndKnownListings(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	seller := SellerSnapshot{ID: "987", FoldedName: "händler änne", DisplayName: "Händler Änne", PublicJSON: json.RawMessage(`{"rating":"4.9"}`), Source: "listing", Completeness: "direct", ObservedAt: now}
	listing := SellerListingSnapshot{SellerID: "987", ListingID: "123", Title: "Fixture", Status: "available", URL: "https://www.kleinanzeigen.de/s-anzeige/x/123-1-1", ObservedAt: now}
	if err := database.UpsertSellerListing(ctx, seller, listing); err != nil {
		t.Fatal(err)
	}
	got, err := database.SellerByID(ctx, "987")
	if err != nil || got.DisplayName != seller.DisplayName || got.ObservedAt != now {
		t.Fatalf("seller=%#v err=%v", got, err)
	}
	matches, info, err := database.SearchSellers(ctx, "ändler", "contains", 25)
	if err != nil || len(matches) != 1 || info.Size != 1 || info.OldestObservedAt != now || info.NewestObservedAt != now {
		t.Fatalf("matches=%#v info=%#v err=%v", matches, info, err)
	}
	if matches, _, err := database.SearchSellers(ctx, "%", "contains", 25); err != nil || len(matches) != 0 {
		t.Fatalf("LIKE wildcard was not escaped: matches=%#v err=%v", matches, err)
	}
	known, err := database.SellerListings(ctx, "987", 25)
	if err != nil || len(known) != 1 || known[0].Status != "available" {
		t.Fatalf("known=%#v err=%v", known, err)
	}
}

func TestSellerIndexExpiresOldSnapshots(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	insert := func(id string, observed time.Time) {
		t.Helper()
		err := database.UpsertSellerListing(ctx, SellerSnapshot{ID: id, FoldedName: id, DisplayName: id, PublicJSON: json.RawMessage(`{}`), Source: "listing", Completeness: "direct", ObservedAt: observed}, SellerListingSnapshot{SellerID: id, ListingID: id, Title: id, Status: "available", URL: "https://www.kleinanzeigen.de/s-anzeige/x/" + id + "-1-1", ObservedAt: observed})
		if err != nil {
			t.Fatal(err)
		}
	}
	insert("111", now.Add(-31*24*time.Hour))
	insert("222", now)
	info, err := database.SellerIndexInfo(ctx)
	if err != nil || info.Size != 1 {
		t.Fatalf("expired seller retained: info=%#v err=%v", info, err)
	}
}
