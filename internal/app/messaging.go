package app

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/platform"
	"github.com/johannhipp/kcli/internal/state"
	"golang.org/x/text/unicode/norm"
)

const (
	messageMaximumBytes     = 64 << 10
	messageReconcileWindow  = 2 * time.Minute
	boundedConversationScan = 5
)

func (a *App) DMReply(ctx context.Context, profile, requestID string, input domain.DMReplyInputV1) (domain.DMOutputV1, error) {
	if err := platform.ValidateProfile(profile); err != nil {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid profile binding", Cause: err}
	}
	if err := validateMessagingPhase(input.DryRun, input.Confirm, input.AcknowledgeWarning, input.AcknowledgePossibleDuplicate); err != nil {
		return domain.DMOutputV1{}, err
	}
	if err := kleinanzeigen.ValidateConversationID(input.ConversationID); err != nil {
		return domain.DMOutputV1{}, err
	}
	if err := validateAppMessageInput(input.Message, input.MessageFile, input.Input); err != nil {
		return domain.DMOutputV1{}, err
	}
	identity, err := a.messagingIdentity(ctx)
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	digest := state.DigestMessageContent(input.Message)
	target := replyConfirmationTarget(input.ConversationID, profile)
	if input.DryRun {
		return a.dmReplyPlan(ctx, profile, requestID, identity, target, digest, input)
	}
	plan, err := a.claimMessagingPlan(ctx, state.ClaimPlanInput{
		ConfirmationID: input.Confirm, ProfileUUID: identity.profileUUID,
		AccountHash: identity.account.SubjectHash, OperationKind: state.ConfirmationKindReply,
		TargetID: target, MessageDigest: digest, InitialStage: state.ConfirmationStageExecutingSend,
		Now: a.Clock.Now().UTC(),
	})
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	_, client, err := a.dmClient(ctx, profile, requestID)
	if err != nil {
		return domain.DMOutputV1{}, a.failClaimedPlan(ctx, plan.ConfirmationID, err)
	}
	return a.attemptMessageSend(ctx, profile, requestID, client, plan, input.ConversationID, input.Message)
}

