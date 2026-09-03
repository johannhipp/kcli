package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/state"
)

type DMConversationV1 struct {
	domain.ConversationV1
	ListingTitle       string `json:"listing_title,omitempty"`
	ListingStatus      string `json:"listing_status,omitempty"`
	Role               string `json:"role,omitempty"`
	Counterparty       string `json:"counterparty,omitempty"`
	UnreadMessageCount int    `json:"unread_message_count"`
	LastActivity       string `json:"last_activity,omitempty"`
	Source             string `json:"source"`
}

type DMListOutputV1 struct {
	domain.Envelope[[]DMConversationV1]
}

type DMMessageV1 struct {
	domain.MessageV1
	Kind        string          `json:"kind"`
	ReceivedRaw string          `json:"received_at_raw,omitempty"`
	Raw         json.RawMessage `json:"raw,omitempty"`
}

func (a *App) DMList(ctx context.Context, profile, requestID string, input domain.DMListInputV1) (DMListOutputV1, error) {
	canonical, err := canonicalDMListInput(input)
	if err != nil {
		return DMListOutputV1{}, err
	}
	account, client, err := a.dmClient(ctx, profile, requestID)
	if err != nil {
		return DMListOutputV1{}, err
	}
	observedAt := a.Clock.Now().UTC()
	items := make([]DMConversationV1, 0, canonical.Limit)
	warnings := []domain.WarningV1{}
	seen := make(map[string]struct{})
	fetched := 0
	pageNumber := canonical.Page
	stopReason := ""
	for {
		if canonical.Paginate && fetched > 0 && fetched+canonical.PageSize > 500 {
			stopReason = "scan_bound"
			break
		}
		page, fetchErr := client.ListConversations(ctx, pageNumber, canonical.PageSize)
		if fetchErr != nil {
			return DMListOutputV1{}, fetchErr
		}
		fetched += len(page.Conversations)
		warnings = append(warnings, page.Warnings...)
		newIDs := 0
		for _, remote := range page.Conversations {
			if _, duplicate := seen[remote.ID]; duplicate {
				continue
			}
			seen[remote.ID] = struct{}{}
			newIDs++
			stored, storeErr := a.State.UpsertConversationSummary(ctx, stateConversation(account.SubjectHash, remote, observedAt))
			if storeErr != nil {
				return DMListOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "store conversation summary", Cause: storeErr}
			}
			if canonical.Unread && !stored.Unread {
				continue
			}
			items = append(items, outputConversation(stored))
			if len(items) == canonical.Limit {
				break
			}
		}
		switch {
		case len(items) >= canonical.Limit:
			stopReason = "bound"
		case len(page.Conversations) < canonical.PageSize:
			stopReason = "last_page"
		case newIDs == 0:
			stopReason = "no_new_ids"
		case !canonical.Paginate:
			stopReason = "single_page"
		}
		if stopReason != "" {
			break
		}
		pageNumber++
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, leftKnown := parseDMOutputTime(items[i].LastActivity)
		right, rightKnown := parseDMOutputTime(items[j].LastActivity)
		if leftKnown != rightKnown {
			return leftKnown
		}
		return left.After(right)
	})
	warnings = append(warnings, domain.WarningV1{Code: "dm_list_metadata", Message: "conversation listing completed at a bounded stop condition", Details: map[string]any{"stop_reason": stopReason, "page": canonical.Page, "page_size": canonical.PageSize, "limit": canonical.Limit}})
	envelope := Envelope(a.Clock, "kcli.conversations/v1", requestID, "message-gateway", items)
	envelope.ObservedAt = observedAt
	envelope.Page = &domain.PageV1{Number: canonical.Page, Size: canonical.PageSize, Fetched: fetched, Returned: len(items)}
	envelope.Warnings = warnings
	if len(warnings) > 1 {
		envelope.Completeness = domain.CompletenessPartial
	}
	if stopReason == "single_page" {
		next := strconv.Itoa(canonical.Page + 1)
		envelope.Next = &next
	}
	return DMListOutputV1{Envelope: envelope}, nil
}

