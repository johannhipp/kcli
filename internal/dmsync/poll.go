package dmsync

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/state"
)

const (
	ConversationPageSize = 100
	OrdinaryPageLimit    = 2
	ReconcilePageLimit   = 5
	ReconcileEvery       = 120
	SyncLeaseTTL         = 2 * time.Minute
)

type Clock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type ConversationClient interface {
	ListConversations(context.Context, int, int) (kleinanzeigen.ConversationPage, error)
	OpenConversation(context.Context, string) (kleinanzeigen.OpenedConversation, error)
}

type PollOptions struct {
	After       string
	Since       string
	Limit       int
	OpenChanged bool
	LeaseOwner  string
}

type PollResult struct {
	Events         []domain.EventV1
	Cursor         string
	Sequence       int64
	Generation     int64
	ProfileUUID    string
	AccountHash    string
	Warnings       []domain.WarningV1
	Fetched        int
	Pages          int
	Reconciliation bool
}

type Poller struct {
	State       *state.DB
	Client      ConversationClient
	Clock       Clock
	AccountHash string
}

func (p *Poller) Run(ctx context.Context, options PollOptions) (PollResult, error) {
	if p == nil || p.State == nil || p.Client == nil || p.Clock == nil || options.LeaseOwner == "" {
		return PollResult{}, &domain.Error{Code: domain.CodeUnavailable, Message: "DM synchronization dependencies are unavailable"}
	}
	if options.Limit == 0 {
		options.Limit = 200
	}
	if options.Limit < 1 || options.Limit > 500 || (options.After != "" && options.Since != "") {
		return PollResult{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "DM poll requires a limit between 1 and 500 and only one starting position"}
	}
	identity, err := p.State.CursorIdentity(ctx, p.AccountHash)
	if err != nil {
		return PollResult{}, unavailable("read synchronization identity", err)
	}
	head, hasHead, err := p.State.CursorHead(ctx, p.AccountHash)
	if err != nil {
		return PollResult{}, continuityError("local cursor head is unreadable", err)
	}
	startSequence, initialHead, baselineNow, sinceTime, err := p.startPosition(ctx, options, identity, head, hasHead)
	if err != nil {
		return PollResult{}, err
	}
	acquired, err := p.State.AcquireSyncLease(ctx, identity.ProfileUUID, p.AccountHash, options.LeaseOwner, p.Clock.Now().UTC(), SyncLeaseTTL)
	if err != nil {
		return PollResult{}, unavailable("acquire DM synchronization lease", err)
	}
	if !acquired {
		return PollResult{}, &domain.Error{Code: domain.CodeUnavailable, Message: "another DM synchronization cycle is already active", Retryable: true}
	}
	defer func() {
		releaseContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = p.State.ReleaseSyncLease(releaseContext, identity.ProfileUUID, p.AccountHash, options.LeaseOwner)
	}()

	cycleCount, err := p.State.SyncCycleCount(ctx, p.AccountHash)
	if err != nil {
		return PollResult{}, continuityError("local synchronization cycle state is unreadable", err)
	}
	pageLimit := OrdinaryPageLimit
	reconciliation := (cycleCount+1)%ReconcileEvery == 0
	if reconciliation {
		pageLimit = ReconcilePageLimit
	}
	observedAt := p.Clock.Now().UTC()
	remote, warnings, pages, err := p.fetchConversations(ctx, pageLimit)
	if err != nil {
		return PollResult{}, err
	}

	summaries := make([]state.ConversationSummary, 0, len(remote))
	messages := make([]state.MessageFingerprint, 0)
	pending := make([]state.PendingEvent, 0)
	for _, conversation := range remote {
		incoming := conversationState(p.AccountHash, conversation, observedAt)
		previous, previousErr := p.State.ConversationSummary(ctx, p.AccountHash, conversation.ID)
		exists := previousErr == nil
		if previousErr != nil && !errors.Is(previousErr, sql.ErrNoRows) {
			return PollResult{}, continuityError("stored conversation state is unreadable", previousErr)
		}
		if exists {
			preserveUnknownSummary(&incoming, conversation, previous)
		}
		fingerprint, fingerprintErr := state.ConversationSummaryFingerprint(incoming)
		if fingerprintErr != nil {
			return PollResult{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "conversation summary could not be fingerprinted", Cause: fingerprintErr}
		}
		changed := !exists || previous.RemoteFingerprint != fingerprint
		summaries = append(summaries, incoming)
		if baselineNow {
			continue
		}
		if !sinceTime.IsZero() {
			observed, parseErr := time.Parse(time.RFC3339Nano, incoming.LastActivity)
			if parseErr != nil {
				warnings = append(warnings, domain.WarningV1{Code: "conversation_timestamp_unparseable", Message: "a conversation had an unparseable last-activity timestamp and was excluded from the since window"})
				continue
			}
			if observed.Before(sinceTime) {
				continue
			}
		}
		if changed {
			pending = append(pending, conversationEvents(p.AccountHash, previous, incoming, fingerprint, exists, observedAt)...)
		}
		if !options.OpenChanged || !changed {
			continue
		}
		opened, openErr := p.Client.OpenConversation(ctx, conversation.ID)
		if openErr != nil {
			return PollResult{}, openErr
		}
		openedMessages := append([]kleinanzeigen.Message(nil), opened.Messages...)
		sort.SliceStable(openedMessages, func(i, j int) bool {
			left, right := openedMessages[i].ReceivedAt, openedMessages[j].ReceivedAt
			if left.IsZero() != right.IsZero() {
				return !left.IsZero()
			}
			return left.Before(right)
		})
		for _, message := range openedMessages {
			messageState := messageFingerprint(p.AccountHash, conversation.ID, message, observedAt)
			key, keyErr := state.MessageFingerprintKey(messageState)
			if keyErr != nil {
				warnings = append(warnings, domain.WarningV1{Code: "message_fingerprint_unavailable", Message: "a changed conversation contained a message without safe fingerprint metadata"})
				continue
			}
			known, oldDigest, stateErr := p.State.MessageFingerprintState(ctx, messageState)
			if stateErr != nil {
				return PollResult{}, continuityError("stored message fingerprint is unreadable", stateErr)
			}
			messages = append(messages, messageState)
			switch {
			case !known:
				pending = append(pending, messageCreatedEvent(p.AccountHash, incoming, messageState, key, observedAt))
			case oldDigest != messageState.ContentDigest:
				pending = append(pending, messageCollisionEvent(p.AccountHash, incoming, key, messageState.ContentDigest, observedAt))
			}
		}
	}

	_, err = p.State.CommitSyncBatch(ctx, state.SyncBatch{
		AccountHash: p.AccountHash, Generation: identity.Generation, InitialHead: initialHead,
		InitialSequence: startSequence, Summaries: summaries, Messages: messages, Events: pending,
		ObservedAt: observedAt, IncrementCycle: true,
	})
	if err != nil {
		if errors.Is(err, state.ErrCursorContinuity) {
			return PollResult{}, continuityError("local cursor changed during synchronization", err)
		}
		return PollResult{}, unavailable("commit DM synchronization batch", err)
	}
	stored, err := p.State.ReadEventsAfter(ctx, p.AccountHash, startSequence, options.Limit)
	if err != nil {
		return PollResult{}, continuityError("read committed event spool", err)
	}
	result := PollResult{
		Events: make([]domain.EventV1, 0, len(stored)), Sequence: startSequence,
		Generation: identity.Generation, ProfileUUID: identity.ProfileUUID, AccountHash: p.AccountHash,
		Warnings: warnings, Fetched: len(remote), Pages: pages, Reconciliation: reconciliation,
	}
	for _, event := range stored {
		cursor, encodeErr := encodeCursor(identity, event.Sequence)
		if encodeErr != nil {
			return PollResult{}, continuityError("encode event cursor", encodeErr)
		}
		result.Events = append(result.Events, domain.EventV1{
			Schema: domain.EventSchemaV1, EventID: event.EventID, Cursor: cursor, Type: event.Type,
			ObservedAt: event.ObservedAt.UTC(), AccountID: accountPseudonym(p.AccountHash),
			ConversationID: event.ConversationID, ListingID: event.ListingID, Data: event.Data,
		})
		result.Sequence = event.Sequence
	}
	result.Cursor, err = encodeCursor(identity, result.Sequence)
	if err != nil {
		return PollResult{}, continuityError("encode result cursor", err)
	}
	return result, nil
}

