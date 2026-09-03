package state

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	generated "github.com/johannhipp/kcli/internal/state/sqlc"
)

func TestOpenMigratesAndConfiguresSQLite(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "profile", "state.db")
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	names, err := db.Queries().ListSchemaTables(ctx)
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
	err = db.WithTx(ctx, func(_ *sql.Tx, q *generated.Queries) error {
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
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
