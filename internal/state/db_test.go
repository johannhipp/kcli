package state

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pressly/goose/v3"
)

func TestOpenMigratesAndConfiguresSQLite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "profile", "state.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	names, err := db.SchemaTables(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"meta", "rate_slots", "category_snapshots", "filter_snapshots", "location_cache", "sellers", "seller_listings", "conversations", "messages", "events", "cursor_heads", "confirmation_plans", "leases"} {
		if !contains(names, want) {
			t.Errorf("missing table %s in %v", want, names)
		}
	}
	var busy, foreign, synchronous int
	if err := db.SQL().QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busy); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreign); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
		t.Fatal(err)
	}
	if busy != 5000 || foreign != 1 || synchronous != 2 {
		t.Fatalf("pragmas busy=%d foreign=%d synchronous=%d", busy, foreign, synchronous)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("state permissions: %v, %v", info, err)
	}
}
func TestTransactionRollbackAndLease(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sentinel := errors.New("rollback")
	err = db.WithTx(ctx, func(_ *sql.Tx, q *Queries) error {
		if err := q.SetMeta(ctx, "rolled_back", "value"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("WithTx error = %v", err)
	}
	if _, err := db.Queries().GetMeta(ctx, "rolled_back"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("rollback left value: %v", err)
	}
	acquired, err := db.AcquireLease(ctx, "refresh", "owner-a", time.Minute)
	if err != nil || !acquired {
		t.Fatalf("first acquire = %v, %v", acquired, err)
	}
	acquired, err = db.AcquireLease(ctx, "refresh", "owner-b", time.Minute)
	if err != nil || acquired {
		t.Fatalf("contended acquire = %v, %v", acquired, err)
	}
	if err := db.ReleaseLease(ctx, "refresh", "owner-a"); err != nil {
		t.Fatal(err)
	}
}
func TestCorruptDatabaseIsPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	original := []byte("not-a-sqlite-database")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if db, err := Open(context.Background(), path); err == nil {
		db.Close()
		t.Fatal("corrupt database unexpectedly opened")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("corrupt database was modified: %q", got)
	}
}

func TestUpgradePreservesSellerObservationPrecision(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	legacy, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer legacy.Close()
	migrations, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	provider, err := goose.NewProvider(goose.DialectSQLite3, legacy, migrations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 4); err != nil {
		t.Fatal(err)
	}
	// These are the three RFC3339Nano widths written by older versions.
	observations := []string{"2026-09-03T12:00:00Z", "2026-09-03T12:00:00.1Z", "2026-09-03T12:00:00.100000001Z"}
	for i, observed := range observations {
		id := []string{"1", "2", "3"}[i]
		if _, err := legacy.ExecContext(ctx, `INSERT INTO sellers VALUES(?, 'fixture', 'Fixture', x'7b7d', 'listing', 'direct', ?)`, id, observed); err != nil {
			t.Fatal(err)
		}
		if _, err := legacy.ExecContext(ctx, `INSERT INTO seller_listings VALUES(?, '42', 'Fixture', 'available', ?, ?)`, id, "https://www.kleinanzeigen.de/s-anzeige/fixture-"+id+"/42-1-1", observed); err != nil {
			t.Fatal(err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	matches, info, err := db.SearchSellers(ctx, "fixture", "exact", 3)
	if err != nil || len(matches) != 3 || matches[0].ID != "3" || matches[1].ID != "2" || matches[2].ID != "1" || info.Size != 3 {
		t.Fatalf("upgraded seller chronology: matches=%v info=%+v err=%v", matches, info, err)
	}
	for i, original := range observations {
		want, err := time.Parse(time.RFC3339Nano, original)
		if err != nil {
			t.Fatal(err)
		}
		if !matches[2-i].ObservedAt.Equal(want) {
			t.Errorf("seller observation changed: got %s want %s", matches[2-i].ObservedAt, want)
		}
		listings, err := db.SellerListings(ctx, matches[2-i].ID, 1)
		if err != nil || len(listings) != 1 || !listings[0].ObservedAt.Equal(want) {
			t.Errorf("listing observation changed: listings=%v want=%s err=%v", listings, want, err)
		}
	}
	url, err := db.PublicListingURL(ctx, "42")
	if err != nil || url != "https://www.kleinanzeigen.de/s-anzeige/fixture-3/42-1-1" {
		t.Errorf("upgraded listing chronology: url=%q err=%v", url, err)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