func (a *App) DMStart(ctx context.Context, profile, requestID string, input domain.DMStartInputV1) (domain.DMOutputV1, error) {
	if err := platform.ValidateProfile(profile); err != nil {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid profile binding", Cause: err}
	}
	if err := validateMessagingPhase(input.DryRun, input.Confirm, input.AcknowledgeWarning, input.AcknowledgePossibleDuplicate); err != nil {
		return domain.DMOutputV1{}, err
	}
	if err := validateAppMessageInput(input.Message, input.MessageFile, input.Input); err != nil {
		return domain.DMOutputV1{}, err
	}
	listingID, err := kleinanzeigen.ListingReferenceID(input.ListingIDOrURL)
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	if !validMessagingContactName(input.ContactName) {
		return domain.DMOutputV1{}, &domain.Error{
			Code:    domain.CodeConfirmationRequired,
			Message: "--contact-name is required because the platform default is not verified",
			Details: map[string]any{"contact_name_default": "BLOCKED_PENDING_TWO_ACCOUNT_LIVE_PROOF"},
		}
	}
	identity, err := a.messagingIdentity(ctx)
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	digest := state.DigestMessageContent(input.Message)
	target := startConfirmationTarget(listingID, input.ContactName, profile)
	if input.DryRun {
		return a.dmStartPlan(ctx, profile, requestID, identity, listingID, target, digest, input)
	}
	plan, err := a.claimMessagingPlan(ctx, state.ClaimPlanInput{
		ConfirmationID: input.Confirm, ProfileUUID: identity.profileUUID,
		AccountHash: identity.account.SubjectHash, OperationKind: state.ConfirmationKindStart,
		TargetID: target, MessageDigest: digest, InitialStage: state.ConfirmationStageExecutingCreate,
		Now: a.Clock.Now().UTC(),
	})
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	_, client, err := a.dmClient(ctx, profile, requestID)
	if err != nil {
		return domain.DMOutputV1{}, a.failClaimedPlan(ctx, plan.ConfirmationID, err)
	}
	before, complete, _, err := scanConversations(ctx, client, "", boundedConversationScan)
	if err != nil {
		return domain.DMOutputV1{}, a.failClaimedPlan(ctx, plan.ConfirmationID, err)
	}
	if !complete {
		err := &domain.Error{Code: domain.CodeDuplicateUnresolved, Message: "could not prove that the listing has no existing conversation within the bounded inbox scan", Details: map[string]any{"listing_id": listingID, "create_attempted": false}}
		return domain.DMOutputV1{}, a.failClaimedPlan(ctx, plan.ConfirmationID, err)
	}
	if existing := conversationsForListing(before, listingID); len(existing) != 0 {
		err := existingConversationError(listingID, existing[0].ID)
		return domain.DMOutputV1{}, a.failClaimedPlan(ctx, plan.ConfirmationID, err)
	}
	attemptedAt := a.Clock.Now().UTC()
	conversationID, err := client.CreateConversation(ctx, listingID, input.ContactName)
	if err != nil {
		if externalFailureDefinite(err) {
			return domain.DMOutputV1{}, a.failClaimedPlan(ctx, plan.ConfirmationID, err)
		}
		return a.reconcileAmbiguousCreate(ctx, profile, requestID, client, plan, before, listingID, input.Message, attemptedAt, err)
	}
	stateCtx := context.WithoutCancel(ctx)
	if err := a.State.MarkConversationCreated(stateCtx, plan.ConfirmationID, conversationID, a.Clock.Now().UTC()); err != nil {
		return domain.DMOutputV1{}, messagingAuditError("record created conversation", err)
	}
	if err := a.State.MarkExecutingSend(stateCtx, plan.ConfirmationID, a.Clock.Now().UTC()); err != nil {
		return domain.DMOutputV1{}, messagingAuditError("record first-message attempt", err)
	}
	plan.Stage = state.ConfirmationStageExecutingSend
	return a.attemptMessageSend(ctx, profile, requestID, client, plan, conversationID, input.Message)
}

type messagingIdentity struct {
	profileUUID string
	account     state.AuthAccountState
}

func (a *App) messagingIdentity(ctx context.Context) (messagingIdentity, error) {
	if a == nil || a.State == nil || a.Transport == nil || a.Secrets == nil || a.Clock == nil {
		return messagingIdentity{}, &domain.Error{Code: domain.CodeUnavailable, Message: "DM messaging runtime dependencies are unavailable"}
	}
	account, exists, err := a.State.AuthAccount(ctx)
	if err != nil {
		return messagingIdentity{}, &domain.Error{Code: domain.CodeUnavailable, Message: "read signed-in account state", Cause: err}
	}
	if !exists || account.LoginRequired {
		return messagingIdentity{}, &domain.Error{Code: domain.CodeAuthRequired, Message: "login is required"}
	}
	profileUUID, err := a.State.ProfileUUID(ctx)
	if err != nil {
		return messagingIdentity{}, &domain.Error{Code: domain.CodeUnavailable, Message: "read profile identity", Cause: err}
	}
	return messagingIdentity{profileUUID: profileUUID, account: account}, nil
}

