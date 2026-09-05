package state

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const messageFingerprintVersion = "kcli-message-fingerprint/v1"

var (
	dmDigestPattern     = regexp.MustCompile(`^[a-f0-9]{64}$`)
	dmIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

type ConversationSummary struct {
	AccountHash        string
	ConversationID     string
	ListingID          string
	ListingTitle       string
	ListingStatus      string
	Role               string
	Counterparty       string
	Unread             bool
	UnreadMessageCount int
	MessageCount       int
	LastActivity       string
	Preview            string
	RemoteFingerprint  string
	ObservedAt         time.Time
}

type MessageFingerprint struct {
	AccountHash    string
	ConversationID string
	RemoteID       string
	Direction      string
	ReceivedAt     string
	Kind           string
	ContentDigest  string
	ObservedAt     time.Time
}

type storedConversationSummary struct {
	ListingID          string `json:"listing_id,omitempty"`
	ListingTitle       string `json:"listing_title,omitempty"`
	ListingStatus      string `json:"listing_status,omitempty"`
	Role               string `json:"role,omitempty"`
	Counterparty       string `json:"counterparty,omitempty"`
	Unread             bool   `json:"unread"`
	UnreadMessageCount int    `json:"unread_message_count"`
	MessageCount       int    `json:"message_count"`
	LastActivity       string `json:"last_activity,omitempty"`
	Preview            string `json:"preview,omitempty"`
}

func DigestMessageContent(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}

// MessageFingerprintKey returns an upstream-ID key when available. Otherwise it
// hashes only metadata and a precomputed content digest; callers cannot pass a
// message body to this helper.
func MessageFingerprintKey(input MessageFingerprint) (string, error) {
	if !authAccountHashPattern.MatchString(input.AccountHash) || !dmIdentifierPattern.MatchString(input.ConversationID) || !validDMMetaText(input.RemoteID, 256, false) {
		return "", fmt.Errorf("invalid message fingerprint identity")
	}
	if input.RemoteID != "" {
		return "id:" + input.RemoteID, nil
	}
	if !validDMMetaText(input.Direction, 64, true) || !validDMMetaText(input.ReceivedAt, 128, false) || !validDMMetaText(input.Kind, 256, true) || !dmDigestPattern.MatchString(input.ContentDigest) {
		return "", fmt.Errorf("invalid message fingerprint metadata")
	}
	received := normalizeDMTime(input.ReceivedAt)
	digest := sha256.New()
	for _, field := range []string{messageFingerprintVersion, input.AccountHash, input.ConversationID, strings.ToUpper(input.Direction), received, input.Kind, input.ContentDigest} {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(field)))
		_, _ = digest.Write(size[:])
		_, _ = digest.Write([]byte(field))
	}
	return "fp:v1:" + hex.EncodeToString(digest.Sum(nil)), nil
}

func ConversationSummaryFingerprint(summary ConversationSummary) (string, error) {
	if err := validateConversationFingerprint(summary); err != nil {
		return "", err
	}
	digest := sha256.New()
	fields := []string{
		"kcli-conversation-summary/v1", summary.ConversationID, summary.ListingID,
		summary.ListingTitle, summary.ListingStatus, strings.ToUpper(summary.Role),
		summary.Counterparty, fmt.Sprintf("%t", summary.Unread),
		fmt.Sprintf("%d", summary.UnreadMessageCount), fmt.Sprintf("%d", summary.MessageCount),
		normalizeDMTime(summary.LastActivity), summary.Preview,
	}
	for _, field := range fields {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(field)))
		_, _ = digest.Write(size[:])
		_, _ = digest.Write([]byte(field))
	}
	return "fp:v1:" + hex.EncodeToString(digest.Sum(nil)), nil
}

