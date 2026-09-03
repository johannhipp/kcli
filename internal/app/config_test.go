package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPrecedenceDryRunAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "config.json")
	store, err := NewConfigStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if change, err := store.Set("alpha", "global.output", "table", false); err != nil || change.New != "table" {
		t.Fatalf("global set: %#v, %v", change, err)
	}
	if _, err := store.Set("alpha", "output", "json", true); err != nil {
		t.Fatal(err)
	}
	value, err := store.Get("alpha", "output")
	if err != nil {
		t.Fatal(err)
	}
	if value.Value != "table" || value.Source != "global" {
		t.Fatalf("dry run persisted: %#v", value)
	}
	if _, err := store.Set("alpha", "output", "json", false); err != nil {
		t.Fatal(err)
	}
	value, err = store.Get("alpha", "output")
	if err != nil {
		t.Fatal(err)
	}
	if value.Value != "json" || value.Source != "profile:alpha" {
		t.Fatalf("profile precedence: %#v", value)
	}
	global, err := store.Get("alpha", "global.output")
	if err != nil {
		t.Fatal(err)
	}
	if global.Value != "table" || global.Source != "global" {
		t.Fatalf("global lookup: %#v", global)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("config mode = %o", info.Mode().Perm())
	}
}
func TestConfigRejectsUnknownAndTrailingData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	store, _ := NewConfigStore(path)
	if _, err := store.Set("default", "secret", "value", false); err == nil {
		t.Fatal("unknown key accepted")
	}
	if err := os.WriteFile(path, []byte(`{"global":{}} {}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("trailing JSON accepted")
	}
}
