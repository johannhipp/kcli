package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

const (
	DefaultEventRetention = 7 * 24 * time.Hour
	DefaultEventLimit     = 10_000
)

var ErrCursorContinuity = errors.New("cursor continuity cannot be established")

type CursorHead struct {
	ProfileUUID  string
	AccountHash  string
	Generation   int64
	Acknowledged int64
}

type StoredEvent struct {
	Sequence       int64
	EventID        string
	AccountHash    string
	Type           string
	ConversationID string
	ListingID      string
	Data           map[string]any
	ObservedAt     time.Time
}

type PendingEvent struct {
	EventID        string
	Type           string
	ConversationID string
	ListingID      string
	Data           map[string]any
	ObservedAt     time.Time
}

type SyncBatch struct {
	AccountHash     string
	Generation      int64
	InitialHead     bool
	InitialSequence int64
	Summaries       []ConversationSummary
	Messages        []MessageFingerprint
	Events          []PendingEvent
	ObservedAt      time.Time
	Retention       time.Duration
	MaxEvents       int
	IncrementCycle  bool
}

type SyncCommit struct {
	LastSequence int64
	CycleCount   int64
}

func (d *DB) CursorIdentity(ctx context.Context, accountHash string) (CursorHead, error) {
	if d == nil || d.sql == nil || !authAccountHashPattern.MatchString(accountHash) {
		return CursorHead{}, fmt.Errorf("invalid cursor account")
	}
	profileUUID, err := d.ProfileUUID(ctx)
	if err != nil {
		return CursorHead{}, err
	}
	rawGeneration, err := d.queries.GetMeta(ctx, authCursorGenerationKey)
	if err != nil {
		return CursorHead{}, fmt.Errorf("read cursor generation: %w", err)
	}
	generation, err := strconv.ParseInt(rawGeneration, 10, 64)
	if err != nil || generation < 0 {
		return CursorHead{}, fmt.Errorf("stored cursor generation is invalid")
	}
	return CursorHead{ProfileUUID: profileUUID, AccountHash: accountHash, Generation: generation}, nil
}

func (d *DB) CursorHead(ctx context.Context, accountHash string) (CursorHead, bool, error) {
	identity, err := d.CursorIdentity(ctx, accountHash)
	if err != nil {
		return CursorHead{}, false, err
	}
	err = d.sql.QueryRowContext(ctx, `SELECT generation, acknowledged_sequence FROM cursor_heads WHERE account_hash=?`, accountHash).Scan(&identity.Generation, &identity.Acknowledged)
	if errors.Is(err, sql.ErrNoRows) {
		return identity, false, nil
	}
	if err != nil {
		return CursorHead{}, false, fmt.Errorf("read cursor head: %w", err)
	}
	if identity.Generation < 0 || identity.Acknowledged < 0 {
		return CursorHead{}, false, fmt.Errorf("stored cursor head is invalid")
	}
	return identity, true, nil
}

func (d *DB) ValidateCursor(ctx context.Context, accountHash string, payload domain.CursorPayloadV1) error {
	if err := payload.Validate(); err != nil || payload.AccountSubjectHash != accountHash {
		return ErrCursorContinuity
	}
	identity, err := d.CursorIdentity(ctx, accountHash)
	if err != nil {
		return err
	}
	if payload.ProfileUUID != identity.ProfileUUID || payload.Generation != identity.Generation {
		return ErrCursorContinuity
	}
	head, exists, err := d.CursorHead(ctx, accountHash)
	if err != nil {
		return err
	}
	if !exists || head.Generation != payload.Generation {
		return ErrCursorContinuity
	}
	if payload.Sequence == 0 || payload.Sequence == head.Acknowledged {
		return nil
	}
	var one int
	if err := d.sql.QueryRowContext(ctx, `SELECT 1 FROM events WHERE account_hash=? AND sequence=?`, accountHash, payload.Sequence).Scan(&one); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrCursorContinuity
		}
		return fmt.Errorf("validate cursor event: %w", err)
	}
	return nil
}