func (a *App) dmReplyPlan(ctx context.Context, profile, requestID string, identity messagingIdentity, target, digest string, input domain.DMReplyInputV1) (domain.DMOutputV1, error) {
	_, client, err := a.dmClient(ctx, profile, requestID)
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	conversations, _, warnings, err := scanConversations(ctx, client, input.ConversationID, boundedConversationScan)
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	var conversation kleinanzeigen.ConversationSummary
	for _, candidate := range conversations {
		if candidate.ID == input.ConversationID {
			conversation = candidate
			break
		}
	}
	if conversation.ID == "" {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeNotFound, Message: "conversation does not belong to the signed-in account", Details: map[string]any{"conversation_id": input.ConversationID}}
	}
	if conversation.ListingStatus != "" && !strings.EqualFold(conversation.ListingStatus, "available") && !strings.EqualFold(conversation.ListingStatus, "active") {
		warnings = append(warnings, domain.WarningV1{Code: "listing_status_context", Message: "the conversation listing is not reported as active", Details: map[string]any{"listing_status": conversation.ListingStatus}})
	}
	return a.createMessagingPlan(ctx, requestID, identity, state.ConfirmationKindReply, target, digest, input.Message, input.AcknowledgeWarning, input.AcknowledgePossibleDuplicate, map[string]any{
		"conversation_id": conversation.ID,
		"listing_id":      conversation.ListingID,
		"counterparty":    conversation.Counterparty,
		"listing_title":   conversation.ListingTitle,
		"listing_status":  conversation.ListingStatus,
	}, []string{state.ConfirmationStageExecutingSend}, warnings)
}

func (a *App) dmStartPlan(ctx context.Context, profile, requestID string, identity messagingIdentity, listingID, target, digest string, input domain.DMStartInputV1) (domain.DMOutputV1, error) {
	_, client, err := a.dmClient(ctx, profile, requestID)
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	conversations, complete, inboxWarnings, err := scanConversations(ctx, client, "", boundedConversationScan)
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	if !complete {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeDuplicateUnresolved, Message: "could not prove that the listing has no existing conversation within the bounded inbox scan", Details: map[string]any{"listing_id": listingID}}
	}
	if existing := conversationsForListing(conversations, listingID); len(existing) != 0 {
		return domain.DMOutputV1{}, existingConversationError(listingID, existing[0].ID)
	}
	detail, err := kleinanzeigen.ListingFetch(ctx, a.Transport, listingID)
	if err != nil {
		return domain.DMOutputV1{}, err
	}
	warnings := append([]domain.WarningV1(nil), inboxWarnings...)
	warnings = append(warnings, detail.Warnings...)
	warnings = append(warnings, domain.WarningV1{
		Code:    "conversation_creation_visibility_unverified",
		Message: "creating a conversation may be visible to the seller before the first message is sent",
		Details: map[string]any{"live_proof": "BLOCKED_PENDING_TWO_ACCOUNT_LIVE_PROOF"},
	})
	return a.createMessagingPlan(ctx, requestID, identity, state.ConfirmationKindStart, target, digest, input.Message, input.AcknowledgeWarning, input.AcknowledgePossibleDuplicate, map[string]any{
		"listing_id":          listingID,
		"counterparty":        detail.Seller.Name,
		"listing_title":       detail.Listing.Title,
		"listing_status":      detail.Listing.Availability,
		"contact_name":        input.ContactName,
		"creation_visibility": "may_be_visible_to_seller",
	}, []string{state.ConfirmationStageExecutingCreate, state.ConfirmationStageConversationCreated, state.ConfirmationStageExecutingSend}, warnings)
}

