package state

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthAccountStateAndLogoutGeneration(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	hash := strings.Repeat("a", 64)
	if err := db.AuthStoreAccount(ctx, hash, "123456"); err != nil {
		t.Fatal(err)
	}
	account, exists, err := db.AuthAccount(ctx)
	if err != nil || !exists || account.SubjectHash != hash || account.AccountID != "123456" || account.LoginRequired {
		t.Fatalf("account = %#v, exists = %v, err = %v", account, exists, err)
	}
	if err := db.AuthSetLoginRequired(ctx); err != nil {
		t.Fatal(err)
	}
	account, exists, err = db.AuthAccount(ctx)
	if err != nil || !exists || !account.LoginRequired {
		t.Fatalf("login-required account = %#v, exists = %v, err = %v", account, exists, err)
	}

	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO conversations(account_hash,conversation_id,summary_json,remote_fingerprint,observed_at) VALUES(?,?,?,?,?)`, hash, "conversation-1", []byte(`{}`), "fingerprint", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO events(event_id,account_hash,type,preview_json,observed_at) VALUES(?,?,?,?,?)`, "event-1", hash, "dm.changed", []byte(`{}`), "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO cursor_heads(account_hash,generation,acknowledged_sequence) VALUES(?,?,?)`, hash, 0, 0); err != nil {
		t.Fatal(err)
	}

	preview, err := db.AuthLogoutPreview(ctx)
	if err != nil || !preview.HadAccount || preview.AccountID != "123456" || preview.NextGeneration != 1 {
		t.Fatalf("preview = %#v, err = %v", preview, err)
	}
	if _, exists, err := db.AuthAccount(ctx); err != nil || !exists {
		t.Fatalf("dry-run preview changed state: exists = %v, err = %v", exists, err)
	}

	effect, err := db.AuthLogout(ctx)
	if err != nil || !effect.HadAccount || effect.NextGeneration != 1 {
		t.Fatalf("logout = %#v, err = %v", effect, err)
	}
	if _, exists, err := db.AuthAccount(ctx); err != nil || exists {
		t.Fatalf("account after logout: exists = %v, err = %v", exists, err)
	}
	for _, table := range []string{"conversations", "events", "cursor_heads"} {
		var count int
		if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE account_hash=?`, hash).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("%s rows after logout = %d", table, count)
		}
	}
}

func TestAuthAccountChangeAdvancesGenerationAndPurgesOldContext(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	oldHash := strings.Repeat("b", 64)
	newHash := strings.Repeat("c", 64)
	if err := db.AuthStoreAccount(ctx, oldHash, "111"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SQL().ExecContext(ctx, `INSERT INTO cursor_heads(account_hash,generation,acknowledged_sequence) VALUES(?,?,?)`, oldHash, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := db.AuthStoreAccount(ctx, newHash, "222"); err != nil {
		t.Fatal(err)
	}
	account, exists, err := db.AuthAccount(ctx)
	if err != nil || !exists || account.SubjectHash != newHash || account.AccountID != "222" {
		t.Fatalf("new account = %#v, exists = %v, err = %v", account, exists, err)
	}
	generation, err := db.Queries().GetMeta(ctx, "cursor_generation")
	if err != nil || generation != "1" {
		t.Fatalf("generation = %q, err = %v", generation, err)
	}
	var count int
	if err := db.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM cursor_heads WHERE account_hash=?`, oldHash).Scan(&count); err != nil || count != 0 {
		t.Fatalf("old cursor count = %d, err = %v", count, err)
	}
}
