//go:build testing

package secret

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStoreIsTestOnlyAndSupportsChunking(t *testing.T) {
	store, err := NewFileStore(filepath.Join(t.TempDir(), "secrets.json"), "kcli-test")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("x", 4096)
	if err := store.Set("default", "refresh_token", want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("default", "refresh_token")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %d bytes, want %d", len(got), len(want))
	}
}