func (d *DB) UpsertConversationSummary(ctx context.Context, incoming ConversationSummary) (ConversationSummary, error) {
	if d == nil || d.sql == nil {
		return ConversationSummary{}, fmt.Errorf("state database is unavailable")
	}
	if err := validateConversationSummary(incoming); err != nil {
		return ConversationSummary{}, err
	}
	var merged ConversationSummary
	err := d.WithTx(ctx, func(tx *sql.Tx, _ *Queries) error {
		previous, exists, err := conversationSummaryFrom(ctx, tx, incoming.AccountHash, incoming.ConversationID)
		if err != nil {
			return err
		}
		merged = mergeConversationSummary(previous, incoming, exists)
		merged.Preview = boundedDMPreview(merged.Preview)
		fingerprint, err := ConversationSummaryFingerprint(merged)
		if err != nil {
			return err
		}
		merged.RemoteFingerprint = fingerprint
		stored := storedFromConversation(merged)
		summaryJSON, err := json.Marshal(stored)
		if err != nil {
			return fmt.Errorf("encode conversation summary: %w", err)
		}
		var listingID, counterparty, updatedAt any
		if merged.ListingID != "" {
			listingID = merged.ListingID
		}
		if merged.Counterparty != "" {
			counterparty = merged.Counterparty
		}
		if normalized := normalizeDMTime(merged.LastActivity); normalized != "" {
			updatedAt = normalized
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO conversations(account_hash, conversation_id, listing_id, counterparty, summary_json, remote_fingerprint, remote_updated_at, observed_at)
			VALUES(?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(account_hash, conversation_id) DO UPDATE SET
			listing_id=COALESCE(excluded.listing_id, conversations.listing_id),
			counterparty=COALESCE(excluded.counterparty, conversations.counterparty),
			summary_json=excluded.summary_json,
			remote_fingerprint=excluded.remote_fingerprint,
			remote_updated_at=COALESCE(excluded.remote_updated_at, conversations.remote_updated_at),
			observed_at=excluded.observed_at`,
			merged.AccountHash, merged.ConversationID, listingID, counterparty, summaryJSON,
			merged.RemoteFingerprint, updatedAt, merged.ObservedAt.UTC().Format(time.RFC3339Nano))
		return err
	})
	if err != nil {
		return ConversationSummary{}, fmt.Errorf("upsert conversation summary: %w", err)
	}
	return merged, nil
}

func (d *DB) ConversationSummary(ctx context.Context, accountHash, conversationID string) (ConversationSummary, error) {
	if d == nil || d.sql == nil {
		return ConversationSummary{}, fmt.Errorf("state database is unavailable")
	}
	if !authAccountHashPattern.MatchString(accountHash) || !dmIdentifierPattern.MatchString(conversationID) {
		return ConversationSummary{}, fmt.Errorf("invalid conversation identity")
	}
	summary, exists, err := conversationSummaryFrom(ctx, d.sql, accountHash, conversationID)
	if err != nil {
		return ConversationSummary{}, err
	}
	if !exists {
		return ConversationSummary{}, sql.ErrNoRows
	}
	return summary, nil
}

func (d *DB) ConversationSummaries(ctx context.Context, accountHash string, conversationIDs []string) ([]ConversationSummary, error) {
	if !authAccountHashPattern.MatchString(accountHash) || len(conversationIDs) == 0 || len(conversationIDs) > 100 {
		return nil, fmt.Errorf("invalid conversation lookup")
	}
	result := make([]ConversationSummary, 0, len(conversationIDs))
	for _, id := range conversationIDs {
		summary, err := d.ConversationSummary(ctx, accountHash, id)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		result = append(result, summary)
	}
	return result, nil
}

func (d *DB) UpsertMessageFingerprints(ctx context.Context, messages []MessageFingerprint) error {
	if d == nil || d.sql == nil {
		return fmt.Errorf("state database is unavailable")
	}
	if len(messages) > 10000 {
		return fmt.Errorf("too many message fingerprints")
	}
	return d.WithTx(ctx, func(tx *sql.Tx, _ *Queries) error {
		for _, message := range messages {
			if err := validateMessageFingerprint(message); err != nil {
				return err
			}
			key, err := MessageFingerprintKey(message)
			if err != nil {
				return err
			}
			var remoteID, receivedAt any
			if message.RemoteID != "" {
				remoteID = message.RemoteID
			}
			if normalized := normalizeDMTime(message.ReceivedAt); normalized != "" {
				receivedAt = normalized
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO messages(account_hash, conversation_id, message_key, remote_id, direction, received_at, content_digest, observed_at)
				VALUES(?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(account_hash, conversation_id, message_key) DO UPDATE SET
				remote_id=COALESCE(excluded.remote_id, messages.remote_id), direction=excluded.direction,
				received_at=COALESCE(excluded.received_at, messages.received_at), content_digest=excluded.content_digest,
				observed_at=excluded.observed_at`,
				message.AccountHash, message.ConversationID, key, remoteID, strings.ToUpper(message.Direction), receivedAt,
				message.ContentDigest, message.ObservedAt.UTC().Format(time.RFC3339Nano))
			if err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) MarkConversationSummariesRead(ctx context.Context, accountHash string, conversationIDs []string, observedAt time.Time) error {
	if d == nil || d.sql == nil || !authAccountHashPattern.MatchString(accountHash) || len(conversationIDs) == 0 || len(conversationIDs) > 100 || observedAt.IsZero() {
		return fmt.Errorf("invalid local mark-read update")
	}
	return d.WithTx(ctx, func(tx *sql.Tx, _ *Queries) error {
		for _, id := range conversationIDs {
			summary, exists, err := conversationSummaryFrom(ctx, tx, accountHash, id)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("conversation %q does not belong to the account", id)
			}
			summary.Unread = false
			summary.UnreadMessageCount = 0
			summary.ObservedAt = observedAt.UTC()
			fingerprint, err := ConversationSummaryFingerprint(summary)
			if err != nil {
				return err
			}
			summary.RemoteFingerprint = fingerprint
			encoded, err := json.Marshal(storedFromConversation(summary))
			if err != nil {
				return err
			}
			result, err := tx.ExecContext(ctx, `UPDATE conversations SET summary_json=?, remote_fingerprint=?, observed_at=? WHERE account_hash=? AND conversation_id=?`, encoded, fingerprint, observedAt.UTC().Format(time.RFC3339Nano), accountHash, id)
			if err != nil {
				return err
			}
			if changed, err := result.RowsAffected(); err != nil || changed != 1 {
				if err != nil {
					return err
				}
				return fmt.Errorf("conversation %q does not belong to the account", id)
			}
		}
		return nil
	})
}

type dmQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func conversationSummaryFrom(ctx context.Context, queryer dmQueryer, accountHash, conversationID string) (ConversationSummary, bool, error) {
	var listingID, counterparty, remoteUpdated sql.NullString
	var summaryJSON []byte
	var fingerprint, observedRaw string
	err := queryer.QueryRowContext(ctx, `SELECT listing_id, counterparty, summary_json, remote_fingerprint, remote_updated_at, observed_at FROM conversations WHERE account_hash=? AND conversation_id=?`, accountHash, conversationID).
		Scan(&listingID, &counterparty, &summaryJSON, &fingerprint, &remoteUpdated, &observedRaw)
	if err == sql.ErrNoRows {
		return ConversationSummary{}, false, nil
	}
	if err != nil {
		return ConversationSummary{}, false, err
	}
	var stored storedConversationSummary
	if err := json.Unmarshal(summaryJSON, &stored); err != nil {
		return ConversationSummary{}, false, fmt.Errorf("decode conversation summary: %w", err)
	}
	observedAt, err := time.Parse(time.RFC3339Nano, observedRaw)
	if err != nil {
		return ConversationSummary{}, false, fmt.Errorf("decode conversation observation time: %w", err)
	}
	summary := ConversationSummary{
		AccountHash: accountHash, ConversationID: conversationID, ListingID: stored.ListingID,
		ListingTitle: stored.ListingTitle, ListingStatus: stored.ListingStatus, Role: stored.Role,
		Counterparty: stored.Counterparty, Unread: stored.Unread, UnreadMessageCount: stored.UnreadMessageCount,
		MessageCount: stored.MessageCount, LastActivity: stored.LastActivity, Preview: stored.Preview,
		RemoteFingerprint: fingerprint, ObservedAt: observedAt.UTC(),
	}
	if summary.ListingID == "" {
		summary.ListingID = listingID.String
	}
	if summary.Counterparty == "" {
		summary.Counterparty = counterparty.String
	}
	if summary.LastActivity == "" {
		summary.LastActivity = remoteUpdated.String
	}
	return summary, true, nil
}

func mergeConversationSummary(previous, incoming ConversationSummary, exists bool) ConversationSummary {
	if !exists {
		return incoming
	}
	if incoming.ListingID == "" {
		incoming.ListingID = previous.ListingID
	}
	if incoming.ListingTitle == "" {
		incoming.ListingTitle = previous.ListingTitle
	}
	if incoming.ListingStatus == "" {
		incoming.ListingStatus = previous.ListingStatus
	}
	if incoming.Role == "" {
		incoming.Role = previous.Role
	}
	if incoming.Counterparty == "" {
		incoming.Counterparty = previous.Counterparty
	}
	if incoming.LastActivity == "" {
		incoming.LastActivity = previous.LastActivity
	}
	return incoming
}

func storedFromConversation(summary ConversationSummary) storedConversationSummary {
	return storedConversationSummary{
		ListingID: summary.ListingID, ListingTitle: summary.ListingTitle, ListingStatus: summary.ListingStatus,
		Role: strings.ToUpper(summary.Role), Counterparty: summary.Counterparty, Unread: summary.Unread,
		UnreadMessageCount: summary.UnreadMessageCount, MessageCount: summary.MessageCount,
		LastActivity: normalizeDMTime(summary.LastActivity), Preview: boundedDMPreview(summary.Preview),
	}
}

func validateConversationSummary(summary ConversationSummary) error {
	if summary.ObservedAt.IsZero() {
		return fmt.Errorf("invalid conversation observation time")
	}
	return validateConversationFingerprint(summary)
}

func validateConversationFingerprint(summary ConversationSummary) error {
	if !authAccountHashPattern.MatchString(summary.AccountHash) || !dmIdentifierPattern.MatchString(summary.ConversationID) || summary.UnreadMessageCount < 0 || summary.MessageCount < 0 {
		return fmt.Errorf("invalid conversation summary")
	}
	for _, value := range []struct {
		text string
		max  int
	}{{summary.ListingID, 128}, {summary.ListingTitle, 4096}, {summary.ListingStatus, 128}, {summary.Role, 64}, {summary.Counterparty, 1024}, {summary.LastActivity, 128}, {summary.Preview, 4096}} {
		if !validDMText(value.text, value.max, false) {
			return fmt.Errorf("invalid conversation summary text")
		}
	}
	return nil
}

func validateMessageFingerprint(message MessageFingerprint) error {
	if message.ObservedAt.IsZero() || !authAccountHashPattern.MatchString(message.AccountHash) || !dmIdentifierPattern.MatchString(message.ConversationID) || !validDMMetaText(message.RemoteID, 256, false) || !validDMMetaText(message.Direction, 64, true) || !validDMMetaText(message.ReceivedAt, 128, false) || !validDMMetaText(message.Kind, 256, true) || !dmDigestPattern.MatchString(message.ContentDigest) {
		return fmt.Errorf("invalid message fingerprint metadata")
	}
	return nil
}

func validDMText(value string, max int, required bool) bool {
	if required && value == "" {
		return false
	}
	return len(value) <= max && utf8.ValidString(value) && strings.IndexByte(value, 0) < 0 && strings.IndexFunc(value, func(r rune) bool { return (r < 0x20 && r != '\t' && r != '\n' && r != '\r') || r == 0x7f }) < 0
}

func validDMMetaText(value string, max int, required bool) bool {
	if required && value == "" {
		return false
	}
	return len(value) <= max && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}

func normalizeDMTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC().Format(time.RFC3339Nano)
	}
	return value
}

func boundedDMPreview(value string) string {
	const limit = 1024
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
