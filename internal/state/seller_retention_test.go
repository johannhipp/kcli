package state

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestSearchSellerRetention(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	record := func(id string, observed time.Time) SearchSellerRecord {
		return SearchSellerRecord{SellerID: id, ListingID: id, DisplayName: id, PublicJSON: json.RawMessage(`{}`), Source: "search", Completeness: "best-effort", ObservedAt: observed}
	}
	if err := db.SearchUpsertSellers(ctx, []SearchSellerRecord{record("old", now.Add(-31*24*time.Hour))}); err != nil {
		t.Fatal(err)
	}
	if err := db.SearchUpsertSellers(ctx, []SearchSellerRecord{record("new", now)}); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM seller_listings WHERE seller_id='old'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("expired search seller and listing retained")
	}
	records := make([]SearchSellerRecord, listingSellerMaximum)
	for i := range records {
		records[i] = record(fmt.Sprint(i), now.Add(time.Second))
	}
	if err := db.SearchUpsertSellers(ctx, records); err != nil {
		t.Fatal(err)
	}
	info, err := db.SellerIndexInfo(ctx)
	if err != nil || info.Size != listingSellerMaximum {
		t.Fatalf("seller count=%d err=%v", info.Size, err)
	}
	if err := db.sql.QueryRowContext(ctx, `SELECT COUNT(*) FROM seller_listings`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != listingSellerMaximum {
		t.Fatalf("listing count=%d", count)
	}
}