func (a *App) createMessagingPlan(ctx context.Context, requestID string, identity messagingIdentity, kind, target, digest, message, warningAck, duplicateAck string, contextData map[string]any, stages []string, warnings []domain.WarningV1) (domain.DMOutputV1, error) {
	unresolved, err := a.State.FindUnresolved(ctx, identity.account.SubjectHash, kind, target, digest)
	if err == nil && unresolved == nil && kind == state.ConfirmationKindReply {
		if conversationID, ok := contextData["conversation_id"].(string); ok {
			unresolved, err = a.State.FindUnresolvedConversation(ctx, identity.account.SubjectHash, conversationID, digest)
		}
	}
	if err != nil {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "check unresolved messaging outcome", Cause: err}
	}
	if unresolved != nil {
		if duplicateAck != unresolved.ConfirmationID {
			return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeDuplicateUnresolved, Message: "a previous attempt has an unresolved outcome; inspect the conversation and acknowledge its confirmation ID before planning a possible duplicate", Details: map[string]any{"previous_confirmation_id": unresolved.ConfirmationID}}
		}
		warnings = append(warnings, domain.WarningV1{Code: "possible_duplicate_acknowledged", Message: "the caller acknowledged a previous unresolved outcome after manual inspection", Details: map[string]any{"previous_confirmation_id": unresolved.ConfirmationID}})
	} else if duplicateAck != "" {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeConfirmationMismatch, Message: "the acknowledged possible-duplicate ID does not identify an unresolved matching attempt"}
	}
	blocked, err := a.State.FindWarningBlocked(ctx, identity.account.SubjectHash, kind, target, digest)
	if err == nil && blocked == nil && kind == state.ConfirmationKindReply {
		if conversationID, ok := contextData["conversation_id"].(string); ok {
			blocked, err = a.State.FindWarningBlockedConversation(ctx, identity.account.SubjectHash, conversationID, digest)
		}
	}
	if err != nil {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "check platform warning state", Cause: err}
	}
	if blocked != nil {
		code := state.WarningCodeFromPlan(*blocked)
		if warningAck != code {
			return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeWarningBlocked, Message: "the platform warning must be acknowledged by its exact code in a new dry run", Details: map[string]any{"warning_code": code, "previous_confirmation_id": blocked.ConfirmationID, "acknowledgement_supported": false}}
		}
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeWarningBlocked, Message: "platform warning acknowledgement remains blocked until the browser-equivalent contract is proven safely", Details: map[string]any{"warning_code": code, "live_proof": "BLOCKED_PENDING_TWO_ACCOUNT_LIVE_PROOF", "acknowledgement_supported": false}}
	}
	if warningAck != "" {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeConfirmationMismatch, Message: "the warning acknowledgement does not match a previously observed warning for this target and message"}
	}
	now := a.Clock.Now().UTC()
	plan, err := a.State.CreatePlan(ctx, state.CreatePlanInput{
		ProfileUUID: identity.profileUUID, AccountHash: identity.account.SubjectHash,
		OperationKind: kind, TargetID: target, MessageDigest: digest, Now: now,
	})
	if err != nil {
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "store confirmation plan", Cause: err}
	}
	data := map[string]any{
		"operation": kind, "dry_run": true, "confirmation_id": plan.ConfirmationID,
		"profile": messagingProfilePseudonym(identity.profileUUID), "account": dmAccountPseudonym(identity.account.SubjectHash),
		"message_preview": message, "message_digest": digest, "message_utf8_bytes": len(message),
		"message_runes": utf8.RuneCountInString(message), "created_at": plan.CreatedAt,
		"expires_at": plan.ExpiresAt, "operation_stages": append([]string(nil), stages...),
		"state_machine":    confirmationStateMachine(kind),
		"network_attempts": 0, "stored_message_body": false,
	}
	for key, value := range contextData {
		data[key] = value
	}
	envelope := Envelope(a.Clock, "kcli.dm-"+kind+"/v1", requestID, "local-confirmation-plan", data)
	envelope.ObservedAt = now
	envelope.Warnings = warnings
	return domain.DMOutputV1{Envelope: envelope}, nil
}

