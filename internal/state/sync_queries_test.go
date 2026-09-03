package state

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

func TestCursorValidationMonotonicHeadAndAccountIsolation(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	accountA, accountB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	identity, err := database.CursorIdentity(ctx, accountA)
	if err != nil {
		t.Fatal(err)
	}
	commit, err := database.CommitSyncBatch(ctx, SyncBatch{
		AccountHash: accountA, Generation: identity.Generation, InitialHead: true, ObservedAt: now,
		Events: []PendingEvent{{EventID: "evt_a", Type: "dm.conversation.created", ConversationID: "conv-a", Data: map[string]any{"complete": false}, ObservedAt: now}},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := domain.CursorPayloadV1{FormatVersion: 1, ProfileUUID: identity.ProfileUUID, AccountSubjectHash: accountA, Generation: identity.Generation, Sequence: commit.LastSequence}
	if err := database.ValidateCursor(ctx, accountA, payload); err != nil {
		t.Fatal(err)
	}
	wrong := payload
	wrong.AccountSubjectHash = accountB
	if err := database.ValidateCursor(ctx, accountA, wrong); !errors.Is(err, ErrCursorContinuity) {
		t.Fatalf("wrong-account cursor error=%v", err)
	}
	wrong = payload
	wrong.ProfileUUID = "123e4567-e89b-42d3-a456-426614174000"
	if err := database.ValidateCursor(ctx, accountA, wrong); !errors.Is(err, ErrCursorContinuity) {
		t.Fatalf("wrong-store cursor error=%v", err)
	}
	wrong = payload
	wrong.Generation++
	if err := database.ValidateCursor(ctx, accountA, wrong); !errors.Is(err, ErrCursorContinuity) {
		t.Fatalf("old-generation cursor error=%v", err)
	}
	if err := database.AdvanceCursorHead(ctx, accountA, identity.Generation, commit.LastSequence); err != nil {
		t.Fatal(err)
	}
	if err := database.AdvanceCursorHead(ctx, accountA, identity.Generation, 0); err != nil {
		t.Fatal(err)
	}
	head, exists, err := database.CursorHead(ctx, accountA)
	if err != nil || !exists || head.Acknowledged != commit.LastSequence {
		t.Fatalf("head=%#v exists=%v err=%v", head, exists, err)
	}
}

func TestEventSpoolOrderingRetentionAndStableDedup(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	account := strings.Repeat("c", 64)
	identity, _ := database.CursorIdentity(ctx, account)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	batch := SyncBatch{AccountHash: account, Generation: identity.Generation, InitialHead: true, ObservedAt: now, MaxEvents: 2, Retention: 7 * 24 * time.Hour}
	for index, id := range []string{"evt_one", "evt_two", "evt_three"} {
		batch.Events = append(batch.Events, PendingEvent{EventID: id, Type: "dm.conversation.updated", ConversationID: "conv-one", Data: map[string]any{"ordinal": index}, ObservedAt: now.Add(time.Duration(index) * time.Second)})
	}
	if _, err := database.CommitSyncBatch(ctx, batch); err != nil {
		t.Fatal(err)
	}
	events, err := database.ReadEventsAfter(ctx, account, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].EventID != "evt_two" || events[1].EventID != "evt_three" || events[0].Sequence >= events[1].Sequence {
		t.Fatalf("events=%#v", events)
	}
	duplicate := SyncBatch{AccountHash: account, Generation: identity.Generation, ObservedAt: now.Add(time.Minute), Events: []PendingEvent{{EventID: "evt_three", Type: "dm.conversation.updated", ConversationID: "conv-one", Data: map[string]any{"ordinal": 99}, ObservedAt: now.Add(time.Minute)}}}
	if _, err := database.CommitSyncBatch(ctx, duplicate); err != nil {
		t.Fatal(err)
	}
	events, _ = database.ReadEventsAfter(ctx, account, 0, 10)
	if len(events) != 2 || events[1].Data["ordinal"] != float64(2) {
		t.Fatalf("deduplicated events=%#v", events)
	}

	oldAccount := strings.Repeat("d", 64)
	oldIdentity, _ := database.CursorIdentity(ctx, oldAccount)
	_, err = database.CommitSyncBatch(ctx, SyncBatch{AccountHash: oldAccount, Generation: oldIdentity.Generation, InitialHead: true, ObservedAt: now, Retention: 7 * 24 * time.Hour, Events: []PendingEvent{{EventID: "evt_old", Type: "dm.conversation.updated", Data: map[string]any{"complete": false}, ObservedAt: now.Add(-8 * 24 * time.Hour)}}})
	if err != nil {
		t.Fatal(err)
	}
	oldEvents, _ := database.ReadEventsAfter(ctx, oldAccount, 0, 10)
	if len(oldEvents) != 0 {
		t.Fatalf("expired events=%#v", oldEvents)
	}
	fresh, _ := database.ReadEventsAfter(ctx, account, 0, 10)
	if len(fresh) != 2 {
		t.Fatalf("other account events were affected: %#v", fresh)
	}
}

func TestSyncLeasePreventsOverlapAndExpires(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	account := strings.Repeat("e", 64)
	identity, _ := database.CursorIdentity(ctx, account)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	acquired, err := database.AcquireSyncLease(ctx, identity.ProfileUUID, account, "owner-one", now, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("first acquire=%v err=%v", acquired, err)
	}
	acquired, err = database.AcquireSyncLease(ctx, identity.ProfileUUID, account, "owner-two", now.Add(30*time.Second), time.Minute)
	if err != nil || acquired {
		t.Fatalf("overlap acquire=%v err=%v", acquired, err)
	}
	acquired, err = database.AcquireSyncLease(ctx, identity.ProfileUUID, account, "owner-two", now.Add(time.Minute), time.Minute)
	if err != nil || !acquired {
		t.Fatalf("expired acquire=%v err=%v", acquired, err)
	}
	if err := database.ReleaseSyncLease(ctx, identity.ProfileUUID, account, "owner-two"); err != nil {
		t.Fatal(err)
	}
}