func (p *Poller) startPosition(ctx context.Context, options PollOptions, identity, head state.CursorHead, hasHead bool) (int64, bool, bool, time.Time, error) {
	if options.After != "" {
		payload, err := domain.DecodeCursor(options.After)
		if err != nil || p.State.ValidateCursor(ctx, p.AccountHash, payload) != nil {
			return 0, false, false, time.Time{}, continuityError("cursor is unknown, stale, or belongs to another account or state store", err)
		}
		return payload.Sequence, false, false, time.Time{}, nil
	}
	if options.Since != "" {
		if options.Since == "now" {
			start := int64(0)
			if hasHead {
				start = head.Acknowledged
			}
			return start, !hasHead, true, time.Time{}, nil
		}
		since, err := time.Parse(time.RFC3339, options.Since)
		if err != nil {
			return 0, false, false, time.Time{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "--since must be now or an RFC 3339 timestamp"}
		}
		start := int64(0)
		if hasHead {
			start = head.Acknowledged
		}
		return start, !hasHead, false, since.UTC(), nil
	}
	if !hasHead {
		return 0, false, false, time.Time{}, &domain.Error{Code: domain.CodeResyncRequired, Message: "first DM synchronization requires --since now or a bounded RFC 3339 timestamp"}
	}
	if head.ProfileUUID != identity.ProfileUUID || head.Generation != identity.Generation {
		return 0, false, false, time.Time{}, continuityError("stored cursor generation does not match this profile", nil)
	}
	return head.Acknowledged, false, false, time.Time{}, nil
}