func (a *App) claimMessagingPlan(ctx context.Context, input state.ClaimPlanInput) (state.ConfirmationPlan, error) {
	plan, err := a.State.ClaimPlan(ctx, input)
	if err == nil {
		return plan, nil
	}
	switch {
	case errors.Is(err, state.ErrConfirmationExpired):
		return state.ConfirmationPlan{}, &domain.Error{Code: domain.CodeConfirmationExpired, Message: "confirmation expired; create a new dry run"}
	case errors.Is(err, state.ErrConfirmationNotFound):
		return state.ConfirmationPlan{}, &domain.Error{Code: domain.CodeConfirmationRequired, Message: "confirmation plan was not found; create a new dry run"}
	case errors.Is(err, state.ErrConfirmationMismatch):
		return state.ConfirmationPlan{}, &domain.Error{Code: domain.CodeConfirmationMismatch, Message: "confirmation does not match the profile, account, target, operation, or exact message digest"}
	case errors.Is(err, state.ErrConfirmationAlreadyUsed):
		return state.ConfirmationPlan{}, &domain.Error{Code: domain.CodeConfirmationRequired, Message: "confirmation was already claimed and cannot be reused"}
	default:
		return state.ConfirmationPlan{}, &domain.Error{Code: domain.CodeUnavailable, Message: "claim confirmation plan", Cause: err}
	}
}

func (a *App) attemptMessageSend(ctx context.Context, profile, requestID string, client *kleinanzeigen.MessageClient, plan state.ConfirmationPlan, conversationID, message string) (domain.DMOutputV1, error) {
	attemptedAt := a.Clock.Now().UTC()
	result, err := client.SendMessage(ctx, conversationID, message)
	stateCtx := context.WithoutCancel(ctx)
	if err == nil && result.WarningCode != "" {
		if stateErr := a.State.MarkWarningBlockedForConversation(stateCtx, plan.ConfirmationID, result.WarningCode, conversationID, a.Clock.Now().UTC()); stateErr != nil {
			return domain.DMOutputV1{}, messagingAuditError("record platform warning", stateErr)
		}
		return domain.DMOutputV1{}, &domain.Error{Code: domain.CodeWarningBlocked, Message: "the platform blocked the message with a content warning; no acknowledgement contract is enabled", Details: map[string]any{"confirmation_id": plan.ConfirmationID, "warning_code": result.WarningCode, "acknowledgement_supported": false, "live_proof": "BLOCKED_PENDING_TWO_ACCOUNT_LIVE_PROOF"}}
	}
	if err == nil {
		if stateErr := a.State.MarkSent(stateCtx, plan.ConfirmationID, "sent", a.Clock.Now().UTC()); stateErr != nil {
			return domain.DMOutputV1{}, messagingAuditError("record sent message", stateErr)
		}
		return confirmedMessagingOutput(a.Clock, requestID, plan.OperationKind, plan.ConfirmationID, conversationID, "sent", false), nil
	}
	if externalFailureDefinite(err) {
		return domain.DMOutputV1{}, a.failClaimedPlan(stateCtx, plan.ConfirmationID, err)
	}
	reconcileCtx, cancel := context.WithTimeout(stateCtx, 10*time.Second)
	defer cancel()
	matched, reconcileErr := a.reconcileMessage(reconcileCtx, profile, requestID, conversationID, message, attemptedAt)
	if reconcileErr == nil && matched {
		if stateErr := a.State.MarkSent(stateCtx, plan.ConfirmationID, "sent_reconciled", a.Clock.Now().UTC()); stateErr != nil {
			return domain.DMOutputV1{}, messagingAuditError("record reconciled message", stateErr)
		}
		return confirmedMessagingOutput(a.Clock, requestID, plan.OperationKind, plan.ConfirmationID, conversationID, "sent_reconciled", true), nil
	}
	if stateErr := a.State.MarkOutcomeUnknownForConversation(stateCtx, plan.ConfirmationID, conversationID, a.Clock.Now().UTC()); stateErr != nil {
		return domain.DMOutputV1{}, messagingAuditError("record ambiguous message outcome", stateErr)
	}
	return domain.DMOutputV1{}, ambiguousMessageError(plan.ConfirmationID, conversationID, reconcileErr == nil, err)
}