func (d *DB) AdvanceCursorHead(ctx context.Context, accountHash string, generation, sequence int64) error {
	if d == nil || d.sql == nil || !authAccountHashPattern.MatchString(accountHash) || generation < 0 || sequence < 0 {
		return fmt.Errorf("invalid cursor advance")
	}
	return d.WithTx(ctx, func(tx *sql.Tx, _ *Queries) error {
		var storedGeneration, current int64
		if err := tx.QueryRowContext(ctx, `SELECT generation, acknowledged_sequence FROM cursor_heads WHERE account_hash=?`, accountHash).Scan(&storedGeneration, &current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrCursorContinuity
			}
			return err
		}
		if storedGeneration != generation {
			return ErrCursorContinuity
		}
		if sequence <= current {
			return nil
		}
		var one int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM events WHERE account_hash=? AND sequence=?`, accountHash, sequence).Scan(&one); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrCursorContinuity
			}
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE cursor_heads SET acknowledged_sequence=? WHERE account_hash=? AND generation=? AND acknowledged_sequence < ?`, sequence, accountHash, generation, sequence)
		return err
	})
}

func (d *DB) ReadEventsAfter(ctx context.Context, accountHash string, sequence int64, limit int) ([]StoredEvent, error) {
	if d == nil || d.sql == nil || !authAccountHashPattern.MatchString(accountHash) || sequence < 0 || limit < 1 || limit > 500 {
		return nil, fmt.Errorf("invalid event spool read")
	}
	rows, err := d.sql.QueryContext(ctx, `SELECT sequence, event_id, type, conversation_id, listing_id, preview_json, observed_at FROM events WHERE account_hash=? AND sequence>? ORDER BY sequence LIMIT ?`, accountHash, sequence, limit)
	if err != nil {
		return nil, fmt.Errorf("read event spool: %w", err)
	}
	defer rows.Close()
	result := make([]StoredEvent, 0, limit)
	for rows.Next() {
		var event StoredEvent
		var conversationID, listingID sql.NullString
		var data []byte
		var observed string
		if err := rows.Scan(&event.Sequence, &event.EventID, &event.Type, &conversationID, &listingID, &data, &observed); err != nil {
			return nil, fmt.Errorf("scan event spool: %w", err)
		}
		if err := json.Unmarshal(data, &event.Data); err != nil {
			return nil, fmt.Errorf("decode event spool: %w", err)
		}
		if event.Data == nil {
			return nil, fmt.Errorf("decode event spool: event data is not an object")
		}
		event.ObservedAt, err = time.Parse(time.RFC3339Nano, observed)
		if err != nil {
			return nil, fmt.Errorf("decode event observation time: %w", err)
		}
		event.AccountHash = accountHash
		event.ConversationID = conversationID.String
		event.ListingID = listingID.String
		result = append(result, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read event spool: %w", err)
	}
	return result, nil
}

func (d *DB) MessageFingerprintState(ctx context.Context, message MessageFingerprint) (bool, string, error) {
	if d == nil || d.sql == nil {
		return false, "", fmt.Errorf("state database is unavailable")
	}
	key, err := MessageFingerprintKey(message)
	if err != nil {
		return false, "", err
	}
	var digest string
	err = d.sql.QueryRowContext(ctx, `SELECT content_digest FROM messages WHERE account_hash=? AND conversation_id=? AND message_key=?`, message.AccountHash, message.ConversationID, key).Scan(&digest)
	if errors.Is(err, sql.ErrNoRows) {
		return false, "", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("read message fingerprint: %w", err)
	}
	return true, digest, nil
}

func (d *DB) SyncCycleCount(ctx context.Context, accountHash string) (int64, error) {
	if d == nil || d.queries == nil || !authAccountHashPattern.MatchString(accountHash) {
		return 0, fmt.Errorf("invalid sync cycle account")
	}
	value, err := d.queries.GetMeta(ctx, syncCycleKey(accountHash))
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count, err := strconv.ParseInt(value, 10, 64)
	if err != nil || count < 0 {
		return 0, fmt.Errorf("stored sync cycle count is invalid")
	}
	return count, nil
}

func (d *DB) AcquireSyncLease(ctx context.Context, profileUUID, accountHash, owner string, now time.Time, ttl time.Duration) (bool, error) {
	name, err := syncLeaseName(profileUUID, accountHash)
	if err != nil || validateLeasePart(owner) != nil || now.IsZero() || ttl <= 0 || ttl > time.Hour {
		return false, fmt.Errorf("invalid sync lease")
	}
	nowMS, expiresMS := now.UTC().UnixMilli(), now.UTC().Add(ttl).UnixMilli()
	var acquired bool
	err = d.WithTx(ctx, func(tx *sql.Tx, _ *Queries) error {
		result, err := tx.ExecContext(ctx, `INSERT INTO leases(name, owner, expires_at_ms) VALUES(?, ?, ?) ON CONFLICT(name) DO UPDATE SET owner=excluded.owner, expires_at_ms=excluded.expires_at_ms WHERE leases.expires_at_ms <= ? OR leases.owner=?`, name, owner, expiresMS, nowMS, owner)
		if err != nil {
			return err
		}
		rows, err := result.RowsAffected()
		acquired = rows == 1
		return err
	})
	return acquired, err
}

func (d *DB) ReleaseSyncLease(ctx context.Context, profileUUID, accountHash, owner string) error {
	name, err := syncLeaseName(profileUUID, accountHash)
	if err != nil {
		return err
	}
	return d.ReleaseLease(ctx, name, owner)
}

func syncLeaseName(profileUUID, accountHash string) (string, error) {
	if !confirmationUUIDPattern.MatchString(profileUUID) || !authAccountHashPattern.MatchString(accountHash) {
		return "", fmt.Errorf("invalid sync lease identity")
	}
	return "dm-sync:" + profileUUID + ":" + accountHash[:16], nil
}

func syncCycleKey(accountHash string) string { return "dm_sync_cycles_" + accountHash }

func (d *DB) CommitSyncBatch(ctx context.Context, batch SyncBatch) (SyncCommit, error) {
	if d == nil || d.sql == nil || !authAccountHashPattern.MatchString(batch.AccountHash) || batch.Generation < 0 || batch.InitialSequence < 0 || batch.ObservedAt.IsZero() {
		return SyncCommit{}, fmt.Errorf("invalid synchronization batch")
	}
	if batch.Retention == 0 {
		batch.Retention = DefaultEventRetention
	}
	if batch.MaxEvents == 0 {
		batch.MaxEvents = DefaultEventLimit
	}
	if batch.Retention < time.Hour || batch.MaxEvents < 1 || batch.MaxEvents > 1_000_000 || len(batch.Events) > 10_000 || len(batch.Messages) > 10_000 || len(batch.Summaries) > 500 {
		return SyncCommit{}, fmt.Errorf("invalid synchronization retention or batch bounds")
	}
	commit := SyncCommit{}
	err := d.WithTx(ctx, func(tx *sql.Tx, q *Queries) error {
		identity, err := d.cursorIdentityTx(ctx, q, batch.AccountHash)
		if err != nil {
			return err
		}
		if identity.Generation != batch.Generation {
			return ErrCursorContinuity
		}
		if batch.InitialHead {
			if _, err := tx.ExecContext(ctx, `INSERT INTO cursor_heads(account_hash, generation, acknowledged_sequence) VALUES(?, ?, ?) ON CONFLICT(account_hash) DO NOTHING`, batch.AccountHash, batch.Generation, batch.InitialSequence); err != nil {
				return err
			}
		}
		var storedGeneration int64
		if err := tx.QueryRowContext(ctx, `SELECT generation FROM cursor_heads WHERE account_hash=?`, batch.AccountHash).Scan(&storedGeneration); err != nil || storedGeneration != batch.Generation {
			if err == nil {
				return ErrCursorContinuity
			}
			return err
		}
		for _, summary := range batch.Summaries {
			if summary.AccountHash != batch.AccountHash || validateConversationSummary(summary) != nil {
				return fmt.Errorf("invalid conversation in synchronization batch")
			}
			if err := upsertConversationTx(ctx, tx, summary); err != nil {
				return err
			}
		}
		for _, message := range batch.Messages {
			if message.AccountHash != batch.AccountHash || validateMessageFingerprint(message) != nil {
				return fmt.Errorf("invalid message in synchronization batch")
			}
			if err := upsertMessageTx(ctx, tx, message); err != nil {
				return err
			}
		}
		for _, event := range batch.Events {
			if err := insertEventTx(ctx, tx, batch.AccountHash, event); err != nil {
				return err
			}
		}
		if batch.IncrementCycle {
			count, err := syncCycleCountTx(ctx, q, batch.AccountHash)
			if err != nil || count == int64(^uint64(0)>>1) {
				if err == nil {
					err = fmt.Errorf("sync cycle count overflow")
				}
				return err
			}
			commit.CycleCount = count + 1
			if err := q.SetMeta(ctx, syncCycleKey(batch.AccountHash), strconv.FormatInt(commit.CycleCount, 10)); err != nil {
				return err
			}
		}
		cutoff := batch.ObservedAt.UTC().Add(-batch.Retention).Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE account_hash=? AND unixepoch(observed_at) < unixepoch(?)`, batch.AccountHash, cutoff); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE account_hash=? AND sequence NOT IN (SELECT sequence FROM events WHERE account_hash=? ORDER BY sequence DESC LIMIT ?)`, batch.AccountHash, batch.AccountHash, batch.MaxEvents); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM events WHERE account_hash=?`, batch.AccountHash).Scan(&commit.LastSequence)
	})
	if err != nil {
		return SyncCommit{}, fmt.Errorf("commit synchronization batch: %w", err)
	}
	return commit, nil
}

func (d *DB) cursorIdentityTx(ctx context.Context, q *Queries, accountHash string) (CursorHead, error) {
	profileUUID, err := q.GetMeta(ctx, "profile_uuid")
	if err != nil {
		return CursorHead{}, err
	}
	rawGeneration, err := q.GetMeta(ctx, authCursorGenerationKey)
	if err != nil {
		return CursorHead{}, err
	}
	generation, err := strconv.ParseInt(rawGeneration, 10, 64)
	if err != nil || generation < 0 {
		return CursorHead{}, fmt.Errorf("stored cursor generation is invalid")
	}
	return CursorHead{ProfileUUID: profileUUID, AccountHash: accountHash, Generation: generation}, nil
}

func syncCycleCountTx(ctx context.Context, q *Queries, accountHash string) (int64, error) {
	value, err := q.GetMeta(ctx, syncCycleKey(accountHash))
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	count, err := strconv.ParseInt(value, 10, 64)
	if err != nil || count < 0 {
		return 0, fmt.Errorf("stored sync cycle count is invalid")
	}
	return count, nil
}

func upsertConversationTx(ctx context.Context, tx *sql.Tx, incoming ConversationSummary) error {
	previous, exists, err := conversationSummaryFrom(ctx, tx, incoming.AccountHash, incoming.ConversationID)
	if err != nil {
		return err
	}
	merged := mergeConversationSummary(previous, incoming, exists)
	merged.Preview = boundedDMPreview(merged.Preview)
	fingerprint, err := ConversationSummaryFingerprint(merged)
	if err != nil {
		return err
	}
	merged.RemoteFingerprint = fingerprint
	summaryJSON, err := json.Marshal(storedFromConversation(merged))
	if err != nil {
		return err
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
	_, err = tx.ExecContext(ctx, `INSERT INTO conversations(account_hash, conversation_id, listing_id, counterparty, summary_json, remote_fingerprint, remote_updated_at, observed_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(account_hash, conversation_id) DO UPDATE SET listing_id=COALESCE(excluded.listing_id, conversations.listing_id), counterparty=COALESCE(excluded.counterparty, conversations.counterparty), summary_json=excluded.summary_json, remote_fingerprint=excluded.remote_fingerprint, remote_updated_at=COALESCE(excluded.remote_updated_at, conversations.remote_updated_at), observed_at=excluded.observed_at`, merged.AccountHash, merged.ConversationID, listingID, counterparty, summaryJSON, fingerprint, updatedAt, merged.ObservedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func upsertMessageTx(ctx context.Context, tx *sql.Tx, message MessageFingerprint) error {
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
	_, err = tx.ExecContext(ctx, `INSERT INTO messages(account_hash, conversation_id, message_key, remote_id, direction, received_at, content_digest, observed_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(account_hash, conversation_id, message_key) DO UPDATE SET remote_id=COALESCE(excluded.remote_id, messages.remote_id), direction=excluded.direction, received_at=COALESCE(excluded.received_at, messages.received_at), content_digest=excluded.content_digest, observed_at=excluded.observed_at`, message.AccountHash, message.ConversationID, key, remoteID, strings.ToUpper(message.Direction), receivedAt, message.ContentDigest, message.ObservedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func insertEventTx(ctx context.Context, tx *sql.Tx, accountHash string, event PendingEvent) error {
	if event.EventID == "" || len(event.EventID) > 128 || event.Type == "" || len(event.Type) > 128 || event.ObservedAt.IsZero() || event.Data == nil || (event.ConversationID != "" && !dmIdentifierPattern.MatchString(event.ConversationID)) || len(event.ListingID) > 128 {
		return fmt.Errorf("invalid synchronization event")
	}
	data, err := json.Marshal(event.Data)
	if err != nil || len(data) > 64*1024 {
		return fmt.Errorf("invalid synchronization event data")
	}
	var conversationID, listingID any
	if event.ConversationID != "" {
		conversationID = event.ConversationID
	}
	if event.ListingID != "" {
		listingID = event.ListingID
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO events(event_id, account_hash, type, conversation_id, listing_id, preview_json, observed_at) VALUES(?, ?, ?, ?, ?, ?, ?) ON CONFLICT(event_id) DO NOTHING`, event.EventID, accountHash, event.Type, conversationID, listingID, data, event.ObservedAt.UTC().Format(time.RFC3339Nano))
	return err
}
