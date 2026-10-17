package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

func TestSellerObservationChronology(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	oldest := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	for i, offset := range []time.Duration{0, 100 * time.Millisecond, 100*time.Millisecond + time.Nanosecond} {
		id := []string{"1", "2", "3"}[i]
		observed := oldest.Add(offset)
		seller := SellerSnapshot{ID: id, FoldedName: "fixture", DisplayName: "Fixture", PublicJSON: json.RawMessage(`{}`), Source: "listing", Completeness: "direct", ObservedAt: observed}
		listing := SellerListingSnapshot{SellerID: id, ListingID: "42", URL: "https://www.kleinanzeigen.de/s-anzeige/fixture-" + id + "/42-1-1", ObservedAt: observed}
		switch i {
		case 0:
			err = db.UpsertPublicSeller(ctx, seller)
		case 1:
			err = db.SearchUpsertSellers(ctx, []SearchSellerRecord{{SellerID: id, ListingID: listing.ListingID, ListingURL: listing.URL, DisplayName: seller.DisplayName, PublicJSON: seller.PublicJSON, Source: seller.Source, Completeness: seller.Completeness, ObservedAt: observed}})
		case 2:
			err = db.UpsertSellerListing(ctx, seller, listing)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	newest := oldest.Add(100*time.Millisecond + time.Nanosecond)
	for _, match := range []string{"exact", "contains"} {
		matches, info, err := db.SearchSellers(ctx, "fixture", match, 1)
		if err != nil || len(matches) != 1 || matches[0].ID != "3" || !info.OldestObservedAt.Equal(oldest) || !info.NewestObservedAt.Equal(newest) {
			t.Errorf("%s: matches=%v info=%+v err=%v", match, matches, info, err)
		}
	}
	url, err := db.PublicListingURL(ctx, "42")
	if err != nil || url != "https://www.kleinanzeigen.de/s-anzeige/fixture-3/42-1-1" {
		t.Errorf("latest listing URL=%q err=%v", url, err)
	}
	// The exact 30-day boundary and newer snapshots survive; older ones do not.
	if err := db.UpsertPublicSeller(ctx, SellerSnapshot{ID: "4", FoldedName: "new", DisplayName: "New", PublicJSON: json.RawMessage(`{}`), Source: "public-web", Completeness: "direct", ObservedAt: oldest.Add(30*24*time.Hour + 100*time.Millisecond)}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SellerByID(ctx, "1"); !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expired whole-second observation retained: %v", err)
	}
	if _, err := db.SellerByID(ctx, "2"); err != nil {
		t.Errorf("boundary observation pruned: %v", err)
	}
	if _, err := db.SellerByID(ctx, "3"); err != nil {
		t.Errorf("newer observation pruned: %v", err)
	}
}