func (a *App) DMGet(ctx context.Context, profile, requestID string, input domain.DMGetInputV1) (domain.DMOutputV1, error) {
	if err := kleinanzeigen.ValidateConversationID(input.ConversationID); err != nil {
		return domain.DMOutputV1{}, err
	}
	account, client, err := a.dmClient(ctx, profile, requestID)
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	previous, previousErr := a.State.ConversationSummary(ctx, account.SubjectHash, input.ConversationID)
	if previousErr != nil && previousErr != sql.ErrNoRows {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "read conversation context", Cause: previousErr}
	}
	opened, err := client.OpenConversation(ctx, input.ConversationID)
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	observedAt := a.Clock.Now().UTC()
	incoming := stateConversation(account.SubjectHash, opened.Conversation, observedAt)
	if previousErr == nil {
		if !opened.Conversation.UnreadKnown {
			incoming.Unread = previous.Unread
		}
		if !opened.Conversation.UnreadCountKnown {
			incoming.UnreadMessageCount = previous.UnreadMessageCount
		}
		if !opened.Conversation.MessageCountKnown {
			incoming.MessageCount = previous.MessageCount
		}
		if incoming.Preview == "" {
			incoming.Preview = previous.Preview
		}
	}
	if incoming.MessageCount == 0 && len(opened.Messages) > 0 {
		incoming.MessageCount = len(opened.Messages)
	}
	if incoming.LastActivity == "" {
		incoming.LastActivity = newestMessageTime(opened.Messages)
	}
	stored, err := a.State.UpsertConversationSummary(ctx, incoming)
	if err != nil {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "store conversation context", Cause: err}
	}
	messages := append([]kleinanzeigen.Message(nil), opened.Messages...)
	sort.SliceStable(messages, func(i, j int) bool {
		left, right := messages[i].ReceivedAt, messages[j].ReceivedAt
		if left.IsZero() != right.IsZero() {
			return !left.IsZero()
		}
		return left.Before(right)
	})
	fingerprints := make([]state.MessageFingerprint, 0, len(messages))
	outputMessages := make([]DMMessageV1, 0, len(messages))
	warnings := append([]domain.WarningV1(nil), opened.Warnings...)
	for _, message := range messages {
		received := message.ReceivedRaw
		if !message.ReceivedAt.IsZero() {
			received = message.ReceivedAt.UTC().Format(time.RFC3339Nano)
		} else if received == "" {
			warnings = append(warnings, domain.WarningV1{Code: "message_missing_received_at", Message: "a message had no usable received time; its relative order is not authoritative"})
		}
		direction := message.Direction
		if direction == "" {
			direction = "UNKNOWN"
		}
		fingerprints = append(fingerprints, state.MessageFingerprint{
			AccountHash: account.SubjectHash, ConversationID: input.ConversationID,
			RemoteID: message.ID, Direction: direction, ReceivedAt: received, Kind: message.Kind,
			ContentDigest: state.DigestMessageContent(message.Text), ObservedAt: observedAt,
		})
		outputMessages = append(outputMessages, DMMessageV1{
			MessageV1: domain.MessageV1{
				ID: message.ID, ConversationID: input.ConversationID, Direction: direction,
				ReceivedAt: message.ReceivedAt, Text: message.Text,
			},
			Kind: message.Kind, ReceivedRaw: message.ReceivedRaw, Raw: append(json.RawMessage(nil), message.Raw...),
		})
	}
	if err := a.State.UpsertMessageFingerprints(ctx, fingerprints); err != nil {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "store message fingerprints", Cause: err}
	}
	data := map[string]any{
		"conversation": outputConversation(stored), "messages": outputMessages,
		"message_count": len(outputMessages), "account_state_touching": true,
		"side_effect": "account-state", "operation": "open-conversation",
	}
	envelope := Envelope(a.Clock, "kcli.conversation/v1", requestID, "message-gateway", data)
	envelope.ObservedAt = observedAt
	envelope.Warnings = warnings
	if len(warnings) > 0 {
		envelope.Completeness = domain.CompletenessPartial
	}
	return domain.DMOutputV1{Envelope: envelope}, nil
}