func (p *Poller) fetchConversations(ctx context.Context, pageLimit int) ([]kleinanzeigen.ConversationSummary, []domain.WarningV1, int, error) {
	result := make([]kleinanzeigen.ConversationSummary, 0, pageLimit*ConversationPageSize)
	warnings := make([]domain.WarningV1, 0)
	seen := make(map[string]struct{}, pageLimit*ConversationPageSize)
	pages := 0
	for pageNumber := 0; pageNumber < pageLimit; pageNumber++ {
		page, err := p.Client.ListConversations(ctx, pageNumber, ConversationPageSize)
		if err != nil {
			return nil, nil, pages, err
		}
		pages++
		warnings = append(warnings, page.Warnings...)
		newIDs := 0
		for _, conversation := range page.Conversations {
			if _, duplicate := seen[conversation.ID]; duplicate {
				continue
			}
			seen[conversation.ID] = struct{}{}
			newIDs++
			result = append(result, conversation)
		}
		if len(page.Conversations) < ConversationPageSize || newIDs == 0 {
			break
		}
	}
	return result, warnings, pages, nil
}

func conversationState(accountHash string, remote kleinanzeigen.ConversationSummary, observedAt time.Time) state.ConversationSummary {
	lastActivity := remote.ReceivedRaw
	if !remote.ReceivedAt.IsZero() {
		lastActivity = remote.ReceivedAt.UTC().Format(time.RFC3339Nano)
	}
	return state.ConversationSummary{
		AccountHash: accountHash, ConversationID: remote.ID, ListingID: remote.ListingID,
		ListingTitle: remote.ListingTitle, ListingStatus: remote.ListingStatus, Role: remote.Role,
		Counterparty: remote.Counterparty, Unread: remote.Unread, UnreadMessageCount: remote.UnreadMessageCount,
		MessageCount: remote.MessageCount, LastActivity: lastActivity, Preview: boundedPreview(remote.Preview), ObservedAt: observedAt,
	}
}

