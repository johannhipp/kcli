package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

func TestDMPollEnvelopeAcknowledgementAndOpenChangedBoundary(t *testing.T) {
	now := time.Date(2026, 9, 3, 13, 0, 0, 0, time.UTC)
	baselineFixture := syncFixture(t, "conversations.redacted.json")
	changedFixture := syncFixture(t, "conversations-unavailable.redacted.json")
	messagesFixture := syncFixture(t, "conversation-messages.redacted.json")
	var changed atomic.Bool
	var putCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.Method {
		case http.MethodGet:
			if changed.Load() {
				_, _ = writer.Write(changedFixture)
			} else {
				_, _ = writer.Write(baselineFixture)
			}
		case http.MethodPut:
			putCalls.Add(1)
			_, _ = writer.Write(messagesFixture)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	application, secrets, database := authTestApp(t, server, now)
	authSeedSession(t, secrets, database, "access-token", "refresh-token", now.Add(time.Hour))

	baseline, err := application.DMPoll(context.Background(), "default", "request-baseline", domain.DMPollInputV1{Since: "now", Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Cursor == "" || len(baseline.Data) != 0 || putCalls.Load() != 0 {
		t.Fatalf("baseline=%#v put=%d", baseline, putCalls.Load())
	}
	changed.Store(true)
	listed, err := application.DMPoll(context.Background(), "default", "request-list-only", domain.DMPollInputV1{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 1 || listed.Data[0].Type != "dm.conversation.read_changed" || putCalls.Load() != 0 {
		t.Fatalf("listed=%#v put=%d", listed.Data, putCalls.Load())
	}
	head, _, _ := database.CursorHead(context.Background(), strings.Repeat("d", 64))
	if head.Acknowledged != 0 {
		t.Fatalf("DMPoll advanced before output acknowledgement: %#v", head)
	}
	if err := application.DMAcknowledge(context.Background(), listed.Cursor); err != nil {
		t.Fatal(err)
	}
	head, _, _ = database.CursorHead(context.Background(), strings.Repeat("d", 64))
	if head.Acknowledged == 0 {
		t.Fatalf("DMAcknowledge did not advance: %#v", head)
	}

	changed.Store(false)
	opened, err := application.DMPoll(context.Background(), "default", "request-open", domain.DMPollInputV1{Limit: 200, OpenChanged: true})
	if err != nil {
		t.Fatal(err)
	}
	if putCalls.Load() == 0 {
		t.Fatal("--open-changed did not issue the state-touching PUT")
	}
	messageEvents := 0
	for _, event := range opened.Data {
		if event.Type == "dm.message.created" {
			messageEvents++
		}
		encoded, _ := json.Marshal(event)
		if strings.Contains(string(encoded), "person@example.invalid") || strings.Contains(string(encoded), "[REDACTED_MESSAGE]") {
			t.Fatalf("private content in event: %s", encoded)
		}
	}
	if messageEvents == 0 {
		t.Fatalf("open-changed events=%#v", opened.Data)
	}
}

func TestDMPollWrongCursorRequiresResync(t *testing.T) {
	now := time.Date(2026, 9, 3, 13, 0, 0, 0, time.UTC)
	fixture := syncFixture(t, "conversations-unavailable.redacted.json")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write(fixture) }))
	defer server.Close()
	application, secrets, database := authTestApp(t, server, now)
	authSeedSession(t, secrets, database, "access-token", "refresh-token", now.Add(time.Hour))
	baseline, err := application.DMPoll(context.Background(), "default", "request-baseline", domain.DMPollInputV1{Since: "now", Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := domain.DecodeCursor(baseline.Cursor)
	payload.ProfileUUID = "123e4567-e89b-42d3-a456-426614174000"
	wrong, _ := domain.EncodeCursor(payload)
	_, err = application.DMPoll(context.Background(), "default", "request-wrong", domain.DMPollInputV1{After: wrong, Limit: 200})
	if domain.ExitCode(err) != 2 {
		t.Fatalf("wrong cursor err=%v exit=%d", err, domain.ExitCode(err))
	}
}

func TestDMPollRefreshesOnceAfter401(t *testing.T) {
	now := time.Date(2026, 9, 3, 13, 0, 0, 0, time.UTC)
	fixture := []byte(`{"conversations":[]}`)
	var listCalls atomic.Int64
	var refreshCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/oauth/token" {
			refreshCalls.Add(1)
			_, _ = writer.Write([]byte(`{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`))
			return
		}
		if strings.HasSuffix(request.URL.Path, "/conversations") {
			call := listCalls.Add(1)
			if call == 1 {
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
			_, _ = writer.Write(fixture)
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()
	application, secrets, database := authTestApp(t, server, now)
	authSeedSession(t, secrets, database, "access-old", "refresh-old", now.Add(time.Hour))
	if _, err := application.DMPoll(context.Background(), "default", "request-refresh", domain.DMPollInputV1{Since: "now", Limit: 200}); err != nil {
		t.Fatal(err)
	}
	if listCalls.Load() != 2 || refreshCalls.Load() != 1 {
		t.Fatalf("list calls=%d refresh calls=%d", listCalls.Load(), refreshCalls.Load())
	}
}

func TestDMWatchHonorsRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 3, 13, 0, 0, 0, time.UTC)
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/conversations") && calls.Add(1) == 1 {
			writer.Header().Set("Retry-After", "120")
			writer.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = writer.Write([]byte(`{"conversations":[]}`))
	}))
	defer server.Close()
	application, secrets, database := authTestApp(t, server, now)
	authSeedSession(t, secrets, database, "access-token", "refresh-token", now.Add(time.Hour))
	ctx, cancel := context.WithCancel(context.Background())
	clock := &syncWatchClock{now: now, cancel: cancel}
	application.Clock = clock
	err := application.DMWatch(ctx, "default", "request-rate-limit", domain.DMWatchInputV1{
		Since: "now", Interval: "30s", Limit: 200,
	}, func(domain.EventV1) error { return nil }, func() error { return nil }, nil)
	if domain.ExitCode(err) != 130 {
		t.Fatalf("watch err=%v exit=%d", err, domain.ExitCode(err))
	}
	clock.mu.Lock()
	sleeps := append([]time.Duration(nil), clock.sleeps...)
	clock.mu.Unlock()
	if len(sleeps) < 2 || sleeps[0] < 2*time.Minute || sleeps[0] > 2*time.Minute+12*time.Second {
		t.Fatalf("watch sleeps=%v", sleeps)
	}
}

type syncWatchClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
	cancel context.CancelFunc
}

func (c *syncWatchClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *syncWatchClock) Sleep(ctx context.Context, duration time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, duration)
	count := len(c.sleeps)
	cancel := c.cancel
	c.mu.Unlock()
	if count == 2 {
		cancel()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func syncFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