func (a *App) reconcileAmbiguousCreate(ctx context.Context, profile, requestID string, client *kleinanzeigen.MessageClient, plan state.ConfirmationPlan, before []kleinanzeigen.ConversationSummary, listingID, message string, attemptedAt time.Time, createErr error) (domain.DMOutputV1, error) {
	stateCtx := context.WithoutCancel(ctx)
	if err := a.State.MarkReconcileCreate(stateCtx, plan.ConfirmationID, a.Clock.Now().UTC()); err != nil {
		return domain.DMOutputV1{}, messagingAuditError("record create reconciliation", err)
	}
	reconcileCtx, cancel := context.WithTimeout(stateCtx, 10*time.Second)
	defer cancel()
	page, err := client.ListConversations(reconcileCtx, 0, 100)
	if err != nil {
		if stateErr := a.State.MarkOutcomeUnknown(stateCtx, plan.ConfirmationID, a.Clock.Now().UTC()); stateErr != nil {
			return domain.DMOutputV1{}, messagingAuditError("record ambiguous create outcome", stateErr)
		}
		return domain.DMOutputV1{}, ambiguousCreateError(plan.ConfirmationID, listingID, false, createErr)
	}
	beforeIDs := make(map[string]struct{}, len(before))
	for _, conversation := range before {
		beforeIDs[conversation.ID] = struct{}{}
	}
	candidates := make([]kleinanzeigen.ConversationSummary, 0, 1)
	for _, conversation := range page.Conversations {
		if conversation.ListingID != listingID {
			continue
		}
		if _, existed := beforeIDs[conversation.ID]; !existed {
			candidates = append(candidates, conversation)
		}
	}
	if len(candidates) != 1 || len(page.Conversations) == 100 {
		if stateErr := a.State.MarkOutcomeUnknown(stateCtx, plan.ConfirmationID, a.Clock.Now().UTC()); stateErr != nil {
			return domain.DMOutputV1{}, messagingAuditError("record ambiguous create outcome", stateErr)
		}
		return domain.DMOutputV1{}, ambiguousCreateError(plan.ConfirmationID, listingID, true, createErr)
	}
	conversationID := candidates[0].ID
	if err := a.State.MarkConversationCreated(stateCtx, plan.ConfirmationID, conversationID, a.Clock.Now().UTC()); err != nil {
		return domain.DMOutputV1{}, messagingAuditError("record reconciled conversation", err)
	}
	matchCount, err := a.reconcileMessageCount(reconcileCtx, profile, requestID, conversationID, message, attemptedAt)
	if err != nil || matchCount > 1 {
		if stateErr := a.State.MarkOutcomeUnknownForConversation(stateCtx, plan.ConfirmationID, conversationID, a.Clock.Now().UTC()); stateErr != nil {
			return domain.DMOutputV1{}, messagingAuditError("record ambiguous created conversation", stateErr)
		}
		return domain.DMOutputV1{}, ambiguousCreateError(plan.ConfirmationID, listingID, true, createErr)
	}
	if err := a.State.MarkExecutingSend(stateCtx, plan.ConfirmationID, a.Clock.Now().UTC()); err != nil {
		return domain.DMOutputV1{}, messagingAuditError("record reconciled first-message stage", err)
	}
	plan.Stage = state.ConfirmationStageExecutingSend
	if matchCount == 1 {
		if err := a.State.MarkSent(stateCtx, plan.ConfirmationID, "sent_reconciled", a.Clock.Now().UTC()); err != nil {
			return domain.DMOutputV1{}, messagingAuditError("record reconciled first message", err)
		}
		return confirmedMessagingOutput(a.Clock, requestID, plan.OperationKind, plan.ConfirmationID, conversationID, "sent_reconciled", true), nil
	}
	return a.attemptMessageSend(reconcileCtx, profile, requestID, client, plan, conversationID, message)
}

func (a *App) reconcileMessage(ctx context.Context, profile, requestID, conversationID, message string, attemptedAt time.Time) (bool, error) {
	count, err := a.reconcileMessageCount(ctx, profile, requestID, conversationID, message, attemptedAt)
	return count == 1, err
}

