package cli

import (
	"bytes"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/state"
)

func TestCommandDeadlineIncludesStateInitialization(t *testing.T) {
	base := setPathEnvironment(t)
	db, err := state.Open(context.Background(), filepath.Join(base, "state", "kcli", "profiles", "default", "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.SQL().Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "BEGIN EXCLUSIVE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "ROLLBACK")
	original := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = original })
	http.DefaultTransport = webAcceptanceRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("network request after failed initialization")
		return nil, nil
	})
	var stdout, stderr bytes.Buffer
	started := time.Now()
	code := Execute(context.Background(), []string{"--timeout", "50ms", "category", "list"}, strings.NewReader(""), &stdout, &stderr)
	elapsed := time.Since(started)
	if elapsed > time.Second || code != 5 {
		t.Fatalf("elapsed=%s exit=%d stdout=%s stderr=%s", elapsed, code, stdout.String(), stderr.String())
	}
}
