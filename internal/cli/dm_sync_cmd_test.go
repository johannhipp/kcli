package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/app"
	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/output"
	"github.com/johannhipp/kcli/internal/state"
)

func TestDMPollCommandFlushesBeforeAdvanceAndBrokenPipeReplays(t *testing.T) {
	ctx := context.Background()
	database, err := state.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 9, 3, 13, 0, 0, 0, time.UTC)
	account := strings.Repeat("e", 64)
	if err := database.AuthStoreAccount(ctx, account, "987654"); err != nil {
		t.Fatal(err)
	}
	secrets := &dmCLISecrets{values: map[string]string{
		"default\x00" + app.AuthSecretAccessToken: "access-token", "default\x00" + app.AuthSecretRefreshToken: "refresh-token",
		"default\x00" + app.AuthSecretEmail: "redacted@example.invalid", "default\x00" + app.AuthSecretExpiry: now.Add(time.Hour).Format(time.RFC3339Nano),
	}}
	transport := &dmCLITransport{list: dmCLIFixture(t, "conversations.redacted.json"), get: dmCLIFixture(t, "conversation-messages.redacted.json")}
	clock := &cliSyncClock{now: now}
	core := app.New(app.Dependencies{Transport: transport, State: database, Secrets: secrets, Clock: clock})
	var stdout, stderr bytes.Buffer
	runtime := &Runtime{Context: ctx, Stdout: &stdout, Stderr: &stderr, RequestID: "request-baseline", Profile: "default", Encoder: output.Encoder{Format: output.FormatJSON}, Core: core}
	if err := (&DMPollCmd{Since: "now", Limit: 200}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	baseline := assertSingleDMJSON(t, stdout.Bytes(), "kcli.dm-events/v1")
	if baseline["cursor"] == "" {
		t.Fatalf("baseline=%#v", baseline)
	}

	transport.list = dmCLIFixture(t, "conversations-unavailable.redacted.json")
	runtime.RequestID = "request-broken"
	runtime.Stdout = failingWriter{}
	err = (&DMPollCmd{Limit: 200}).Run(runtime)
	if err == nil {
		t.Fatal("broken output unexpectedly succeeded")
	}
	head, _, _ := database.CursorHead(ctx, account)
	if head.Acknowledged != 0 {
		t.Fatalf("broken output advanced head: %#v", head)
	}

	stdout.Reset()
	runtime.Stdout = &stdout
	runtime.RequestID = "request-replay"
	if err := (&DMPollCmd{Limit: 200}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	replayed := assertSingleDMJSON(t, stdout.Bytes(), "kcli.dm-events/v1")
	rows, _ := replayed["data"].([]any)
	if len(rows) != 1 {
		t.Fatalf("replayed=%#v", replayed)
	}
	head, _, _ = database.CursorHead(ctx, account)
	if head.Acknowledged == 0 {
		t.Fatalf("successful flush did not advance: %#v", head)
	}
	if !strings.Contains(stderr.String(), "list-only") || strings.Contains(stderr.String(), "redacted@example.invalid") {
		t.Fatalf("diagnostics=%q", stderr.String())
	}
}

func TestDMWatchCommandForcesNDJSONAndHeartbeatsAreOptIn(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	database, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 9, 3, 13, 0, 0, 0, time.UTC)
	if err := database.AuthStoreAccount(context.Background(), strings.Repeat("e", 64), "987654"); err != nil {
		t.Fatal(err)
	}
	secrets := &dmCLISecrets{values: map[string]string{
		"default\x00" + app.AuthSecretAccessToken: "access-token", "default\x00" + app.AuthSecretRefreshToken: "refresh-token",
		"default\x00" + app.AuthSecretEmail: "redacted@example.invalid", "default\x00" + app.AuthSecretExpiry: now.Add(time.Hour).Format(time.RFC3339Nano),
	}}
	clock := &cliSyncClock{now: now, onSleep: cancel}
	transport := &dmCLITransport{list: []byte(`{"conversations":[]}`)}
	var stdout, stderr bytes.Buffer
	runtime := &Runtime{
		Context: ctx, Stdout: &stdout, Stderr: &stderr, RequestID: "request-watch", Profile: "default",
		Encoder: output.Encoder{Format: output.FormatJSON},
		Core:    app.New(app.Dependencies{Transport: transport, State: database, Secrets: secrets, Clock: clock}),
	}
	err = (&DMWatchCmd{Since: "now", Interval: 30 * time.Second, Limit: 200, IncludeHeartbeats: true}).Run(runtime)
	if domain.ExitCode(err) != 130 {
		t.Fatalf("watch err=%v exit=%d", err, domain.ExitCode(err))
	}
	decoder := json.NewDecoder(bytes.NewReader(stdout.Bytes()))
	var event domain.EventV1
	if err := decoder.Decode(&event); err != nil {
		t.Fatal(err)
	}
	if event.Schema != domain.EventSchemaV1 || event.Type != "system.heartbeat" {
		t.Fatalf("event=%#v", event)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("watch output was not one NDJSON event: %v", err)
	}
	if runtime.Encoder.Format != output.FormatNDJSON {
		t.Fatalf("watch format=%q", runtime.Encoder.Format)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

type cliSyncClock struct {
	mu      sync.Mutex
	now     time.Time
	onSleep func()
}

func (c *cliSyncClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *cliSyncClock) Sleep(ctx context.Context, _ time.Duration) error {
	if c.onSleep != nil {
		c.onSleep()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