func (a *App) reconcileMessageCount(ctx context.Context, profile, requestID, conversationID, message string, attemptedAt time.Time) (int, error) {
	opened, err := a.DMGet(ctx, profile, requestID, domain.DMGetInputV1{ConversationID: conversationID})
	if err != nil {
		return 0, err
	}
	messages, ok := opened.Data["messages"].([]DMMessageV1)
	if !ok {
		return 0, &domain.Error{Code: domain.CodeUpstreamContract, Message: "conversation reconciliation did not return typed messages"}
	}
	wanted := normalizeMessageForReconciliation(message)
	windowStart := attemptedAt.Add(-messageReconcileWindow)
	windowEnd := attemptedAt.Add(messageReconcileWindow)
	matches := 0
	for _, candidate := range messages {
		if candidate.Direction != "OUT" || candidate.ReceivedAt.IsZero() || candidate.ReceivedAt.Before(windowStart) || candidate.ReceivedAt.After(windowEnd) {
			continue
		}
		if normalizeMessageForReconciliation(candidate.Text) == wanted {
			matches++
		}
	}
	return matches, nil
}

func scanConversations(ctx context.Context, client *kleinanzeigen.MessageClient, wantedID string, pageLimit int) ([]kleinanzeigen.ConversationSummary, bool, []domain.WarningV1, error) {
	items := make([]kleinanzeigen.ConversationSummary, 0, pageLimit*100)
	warnings := []domain.WarningV1{}
	for pageNumber := range pageLimit {
		page, err := client.ListConversations(ctx, pageNumber, 100)
		if err != nil {
			return nil, false, nil, err
		}
		items = append(items, page.Conversations...)
		warnings = append(warnings, page.Warnings...)
		for _, conversation := range page.Conversations {
			if conversation.ID == wantedID {
				return items, true, warnings, nil
			}
		}
		if len(page.Conversations) < 100 {
			return items, true, warnings, nil
		}
	}
	return items, false, warnings, nil
}

func conversationsForListing(conversations []kleinanzeigen.ConversationSummary, listingID string) []kleinanzeigen.ConversationSummary {
	matches := make([]kleinanzeigen.ConversationSummary, 0, 1)
	for _, conversation := range conversations {
		if conversation.ListingID == listingID {
			matches = append(matches, conversation)
		}
	}
	return matches
}

func validateMessagingPhase(dryRun bool, confirmationID, warningAck, duplicateAck string) error {
	if dryRun == (confirmationID != "") {
		return &domain.Error{Code: domain.CodeConfirmationRequired, Message: "provide exactly one of dry_run or confirm"}
	}
	if confirmationID != "" && (warningAck != "" || duplicateAck != "") {
		return &domain.Error{Code: domain.CodeConfirmationMismatch, Message: "warning and possible-duplicate acknowledgements are valid only while creating a new dry-run plan"}
	}
	return nil
}

func validateAppMessageInput(message, messageFile, input string) error {
	if messageFile != "" || input != "" {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "application messaging accepts resolved message text only"}
	}
	if message == "" || len(message) > messageMaximumBytes || !utf8.ValidString(message) || strings.IndexByte(message, 0) >= 0 {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "message must be non-empty valid UTF-8 no larger than 65536 bytes"}
	}
	return nil
}

func validMessagingContactName(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool { return r == 0 || r < 0x20 || r == 0x7f }) < 0
}

func replyConfirmationTarget(conversationID, profile string) string {
	return "reply:v1:" + conversationID + ":" + state.DigestMessageContent(profile)
}

func startConfirmationTarget(listingID, contactName, profile string) string {
	return "start:v1:" + listingID + ":" + state.DigestMessageContent(contactName) + ":" + state.DigestMessageContent(profile)
}