func (a *App) DMMarkRead(ctx context.Context, profile, requestID string, input domain.DMMarkReadInputV1) (domain.DMOutputV1, error) {
	if len(input.ConversationIDs) == 0 || len(input.ConversationIDs) > 100 {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "mark-read requires between 1 and 100 conversation IDs"}
	}
	account, client, err := a.dmClient(ctx, profile, requestID)
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	ids := append([]string(nil), input.ConversationIDs...)
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if err := kleinanzeigen.ValidateConversationID(id); err != nil {
			return domain.DMOutputV1{}, err
		}
		if _, duplicate := seen[id]; duplicate {
			return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "conversation IDs must be unique"}
		}
		seen[id] = struct{}{}
	}
	summaries, err := a.State.ConversationSummaries(ctx, account.SubjectHash, ids)
	if err != nil {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "validate conversation ownership", Cause: err}
	}
	owned := make(map[string]struct{}, len(summaries))
	for _, summary := range summaries {
		owned[summary.ConversationID] = struct{}{}
	}
	for _, id := range ids {
		if _, ok := owned[id]; !ok {
			return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeNotFound, Message: "conversation does not belong to the signed-in account", Details: map[string]any{"conversation_id": id}}
		}
	}
	targets := make([]DMConversationV1, 0, len(summaries))
	for _, summary := range summaries {
		targets = append(targets, outputConversation(summary))
	}
	observedAt := a.Clock.Now().UTC()
	data := map[string]any{
		"conversation_ids": ids, "conversation_count": len(ids), "conversations": targets, "dry_run": input.DryRun,
		"account": dmAccountPseudonym(account.SubjectHash), "account_state_touching": !input.DryRun,
		"side_effect": "account-state", "operation": "mark-conversations-read",
	}
	if input.DryRun {
		data["would_mark_read"] = true
		data["network_call"] = false
		return dmMapOutput(a.Clock, requestID, "local-plan", observedAt, data, nil), nil
	}
	err = client.MarkConversationsRead(ctx, ids)
	if err == nil {
		if updateErr := a.State.MarkConversationSummariesRead(ctx, account.SubjectHash, ids, observedAt); updateErr != nil {
			return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "update local conversation read state", Cause: updateErr}
		}
		data["marked_read"] = true
		data["network_call"] = true
		data["outcome"] = "confirmed"
		return dmMapOutput(a.Clock, requestID, "message-gateway", observedAt, data, nil), nil
	}
	if !dmAmbiguousRequestError(err) {
		return domain.DMOutputV1{}, err
	}
	reconciled, reconcileErr := a.dmReconcileRead(ctx, client, account.SubjectHash, ids)
	if reconcileErr == nil && reconciled {
		data["marked_read"] = true
		data["network_call"] = true
		data["outcome"] = "confirmed-by-reconciliation"
		data["reconciled"] = true
		return dmMapOutput(a.Clock, requestID, "message-gateway-reconciliation", a.Clock.Now().UTC(), data, nil), nil
	}
	return domain.DMOutputV1{}, &domain.Error{
		Code: domain.CodeAmbiguousExternalState, Message: "mark-read outcome could not be confirmed; the mutation was not repeated",
		Details: map[string]any{"conversation_ids": ids, "reconciled": reconcileErr == nil, "mutation_repeated": false}, Cause: err,
	}
}

func (a *App) dmClient(ctx context.Context, profile, requestID string) (state.AuthAccountState, *kleinanzeigen.MessageClient, error) {
	if a == nil || a.State == nil || a.Transport == nil || a.Clock == nil || a.Secrets == nil {
		return state.AuthAccountState{}, nil, &domain.Error{Code: domain.CodeUnavailable, Message: "DM runtime dependencies are unavailable"}
	}
	account, exists, err := a.State.AuthAccount(ctx)
	if err != nil {
		return state.AuthAccountState{}, nil, &domain.Error{Code: domain.CodeUnavailable, Message: "read signed-in account state", Cause: err}
	}
	if !exists || account.LoginRequired {
		return state.AuthAccountState{}, nil, &domain.Error{Code: domain.CodeAuthRequired, Message: "login is required"}
	}
	client, err := kleinanzeigen.NewMessageClient(account.AccountID, func(callContext context.Context, request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
		return a.AuthDo(callContext, profile, requestID, request)
	})
	return account, client, err
}