func preserveUnknownSummary(incoming *state.ConversationSummary, remote kleinanzeigen.ConversationSummary, previous state.ConversationSummary) {
	if !remote.UnreadKnown {
		incoming.Unread = previous.Unread
	}
	if !remote.UnreadCountKnown {
		incoming.UnreadMessageCount = previous.UnreadMessageCount
	}
	if !remote.MessageCountKnown {
		incoming.MessageCount = previous.MessageCount
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
}

func conversationEvents(accountHash string, previous, incoming state.ConversationSummary, fingerprint string, exists bool, observedAt time.Time) []state.PendingEvent {
	kind := "dm.conversation.created"
	data := conversationData(incoming)
	if exists {
		if previous.Unread != incoming.Unread || previous.UnreadMessageCount != incoming.UnreadMessageCount {
			kind = "dm.conversation.read_changed"
			data["previous_unread"] = previous.Unread
			data["previous_unread_message_count"] = previous.UnreadMessageCount
		} else {
			kind = "dm.conversation.updated"
		}
	}
	return []state.PendingEvent{{
		EventID: stableEventID(accountHash, kind, incoming.ConversationID, fingerprint), Type: kind,
		ConversationID: incoming.ConversationID, ListingID: incoming.ListingID, Data: data, ObservedAt: observedAt,
	}}
}

func conversationData(summary state.ConversationSummary) map[string]any {
	return map[string]any{
		"unread": summary.Unread, "unread_message_count": summary.UnreadMessageCount,
		"message_count": summary.MessageCount, "preview": boundedPreview(summary.Preview), "complete": false,
	}
}

func messageFingerprint(accountHash, conversationID string, message kleinanzeigen.Message, observedAt time.Time) state.MessageFingerprint {
	direction := strings.ToUpper(message.Direction)
	if direction == "" {
		direction = "UNKNOWN"
	}
	kind := message.Kind
	if kind == "" {
		kind = "UNKNOWN"
	}
	received := message.ReceivedRaw
	if !message.ReceivedAt.IsZero() {
		received = message.ReceivedAt.UTC().Format(time.RFC3339Nano)
	}
	return state.MessageFingerprint{
		AccountHash: accountHash, ConversationID: conversationID, RemoteID: message.ID,
		Direction: direction, ReceivedAt: received, Kind: kind,
		ContentDigest: state.DigestMessageContent(message.Text), ObservedAt: observedAt,
	}
}

func messageCreatedEvent(accountHash string, conversation state.ConversationSummary, message state.MessageFingerprint, key string, observedAt time.Time) state.PendingEvent {
	data := map[string]any{"message_key": key, "direction": message.Direction, "kind": message.Kind, "received_at": message.ReceivedAt, "complete": false}
	return state.PendingEvent{
		EventID: stableEventID(accountHash, "dm.message.created", conversation.ConversationID, key, message.ContentDigest),
		Type:    "dm.message.created", ConversationID: conversation.ConversationID, ListingID: conversation.ListingID,
		Data: data, ObservedAt: observedAt,
	}
}

func messageCollisionEvent(accountHash string, conversation state.ConversationSummary, key, digest string, observedAt time.Time) state.PendingEvent {
	return state.PendingEvent{
		EventID: stableEventID(accountHash, "dm.conversation.updated", conversation.ConversationID, "message-edit-or-collision", key, digest),
		Type:    "dm.conversation.updated", ConversationID: conversation.ConversationID, ListingID: conversation.ListingID,
		Data: map[string]any{"reason": "message_edit_or_fingerprint_collision", "message_key": key, "complete": false}, ObservedAt: observedAt,
	}
}

func stableEventID(fields ...string) string {
	digest := sha256.New()
	for _, field := range append([]string{"kcli-event-id/v1"}, fields...) {
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(len(field)))
		_, _ = digest.Write(size[:])
		_, _ = digest.Write([]byte(field))
	}
	return "evt_" + hex.EncodeToString(digest.Sum(nil))
}

func encodeCursor(identity state.CursorHead, sequence int64) (string, error) {
	return domain.EncodeCursor(domain.CursorPayloadV1{
		FormatVersion: domain.CursorFormatV1, ProfileUUID: identity.ProfileUUID,
		AccountSubjectHash: identity.AccountHash, Generation: identity.Generation, Sequence: sequence,
	})
}

func boundedPreview(value string) string {
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

func accountPseudonym(accountHash string) string {
	if len(accountHash) < 16 {
		return "acct_unknown"
	}
	return "acct_" + accountHash[:16]
}

func continuityError(message string, cause error) error {
	return &domain.Error{Code: domain.CodeResyncRequired, Message: message, Cause: cause}
}

func unavailable(message string, cause error) error {
	return &domain.Error{Code: domain.CodeUnavailable, Message: message, Cause: cause}
}

func IsRetryable(err error) bool {
	var typed *domain.Error
	return errors.As(err, &typed) && typed.Retryable
}

func RetryAfter(err error) time.Duration {
	var typed *domain.Error
	if errors.As(err, &typed) && typed.RetryAfter != nil && *typed.RetryAfter > 0 {
		return *typed.RetryAfter
	}
	return 0
}

func ErrorCode(err error) domain.ErrorCode {
	var typed *domain.Error
	if errors.As(err, &typed) {
		return typed.Code
	}
	return ""
}

func EventCountByType(events []domain.EventV1) map[string]int {
	counts := make(map[string]int)
	for _, event := range events {
		counts[event.Type]++
	}
	return counts
}