func confirmationStateMachine(kind string) []string {
	if kind == state.ConfirmationKindReply {
		return []string{
			"planned -> executing_send",
			"executing_send -> sent | failed_definite | warning_blocked | outcome_unknown",
			"planned -> expired",
		}
	}
	return []string{
		"planned -> executing_create",
		"executing_create -> conversation_created | reconcile_create | failed_definite",
		"reconcile_create -> conversation_created | outcome_unknown",
		"conversation_created -> executing_send",
		"executing_send -> sent | failed_definite | warning_blocked | outcome_unknown",
		"planned -> expired",
	}
}

func messagingProfilePseudonym(profileUUID string) string {
	if len(profileUUID) > 12 {
		profileUUID = profileUUID[:12]
	}
	return "profile_" + profileUUID
}

func normalizeMessageForReconciliation(message string) string {
	return strings.Join(strings.Fields(norm.NFC.String(message)), " ")
}

func externalFailureDefinite(err error) bool {
	var typed *domain.Error
	if !errors.As(err, &typed) {
		return false
	}
	if preWrite, _ := typed.Details["pre_write"].(bool); preWrite {
		return true
	}
	if mayHaveSucceeded, _ := typed.Details["create_may_have_succeeded"].(bool); mayHaveSucceeded {
		return false
	}
	switch typed.Code {
	case domain.CodeInvalidCommand, domain.CodeInvalidIdentifier, domain.CodeInvalidInput,
		domain.CodeAuthRequired, domain.CodeAuthExpired, domain.CodeAuthRevoked,
		domain.CodeNotFound, domain.CodeRateLimited, domain.CodeRateLimitedLocal,
		domain.CodeConfirmationRequired, domain.CodeConfirmationExpired,
		domain.CodeConfirmationMismatch, domain.CodeWarningBlocked,
		domain.CodeDuplicateUnresolved, domain.CodeUpstreamContract:
		return true
	default:
		return false
	}
}

func (a *App) failClaimedPlan(ctx context.Context, confirmationID string, original error) error {
	if err := a.State.MarkFailedDefinite(context.WithoutCancel(ctx), confirmationID, a.Clock.Now().UTC()); err != nil {
		return messagingAuditError("record definite messaging failure", err)
	}
	return original
}

func existingConversationError(listingID, conversationID string) error {
	return &domain.Error{Code: domain.CodeDuplicateUnresolved, Message: "an existing conversation already targets this listing; create a dm reply plan instead", Details: map[string]any{"listing_id": listingID, "conversation_id": conversationID, "suggested_command": "dm reply"}}
}

func ambiguousMessageError(confirmationID, conversationID string, reconciled bool, cause error) error {
	return &domain.Error{Code: domain.CodeAmbiguousExternalState, Message: "message outcome is unknown; the send was not repeated and requires human conversation inspection", Details: map[string]any{"confirmation_id": confirmationID, "conversation_id": conversationID, "reconciliation_completed": reconciled, "send_repeated": false}, Cause: cause}
}

func ambiguousCreateError(confirmationID, listingID string, reconciled bool, cause error) error {
	return &domain.Error{Code: domain.CodeAmbiguousExternalState, Message: "conversation creation outcome is unknown; creation was not repeated and requires human inbox inspection", Details: map[string]any{"confirmation_id": confirmationID, "listing_id": listingID, "reconciliation_completed": reconciled, "create_repeated": false}, Cause: cause}
}

func messagingAuditError(action string, cause error) error {
	return &domain.Error{Code: domain.CodeUnavailable, Message: action + "; external state may require human inspection", Cause: cause}
}

func confirmedMessagingOutput(clock Clock, requestID, kind, confirmationID, conversationID, outcome string, reconciled bool) domain.DMOutputV1 {
	data := map[string]any{
		"operation": kind, "confirmation_id": confirmationID, "conversation_id": conversationID,
		"outcome": outcome, "reconciled": reconciled, "network_attempts": 1,
		"message_body_stored": false,
	}
	return domain.DMOutputV1{Envelope: Envelope(clock, "kcli.dm-"+kind+"/v1", requestID, "message-gateway", data)}
}
