package dmsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/state"
)

type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	sleeps  []time.Duration
	onSleep func(int)
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) Sleep(ctx context.Context, duration time.Duration) error {
	c.mu.Lock()
	c.sleeps = append(c.sleeps, duration)
	count, callback := len(c.sleeps), c.onSleep
	c.mu.Unlock()
	if callback != nil {
		callback(count)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

type fakeClient struct {
	mu        sync.Mutex
	pages     map[int][]kleinanzeigen.ConversationSummary
	opened    map[string]kleinanzeigen.OpenedConversation
	listCalls []int
	openCalls []string
	failures  []error
	started   chan struct{}
	block     bool
}

func (c *fakeClient) ListConversations(ctx context.Context, page, _ int) (kleinanzeigen.ConversationPage, error) {
	c.mu.Lock()
	c.listCalls = append(c.listCalls, page)
	if len(c.failures) > 0 {
		err := c.failures[0]
		c.failures = c.failures[1:]
		c.mu.Unlock()
		return kleinanzeigen.ConversationPage{}, err
	}
	block, started := c.block, c.started
	items := append([]kleinanzeigen.ConversationSummary(nil), c.pages[page]...)
	c.mu.Unlock()
	if block {
		if started != nil {
			select {
			case started <- struct{}{}:
			default:
			}
		}
		<-ctx.Done()
		return kleinanzeigen.ConversationPage{}, &domain.Error{Code: domain.CodeInterrupted, Message: "request interrupted", Cause: ctx.Err()}
	}
	return kleinanzeigen.ConversationPage{Conversations: items}, nil
}

func (c *fakeClient) OpenConversation(_ context.Context, id string) (kleinanzeigen.OpenedConversation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.openCalls = append(c.openCalls, id)
	return c.opened[id], nil
}

func TestPollListOnlyBaselineChangesReplayAndMonotonicCursor(t *testing.T) {
	poller, client, _, database, account := newPoller(t)
	client.pages[0] = []kleinanzeigen.ConversationSummary{conversation("conv-one", "first", true, 1, 3)}
	baseline, err := poller.Run(context.Background(), PollOptions{Since: "now", Limit: 200, LeaseOwner: "baseline"})
	if err != nil {
		t.Fatal(err)
	}
	if len(baseline.Events) != 0 || len(client.openCalls) != 0 {
		t.Fatalf("baseline events=%#v opens=%#v", baseline.Events, client.openCalls)
	}
	payload, err := domain.DecodeCursor(baseline.Cursor)
	if err != nil || payload.Sequence != 0 || payload.AccountSubjectHash != account {
		t.Fatalf("baseline cursor=%#v err=%v", payload, err)
	}

	client.pages[0] = []kleinanzeigen.ConversationSummary{conversation("conv-one", "changed", false, 0, 4)}
	changed, err := poller.Run(context.Background(), PollOptions{Limit: 200, LeaseOwner: "changed"})
	if err != nil {
		t.Fatal(err)
	}
	if len(changed.Events) != 1 || changed.Events[0].Type != "dm.conversation.read_changed" || len(client.openCalls) != 0 {
		t.Fatalf("changed=%#v opens=%#v", changed.Events, client.openCalls)
	}
	replay, err := poller.Run(context.Background(), PollOptions{After: baseline.Cursor, Limit: 200, LeaseOwner: "replay"})
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.Events) != 1 || replay.Events[0].EventID != changed.Events[0].EventID {
		t.Fatalf("replay=%#v changed=%#v", replay.Events, changed.Events)
	}
	if err := database.AdvanceCursorHead(context.Background(), account, changed.Generation, changed.Sequence); err != nil {
		t.Fatal(err)
	}
	if err := database.AdvanceCursorHead(context.Background(), account, changed.Generation, 0); err != nil {
		t.Fatal(err)
	}
	head, _, _ := database.CursorHead(context.Background(), account)
	if head.Acknowledged != changed.Sequence {
		t.Fatalf("head rewound: %#v", head)
	}
	client.pages[0][0].Preview = "unknown change"
	updated, err := poller.Run(context.Background(), PollOptions{Limit: 200, LeaseOwner: "unknown-change"})
	if err != nil || len(updated.Events) != 1 || updated.Events[0].Type != "dm.conversation.updated" {
		t.Fatalf("updated events=%#v err=%v", updated.Events, err)
	}
	if err := database.AdvanceCursorHead(context.Background(), account, updated.Generation, updated.Sequence); err != nil {
		t.Fatal(err)
	}
	noChange, err := poller.Run(context.Background(), PollOptions{Limit: 200, LeaseOwner: "no-change"})
	if err != nil || len(noChange.Events) != 0 {
		t.Fatalf("no-change events=%#v err=%v", noChange.Events, err)
	}
}

func TestPollNewThreadOpenChangedMessagesAndCollision(t *testing.T) {
	poller, client, _, database, account := newPoller(t)
	client.pages[0] = nil
	baseline, err := poller.Run(context.Background(), PollOptions{Since: "now", Limit: 200, LeaseOwner: "baseline"})
	if err != nil {
		t.Fatal(err)
	}
	remote := conversation("conv-new", "new preview", true, 1, 1)
	client.pages[0] = []kleinanzeigen.ConversationSummary{remote}
	client.opened["conv-new"] = kleinanzeigen.OpenedConversation{Messages: []kleinanzeigen.Message{{ID: "message-one", Direction: "IN", Kind: "TEXT", ReceivedAt: pollTime.Add(time.Minute), Text: "[REDACTED]"}}}
	result, err := poller.Run(context.Background(), PollOptions{After: baseline.Cursor, Limit: 200, OpenChanged: true, LeaseOwner: "open"})
	if err != nil {
		t.Fatal(err)
	}
	counts := EventCountByType(result.Events)
	if counts["dm.conversation.created"] != 1 || counts["dm.message.created"] != 1 || len(client.openCalls) != 1 {
		t.Fatalf("events=%#v opens=%#v", result.Events, client.openCalls)
	}
	for _, event := range result.Events {
		if fmt.Sprint(event.Data) == "[REDACTED]" || strings.Contains(fmt.Sprint(event.Data), "[REDACTED]") {
			t.Fatalf("message body escaped into event: %#v", event)
		}
	}
	if err := database.AdvanceCursorHead(context.Background(), account, result.Generation, result.Sequence); err != nil {
		t.Fatal(err)
	}
	client.pages[0][0].Preview = "edited preview"
	client.opened["conv-new"] = kleinanzeigen.OpenedConversation{Messages: []kleinanzeigen.Message{{ID: "message-one", Direction: "IN", Kind: "TEXT", ReceivedAt: pollTime.Add(time.Minute), Text: "[REDACTED_EDIT]"}}}
	edited, err := poller.Run(context.Background(), PollOptions{Limit: 200, OpenChanged: true, LeaseOwner: "edit"})
	if err != nil {
		t.Fatal(err)
	}
	foundCollision := false
	for _, event := range edited.Events {
		if event.Data["reason"] == "message_edit_or_fingerprint_collision" {
			foundCollision = true
		}
	}
	if !foundCollision {
		t.Fatalf("edit/collision was not observable: %#v", edited.Events)
	}
}

func TestPollDefaultNeverOpensAndPaginationIsBounded(t *testing.T) {
	poller, client, _, _, _ := newPoller(t)
	first := make([]kleinanzeigen.ConversationSummary, 100)
	for index := range first {
		first[index] = conversation(fmt.Sprintf("conv-%03d", index), "preview", false, 0, 1)
	}
	client.pages[0] = first
	client.pages[1] = []kleinanzeigen.ConversationSummary{conversation("conv-last", "preview", true, 1, 2)}
	result, err := poller.Run(context.Background(), PollOptions{Since: "2026-09-03T11:00:00Z", Limit: 500, LeaseOwner: "pages"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Pages != 2 || result.Fetched != 101 || len(client.openCalls) != 0 {
		t.Fatalf("pages=%d fetched=%d opens=%#v", result.Pages, result.Fetched, client.openCalls)
	}
}

func TestPollRejectsWrongAndUnknownCursor(t *testing.T) {
	poller, client, _, _, _ := newPoller(t)
	client.pages[0] = nil
	baseline, err := poller.Run(context.Background(), PollOptions{Since: "now", Limit: 200, LeaseOwner: "baseline"})
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := domain.DecodeCursor(baseline.Cursor)
	payload.AccountSubjectHash = strings.Repeat("f", 64)
	wrong, _ := domain.EncodeCursor(payload)
	_, err = poller.Run(context.Background(), PollOptions{After: wrong, Limit: 200, LeaseOwner: "wrong"})
	if ErrorCode(err) != domain.CodeResyncRequired || domain.ExitCode(err) != 2 {
		t.Fatalf("wrong cursor err=%v code=%d", err, domain.ExitCode(err))
	}
	payload.AccountSubjectHash = poller.AccountHash
	payload.Sequence = 999
	unknown, _ := domain.EncodeCursor(payload)
	_, err = poller.Run(context.Background(), PollOptions{After: unknown, Limit: 200, LeaseOwner: "unknown"})
	if ErrorCode(err) != domain.CodeResyncRequired {
		t.Fatalf("unknown cursor err=%v", err)
	}
}

func TestWatchHeartbeatBackoffBrokenPipeAndSignal(t *testing.T) {
	t.Run("heartbeat opt in", func(t *testing.T) {
		poller, client, clock, _, _ := newPoller(t)
		client.pages[0] = nil
		ctx, cancel := context.WithCancel(context.Background())
		clock.onSleep = func(_ int) { cancel() }
		var events []domain.EventV1
		watcher := &Watcher{Poller: poller, Clock: clock, Emit: func(event domain.EventV1) error { events = append(events, event); return nil }, Flush: func() error { return nil }}
		err := watcher.Run(ctx, WatchOptions{Since: "now", Interval: 30 * time.Second, Limit: 200, IncludeHeartbeats: true, LeaseOwner: "heartbeat"})
		if ErrorCode(err) != domain.CodeInterrupted || len(events) != 1 || events[0].Type != "system.heartbeat" {
			t.Fatalf("events=%#v err=%v", events, err)
		}
	})

	t.Run("retry after and exponential backoff", func(t *testing.T) {
		poller, client, clock, _, _ := newPoller(t)
		retryAfter := 2 * time.Minute
		client.failures = []error{&domain.Error{Code: domain.CodeRateLimited, Message: "rate limited", Retryable: true, RetryAfter: &retryAfter}}
		client.pages[0] = nil
		ctx, cancel := context.WithCancel(context.Background())
		clock.onSleep = func(count int) {
			if count == 2 {
				cancel()
			}
		}
		watcher := &Watcher{Poller: poller, Clock: clock, Emit: func(domain.EventV1) error { return nil }, Flush: func() error { return nil }}
		err := watcher.Run(ctx, WatchOptions{Since: "now", Interval: 30 * time.Second, Limit: 200, LeaseOwner: "backoff"})
		if ErrorCode(err) != domain.CodeInterrupted {
			t.Fatalf("err=%v", err)
		}
		clock.mu.Lock()
		sleeps := append([]time.Duration(nil), clock.sleeps...)
		clock.mu.Unlock()
		if len(sleeps) < 2 || sleeps[0] < retryAfter || sleeps[0] > retryAfter+retryAfter/10 {
			t.Fatalf("sleeps=%v", sleeps)
		}
	})

	t.Run("broken output does not acknowledge", func(t *testing.T) {
		poller, client, clock, database, account := newPoller(t)
		client.pages[0] = nil
		if _, err := poller.Run(context.Background(), PollOptions{Since: "now", Limit: 200, LeaseOwner: "baseline"}); err != nil {
			t.Fatal(err)
		}
		client.pages[0] = []kleinanzeigen.ConversationSummary{conversation("conv-new", "preview", true, 1, 1)}
		watcher := &Watcher{Poller: poller, Clock: clock, Emit: func(domain.EventV1) error { return errors.New("broken pipe") }, Flush: func() error { return nil }}
		err := watcher.Run(context.Background(), WatchOptions{Interval: 30 * time.Second, Limit: 200, LeaseOwner: "broken"})
		if ErrorCode(err) != domain.CodeUnavailable {
			t.Fatalf("err=%v", err)
		}
		head, _, _ := database.CursorHead(context.Background(), account)
		if head.Acknowledged != 0 {
			t.Fatalf("broken output advanced head: %#v", head)
		}
	})

	t.Run("sigterm cancels active request", func(t *testing.T) {
		poller, client, clock, _, _ := newPoller(t)
		client.block = true
		client.started = make(chan struct{}, 1)
		signals := make(chan os.Signal, 1)
		watcher := &Watcher{Poller: poller, Clock: clock, Emit: func(domain.EventV1) error { return nil }, Flush: func() error { return nil }, Signals: signals}
		done := make(chan error, 1)
		go func() {
			done <- watcher.Run(context.Background(), WatchOptions{Since: "now", Interval: 30 * time.Second, Limit: 200, LeaseOwner: "signal"})
		}()
		<-client.started
		signals <- syscall.SIGTERM
		err := <-done
		if ErrorCode(err) != domain.CodeTerminated || domain.ExitCode(err) != 143 {
			t.Fatalf("signal err=%v exit=%d", err, domain.ExitCode(err))
		}
	})
}

func TestPollRestartPreservesUnacknowledgedEventsAndCorruptionRequiresResync(t *testing.T) {
	t.Run("restart replay", func(t *testing.T) {
		ctx := context.Background()
		path := filepath.Join(t.TempDir(), "state.db")
		database, err := state.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		account := strings.Repeat("a", 64)
		client := &fakeClient{pages: map[int][]kleinanzeigen.ConversationSummary{}, opened: map[string]kleinanzeigen.OpenedConversation{}}
		clock := &fakeClock{now: pollTime}
		poller := &Poller{State: database, Client: client, Clock: clock, AccountHash: account}
		baseline, err := poller.Run(ctx, PollOptions{Since: "now", Limit: 200, LeaseOwner: "baseline"})
		if err != nil {
			t.Fatal(err)
		}
		client.pages[0] = []kleinanzeigen.ConversationSummary{conversation("conv-restart", "preview", true, 1, 1)}
		observed, err := poller.Run(ctx, PollOptions{After: baseline.Cursor, Limit: 200, LeaseOwner: "observed"})
		if err != nil || len(observed.Events) != 1 {
			t.Fatalf("observed=%#v err=%v", observed.Events, err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
		database, err = state.Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		defer database.Close()
		restarted := &Poller{State: database, Client: client, Clock: clock, AccountHash: account}
		replay, err := restarted.Run(ctx, PollOptions{Limit: 200, LeaseOwner: "restarted"})
		if err != nil {
			t.Fatal(err)
		}
		if len(replay.Events) != 1 || replay.Events[0].EventID != observed.Events[0].EventID {
			t.Fatalf("replay=%#v observed=%#v", replay.Events, observed.Events)
		}
	})

	t.Run("corrupted spool", func(t *testing.T) {
		poller, client, _, database, _ := newPoller(t)
		client.pages[0] = nil
		baseline, err := poller.Run(context.Background(), PollOptions{Since: "now", Limit: 200, LeaseOwner: "baseline"})
		if err != nil {
			t.Fatal(err)
		}
		client.pages[0] = []kleinanzeigen.ConversationSummary{conversation("conv-corrupt", "preview", true, 1, 1)}
		if _, err := poller.Run(context.Background(), PollOptions{After: baseline.Cursor, Limit: 200, LeaseOwner: "event"}); err != nil {
			t.Fatal(err)
		}
		if _, err := database.SQL().ExecContext(context.Background(), `UPDATE events SET preview_json=?`, []byte("{")); err != nil {
			t.Fatal(err)
		}
		_, err = poller.Run(context.Background(), PollOptions{Limit: 200, LeaseOwner: "corrupt"})
		if ErrorCode(err) != domain.CodeResyncRequired {
			t.Fatalf("corrupt spool err=%v", err)
		}
	})
}

func TestBackoffCeilingSignalCodesAndIdleSilence(t *testing.T) {
	if delay := BackoffDelay(99, 20*time.Minute, 1); delay != MaximumBackoff {
		t.Fatalf("backoff ceiling=%s", delay)
	}
	if domain.ExitCode(signalError(os.Interrupt)) != 130 || domain.ExitCode(signalError(syscall.SIGTERM)) != 143 {
		t.Fatal("watch signal exit codes changed")
	}
	poller, client, clock, _, _ := newPoller(t)
	client.pages[0] = nil
	ctx, cancel := context.WithCancel(context.Background())
	clock.onSleep = func(_ int) { cancel() }
	emitted := 0
	watcher := &Watcher{Poller: poller, Clock: clock, Emit: func(domain.EventV1) error { emitted++; return nil }, Flush: func() error { return nil }}
	err := watcher.Run(ctx, WatchOptions{Since: "now", Interval: DefaultWatchInterval, Limit: 200, LeaseOwner: "silent"})
	if ErrorCode(err) != domain.CodeInterrupted || emitted != 0 {
		t.Fatalf("idle watch emitted=%d err=%v", emitted, err)
	}
}

var pollTime = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

func newPoller(t *testing.T) (*Poller, *fakeClient, *fakeClock, *state.DB, string) {
	t.Helper()
	database, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	account := strings.Repeat("a", 64)
	client := &fakeClient{pages: make(map[int][]kleinanzeigen.ConversationSummary), opened: make(map[string]kleinanzeigen.OpenedConversation)}
	clock := &fakeClock{now: pollTime}
	return &Poller{State: database, Client: client, Clock: clock, AccountHash: account}, client, clock, database, account
}

func conversation(id, preview string, unread bool, unreadCount, messageCount int) kleinanzeigen.ConversationSummary {
	return kleinanzeigen.ConversationSummary{
		ID: id, ListingID: "listing-one", Unread: unread, UnreadKnown: true,
		UnreadMessageCount: unreadCount, UnreadCountKnown: true, MessageCount: messageCount, MessageCountKnown: true,
		ReceivedAt: pollTime, Preview: preview,
	}
}