func canonicalDMListInput(input domain.DMListInputV1) (domain.DMListInputV1, error) {
	if input.PageSize == 0 {
		input.PageSize = 50
	}
	if input.Limit == 0 {
		input.Limit = 50
	}
	if input.Page < 0 || input.PageSize < 1 || input.PageSize > 100 || input.Limit < 1 || input.Limit > 500 {
		return input, &domain.Error{Code: domain.CodeInvalidInput, Message: "DM list requires a non-negative page, page size between 1 and 100, and limit between 1 and 500"}
	}
	return input, nil
}

func stateConversation(accountHash string, remote kleinanzeigen.ConversationSummary, observedAt time.Time) state.ConversationSummary {
	lastActivity := remote.ReceivedRaw
	if !remote.ReceivedAt.IsZero() {
		lastActivity = remote.ReceivedAt.UTC().Format(time.RFC3339Nano)
	}
	return state.ConversationSummary{
		AccountHash: accountHash, ConversationID: remote.ID, ListingID: remote.ListingID,
		ListingTitle: remote.ListingTitle, ListingStatus: remote.ListingStatus, Role: remote.Role,
		Counterparty: remote.Counterparty, Unread: remote.Unread, UnreadMessageCount: remote.UnreadMessageCount,
		MessageCount: remote.MessageCount, LastActivity: lastActivity, Preview: remote.Preview, ObservedAt: observedAt,
	}
}

func outputConversation(summary state.ConversationSummary) DMConversationV1 {
	return DMConversationV1{
		ConversationV1: domain.ConversationV1{
			ID: summary.ConversationID, ListingID: summary.ListingID, Unread: summary.Unread,
			MessageCount: summary.MessageCount, Preview: summary.Preview,
		},
		ListingTitle: summary.ListingTitle, ListingStatus: summary.ListingStatus,
		Role: summary.Role, Counterparty: summary.Counterparty,
		UnreadMessageCount: summary.UnreadMessageCount, LastActivity: summary.LastActivity,
		Source: "message-gateway",
	}
}

func newestMessageTime(messages []kleinanzeigen.Message) string {
	var newest time.Time
	var raw string
	for _, message := range messages {
		if message.ReceivedAt.After(newest) {
			newest = message.ReceivedAt
			raw = message.ReceivedAt.UTC().Format(time.RFC3339Nano)
		} else if newest.IsZero() && raw == "" && message.ReceivedRaw != "" {
			raw = message.ReceivedRaw
		}
	}
	return raw
}

func dmAccountPseudonym(hash string) string {
	if len(hash) > 12 {
		hash = hash[:12]
	}
	return "acct_" + hash
}

func dmMapOutput(clock Clock, requestID, source string, observedAt time.Time, data map[string]any, warnings []domain.WarningV1) domain.DMOutputV1 {
	envelope := Envelope(clock, "kcli.dm-mark-read/v1", requestID, source, data)
	envelope.ObservedAt = observedAt
	if warnings != nil {
		envelope.Warnings = warnings
	}
	return domain.DMOutputV1{Envelope: envelope}
}

func dmAmbiguousRequestError(err error) bool {
	var typed *domain.Error
	if !errors.As(err, &typed) {
		return true
	}
	return typed.Code == domain.CodeConnectivity || typed.Code == domain.CodeInterrupted
}

func (a *App) dmReconcileRead(ctx context.Context, client *kleinanzeigen.MessageClient, accountHash string, ids []string) (bool, error) {
	wanted := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		wanted[id] = struct{}{}
	}
	foundRead := make(map[string]bool, len(ids))
	for pageNumber := 0; pageNumber < 5 && len(foundRead) < len(wanted); pageNumber++ {
		page, err := client.ListConversations(ctx, pageNumber, 100)
		if err != nil {
			return false, err
		}
		for _, remote := range page.Conversations {
			if _, target := wanted[remote.ID]; target {
				foundRead[remote.ID] = !remote.Unread
			}
		}
		if len(page.Conversations) < 100 {
			break
		}
	}
	if len(foundRead) != len(wanted) {
		return false, nil
	}
	for _, read := range foundRead {
		if !read {
			return false, nil
		}
	}
	if err := a.State.MarkConversationSummariesRead(ctx, accountHash, ids, a.Clock.Now().UTC()); err != nil {
		return false, err
	}
	return true, nil
}

func parseDMOutputTime(value string) (time.Time, bool) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil
}
