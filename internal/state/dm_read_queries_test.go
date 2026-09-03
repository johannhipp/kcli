package state

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMessageFingerprintDerivationUsesRemoteIDOrVersionedMetadata(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	base := MessageFingerprint{
		AccountHash: strings.Repeat("a", 64), ConversationID: "conv-one", Direction: "in",
		ReceivedAt: "2026-09-03T14:00:00+02:00", Kind: "ATTACHMENT",
		ContentDigest: DigestMessageContent("[REDACTED_MESSAGE]"), ObservedAt: now,
	}
	key, err := MessageFingerprintKey(base)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(key, "fp:v1:") || len(key) != len("fp:v1:")+64 {
		t.Fatalf("key=%q", key)
	}
	canonical := base
	canonical.ReceivedAt = "2026-09-03T12:00:00Z"
	canonicalKey, err := MessageFingerprintKey(canonical)
	if err != nil || canonicalKey != key {
		t.Fatalf("canonical key=%q err=%v; original=%q", canonicalKey, err, key)
	}
	withID := base
	withID.RemoteID = "remote-123"
	idKey, err := MessageFingerprintKey(withID)
	if err != nil || idKey != "id:remote-123" {
		t.Fatalf("id key=%q err=%v", idKey, err)
	}
}

func TestConversationContextSnapshotAndAccountIsolation(t *testing.T) {
	ctx := context.Background()
	database := openDMState(t)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	first := ConversationSummary{
		AccountHash: strings.Repeat("a", 64), ConversationID: "conv-one", ListingID: "listing-one",
		ListingTitle: "[REDACTED_LISTING_TITLE]", ListingStatus: "AVAILABLE", Role: "BUYER",
		Counterparty: "[REDACTED_COUNTERPARTY]", Unread: true, UnreadMessageCount: 2,
		MessageCount: 5, LastActivity: "2026-09-03T12:00:00Z", Preview: "[REDACTED_PREVIEW]", ObservedAt: now,
	}
	if _, err := database.UpsertConversationSummary(ctx, first); err != nil {
		t.Fatal(err)
	}
	unavailable := first
	unavailable.ListingTitle = ""
	unavailable.Counterparty = ""
	unavailable.ListingStatus = "UNAVAILABLE"
	unavailable.Unread = false
	unavailable.UnreadMessageCount = 0
	unavailable.ObservedAt = now.Add(time.Minute)
	merged, err := database.UpsertConversationSummary(ctx, unavailable)
	if err != nil {
		t.Fatal(err)
	}
	if merged.ListingTitle != first.ListingTitle || merged.Counterparty != first.Counterparty || merged.ListingStatus != "UNAVAILABLE" || merged.Unread {
		t.Fatalf("merged=%#v", merged)
	}
	other := first
	other.AccountHash = strings.Repeat("b", 64)
	other.ListingTitle = "[REDACTED_OTHER_LISTING]"
	if _, err := database.UpsertConversationSummary(ctx, other); err != nil {
		t.Fatal(err)
	}
	gotA, err := database.ConversationSummary(ctx, first.AccountHash, first.ConversationID)
	if err != nil || gotA.ListingTitle != first.ListingTitle {
		t.Fatalf("account A=%#v err=%v", gotA, err)
	}
	gotB, err := database.ConversationSummary(ctx, other.AccountHash, other.ConversationID)
	if err != nil || gotB.ListingTitle != other.ListingTitle {
		t.Fatalf("account B=%#v err=%v", gotB, err)
	}
	if _, err := database.ConversationSummary(ctx, strings.Repeat("c", 64), first.ConversationID); err != sql.ErrNoRows {
		t.Fatalf("cross-account lookup err=%v", err)
	}
}

func TestMessageFingerprintsPersistWithoutBodies(t *testing.T) {
	ctx := context.Background()
	database := openDMState(t)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	accountHash := strings.Repeat("d", 64)
	if _, err := database.UpsertConversationSummary(ctx, ConversationSummary{AccountHash: accountHash, ConversationID: "conv-one", ObservedAt: now}); err != nil {
		t.Fatal(err)
	}
	body := "[REDACTED_MESSAGE_BODY_SENTINEL]"
	messages := []MessageFingerprint{
		{AccountHash: accountHash, ConversationID: "conv-one", RemoteID: "remote-one", Direction: "OUT", ReceivedAt: now.Format(time.RFC3339Nano), Kind: "TEXT", ContentDigest: DigestMessageContent(body), ObservedAt: now},
		{AccountHash: accountHash, ConversationID: "conv-one", Direction: "IN", ReceivedAt: now.Add(time.Minute).Format(time.RFC3339Nano), Kind: "ATTACHMENT", ContentDigest: DigestMessageContent(body), ObservedAt: now},
	}
	if err := database.UpsertMessageFingerprints(ctx, messages); err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := database.SQL().QueryRowContext(ctx, `SELECT group_concat(message_key || ':' || content_digest || ':' || direction, '|') FROM messages WHERE account_hash=?`, accountHash).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, body) || !strings.Contains(stored, DigestMessageContent(body)) {
		t.Fatalf("stored fingerprint data=%q", stored)
	}
	if err := database.MarkConversationSummariesRead(ctx, accountHash, []string{"conv-one"}, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	conversation, err := database.ConversationSummary(ctx, accountHash, "conv-one")
	if err != nil || conversation.Unread || conversation.UnreadMessageCount != 0 {
		t.Fatalf("conversation=%#v err=%v", conversation, err)
	}
}

func openDMState(t *testing.T) *DB {
	t.Helper()
	database, err := Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}
