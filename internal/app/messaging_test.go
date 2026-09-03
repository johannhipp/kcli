package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/state"
)

func TestDMReplyDryRunDigestOnlyTamperExpiryAndAtomicSingleClaim(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 4, 0, 0, time.UTC)
	listFixture := dmAppFixture(t, "conversations.redacted.json")
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/conversations"):
			_, _ = writer.Write(listFixture)
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/conversations/conv-new"):
			sends.Add(1)
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	application, secrets, database := authTestApp(t, server, now)
	authSeedSession(t, secrets, database, "access-token", "refresh-token", now.Add(time.Hour))
	body := "[REDACTED_REPLY_SENTINEL]"
	planned, err := application.DMReply(context.Background(), "default", "plan", domain.DMReplyInputV1{ConversationID: "conv-new", Message: body, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	confirmationID, _ := planned.Data["confirmation_id"].(string)
	if confirmationID == "" || planned.Data["message_digest"] != state.DigestMessageContent(body) || planned.Data["message_preview"] != body || planned.Data["message_utf8_bytes"] != len(body) || planned.Data["stored_message_body"] != false {
		t.Fatalf("planned=%#v", planned.Data)
	}
	var persisted string
	if err := database.SQL().QueryRowContext(context.Background(), `SELECT group_concat(profile_uuid || account_hash || operation_kind || target_id || message_digest || state || stage || ifnull(outcome, '')) FROM confirmation_plans`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(persisted, body) || !strings.Contains(persisted, state.DigestMessageContent(body)) {
		t.Fatalf("persisted=%q", persisted)
	}
	_, err = application.DMReply(context.Background(), "default", "tampered", domain.DMReplyInputV1{ConversationID: "conv-new", Message: body + " changed", Confirm: confirmationID})
	if domain.ExitCode(err) != 7 || sends.Load() != 0 {
		t.Fatalf("tamper err=%#v exit=%d sends=%d", err, domain.ExitCode(err), sends.Load())
	}
	_, err = application.DMReply(context.Background(), "default", "wrong-target", domain.DMReplyInputV1{ConversationID: "conv-old", Message: body, Confirm: confirmationID})
	if domain.ExitCode(err) != 7 || sends.Load() != 0 {
		t.Fatalf("target err=%#v exit=%d sends=%d", err, domain.ExitCode(err), sends.Load())
	}
	_, err = application.DMReply(context.Background(), "other-profile", "wrong-profile", domain.DMReplyInputV1{ConversationID: "conv-new", Message: body, Confirm: confirmationID})
	if domain.ExitCode(err) != 7 || sends.Load() != 0 {
		t.Fatalf("profile err=%#v exit=%d sends=%d", err, domain.ExitCode(err), sends.Load())
	}
	if _, err := database.SQL().ExecContext(context.Background(), `UPDATE meta SET value = ? WHERE key = 'auth_account_hash'`, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	_, err = application.DMReply(context.Background(), "default", "wrong-account", domain.DMReplyInputV1{ConversationID: "conv-new", Message: body, Confirm: confirmationID})
	if domain.ExitCode(err) != 7 || sends.Load() != 0 {
		t.Fatalf("account err=%#v exit=%d sends=%d", err, domain.ExitCode(err), sends.Load())
	}
	if _, err := database.SQL().ExecContext(context.Background(), `UPDATE meta SET value = ? WHERE key = 'auth_account_hash'`, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	confirmed, err := application.DMReply(context.Background(), "default", "confirm", domain.DMReplyInputV1{ConversationID: "conv-new", Message: body, Confirm: confirmationID})
	if err != nil || confirmed.Data["outcome"] != "sent" || sends.Load() != 1 {
		t.Fatalf("confirmed=%#v err=%v sends=%d", confirmed.Data, err, sends.Load())
	}

	atomicPlan, err := application.DMReply(context.Background(), "default", "atomic-plan", domain.DMReplyInputV1{ConversationID: "conv-new", Message: body + " atomic", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	atomicID := atomicPlan.Data["confirmation_id"].(string)
	var successes atomic.Int32
	var exitSeven atomic.Int32
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, callErr := application.DMReply(context.Background(), "default", "atomic-confirm", domain.DMReplyInputV1{ConversationID: "conv-new", Message: body + " atomic", Confirm: atomicID})
			if callErr == nil {
				successes.Add(1)
			} else if domain.ExitCode(callErr) == 7 {
				exitSeven.Add(1)
			} else {
				t.Errorf("concurrent confirmation error=%#v", callErr)
			}
		}()
	}
	wait.Wait()
	if successes.Load() != 1 || exitSeven.Load() != 1 || sends.Load() != 2 {
		t.Fatalf("successes=%d exit7=%d sends=%d", successes.Load(), exitSeven.Load(), sends.Load())
	}

	expiring, err := application.DMReply(context.Background(), "default", "expiring-plan", domain.DMReplyInputV1{ConversationID: "conv-new", Message: body + " expiring", DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	application.Clock.(*authTestClock).now = now.Add(11 * time.Minute)
	_, err = application.DMReply(context.Background(), "default", "expired", domain.DMReplyInputV1{ConversationID: "conv-new", Message: body + " expiring", Confirm: expiring.Data["confirmation_id"].(string)})
	if domain.ExitCode(err) != 7 || sends.Load() != 2 {
		t.Fatalf("expiry err=%#v exit=%d sends=%d", err, domain.ExitCode(err), sends.Load())
	}
}

func TestDMReplyAmbiguousReconciliationAndWarningBlock(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 4, 0, 0, time.UTC)
	listFixture := dmAppFixture(t, "conversations.redacted.json")
	openFixture := dmAppFixture(t, "conversation-messages.redacted.json")
	warningFixture := dmAppFixture(t, "message-send-warning.redacted.json")

	reconciledTransport := &messagingScriptTransport{listBody: listFixture, openBody: openFixture, sendErr: errors.New("response lost after write")}
	application, database := messagingTestApp(t, reconciledTransport, now)
	message := "  [REDACTED_MESSAGE]\n"
	plan, err := application.DMReply(context.Background(), "default", "reconcile-plan", domain.DMReplyInputV1{ConversationID: "conv-new", Message: message, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	result, err := application.DMReply(context.Background(), "default", "reconcile-confirm", domain.DMReplyInputV1{ConversationID: "conv-new", Message: message, Confirm: plan.Data["confirmation_id"].(string)})
	if err != nil || result.Data["outcome"] != "sent_reconciled" || reconciledTransport.sends.Load() != 1 || reconciledTransport.opens.Load() != 1 {
		t.Fatalf("result=%#v err=%v sends=%d opens=%d", result.Data, err, reconciledTransport.sends.Load(), reconciledTransport.opens.Load())
	}

	unknownMessage := "[REDACTED_DIFFERENT_MESSAGE]"
	unknownPlan, err := application.DMReply(context.Background(), "default", "unknown-plan", domain.DMReplyInputV1{ConversationID: "conv-new", Message: unknownMessage, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	unknownID := unknownPlan.Data["confirmation_id"].(string)
	_, err = application.DMReply(context.Background(), "default", "unknown-confirm", domain.DMReplyInputV1{ConversationID: "conv-new", Message: unknownMessage, Confirm: unknownID})
	if domain.ExitCode(err) != 8 {
		t.Fatalf("unknown err=%#v exit=%d", err, domain.ExitCode(err))
	}
	stored, err := database.ConfirmationPlan(context.Background(), unknownID)
	if err != nil || stored.State != state.ConfirmationStateOutcomeUnknown {
		t.Fatalf("stored=%#v err=%v", stored, err)
	}
	_, err = application.DMReply(context.Background(), "default", "duplicate-plan", domain.DMReplyInputV1{ConversationID: "conv-new", Message: unknownMessage, DryRun: true})
	if domain.ExitCode(err) != 7 {
		t.Fatalf("unresolved duplicate err=%#v", err)
	}
	acknowledged, err := application.DMReply(context.Background(), "default", "duplicate-ack", domain.DMReplyInputV1{ConversationID: "conv-new", Message: unknownMessage, DryRun: true, AcknowledgePossibleDuplicate: unknownID})
	if err != nil || acknowledged.Data["confirmation_id"] == "" {
		t.Fatalf("acknowledged=%#v err=%v", acknowledged.Data, err)
	}

	warningTransport := &messagingScriptTransport{listBody: listFixture, openBody: openFixture, sendBody: warningFixture}
	warningApp, warningDB := messagingTestApp(t, warningTransport, now)
	warningPlan, err := warningApp.DMReply(context.Background(), "default", "warning-plan", domain.DMReplyInputV1{ConversationID: "conv-new", Message: unknownMessage, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	warningID := warningPlan.Data["confirmation_id"].(string)
	_, err = warningApp.DMReply(context.Background(), "default", "warning-confirm", domain.DMReplyInputV1{ConversationID: "conv-new", Message: unknownMessage, Confirm: warningID})
	if domain.ExitCode(err) != 7 {
		t.Fatalf("warning err=%#v", err)
	}
	warningStored, err := warningDB.ConfirmationPlan(context.Background(), warningID)
	if err != nil || warningStored.State != state.ConfirmationStateWarningBlocked || state.WarningCodeFromPlan(warningStored) != "warnEmail" {
		t.Fatalf("warning stored=%#v err=%v", warningStored, err)
	}
	_, err = warningApp.DMReply(context.Background(), "default", "warning-ack", domain.DMReplyInputV1{ConversationID: "conv-new", Message: unknownMessage, DryRun: true, AcknowledgeWarning: "warnEmail"})
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeWarningBlocked || typed.Details["acknowledgement_supported"] != false || warningTransport.sends.Load() != 1 {
		t.Fatalf("warning acknowledgement err=%#v sends=%d", err, warningTransport.sends.Load())
	}
}

func TestDMReplyDefiniteFailuresAndNo401Retry(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 4, 0, 0, time.UTC)
	listFixture := dmAppFixture(t, "conversations.redacted.json")
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			_, _ = writer.Write(listFixture)
			return
		}
		posts.Add(1)
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	application, secrets, database := authTestApp(t, server, now)
	authSeedSession(t, secrets, database, "access-token", "refresh-token", now.Add(time.Hour))
	message := "[REDACTED_AUTH_FAILURE]"
	plan, err := application.DMReply(context.Background(), "default", "no-retry-plan", domain.DMReplyInputV1{ConversationID: "conv-new", Message: message, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	_, err = application.DMReply(context.Background(), "default", "no-retry-confirm", domain.DMReplyInputV1{ConversationID: "conv-new", Message: message, Confirm: plan.Data["confirmation_id"].(string)})
	if posts.Load() != 1 {
		t.Fatalf("401 external send attempts=%d err=%#v", posts.Load(), err)
	}

	preWrite := &messagingScriptTransport{listBody: listFixture, sendErr: kleinanzeigen.ConnectError(&net.DNSError{Err: "refused", Name: "example.invalid", IsTemporary: true})}
	preWriteApp, preWriteDB := messagingTestApp(t, preWrite, now)
	preWritePlan, err := preWriteApp.DMReply(context.Background(), "default", "prewrite-plan", domain.DMReplyInputV1{ConversationID: "conv-new", Message: message, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	preWriteID := preWritePlan.Data["confirmation_id"].(string)
	_, err = preWriteApp.DMReply(context.Background(), "default", "prewrite-confirm", domain.DMReplyInputV1{ConversationID: "conv-new", Message: message, Confirm: preWriteID})
	stored, stateErr := preWriteDB.ConfirmationPlan(context.Background(), preWriteID)
	if err == nil || stateErr != nil || stored.State != state.ConfirmationStateFailedDefinite || preWrite.opens.Load() != 0 || preWrite.sends.Load() != 1 {
		t.Fatalf("pre-write err=%#v stored=%#v stateErr=%v sends=%d opens=%d", err, stored, stateErr, preWrite.sends.Load(), preWrite.opens.Load())
	}

	afterWrite := &messagingScriptTransport{
		listBody: listFixture, openBody: dmAppFixture(t, "conversation-messages.redacted.json"),
		sendErr: context.DeadlineExceeded,
	}
	afterWriteApp, afterWriteDB := messagingTestApp(t, afterWrite, now)
	afterWritePlan, err := afterWriteApp.DMReply(context.Background(), "default", "afterwrite-plan", domain.DMReplyInputV1{ConversationID: "conv-new", Message: message, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	afterWriteID := afterWritePlan.Data["confirmation_id"].(string)
	confirmContext, cancel := context.WithCancel(context.Background())
	afterWrite.sendCancel = cancel
	_, err = afterWriteApp.DMReply(confirmContext, "default", "afterwrite-confirm", domain.DMReplyInputV1{ConversationID: "conv-new", Message: message, Confirm: afterWriteID})
	afterWriteStored, stateErr := afterWriteDB.ConfirmationPlan(context.Background(), afterWriteID)
	if domain.ExitCode(err) != 8 || stateErr != nil || afterWriteStored.State != state.ConfirmationStateOutcomeUnknown || afterWrite.sends.Load() != 1 || afterWrite.opens.Load() != 1 {
		t.Fatalf("after-write err=%#v stored=%#v stateErr=%v sends=%d opens=%d", err, afterWriteStored, stateErr, afterWrite.sends.Load(), afterWrite.opens.Load())
	}
}

func TestDMStartRejectsExistingConversationAndRecordsCreateSuccessSendFailure(t *testing.T) {
	now := time.Date(2026, 9, 3, 12, 4, 0, 0, time.UTC)
	listingFixture := dmAppFixture(t, "listing.json")
	createdFixture := dmAppFixture(t, "conversation-created.redacted.json")
	existingServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"data":[{"id":"conv-existing","adId":"1234567890","adTitle":"[REDACTED_LISTING]","role":"BUYER","sellerName":"[REDACTED_SELLER]"}]}`))
	}))
	defer existingServer.Close()
	existingApp, existingSecrets, existingDB := authTestApp(t, existingServer, now)
	authSeedSession(t, existingSecrets, existingDB, "access-token", "refresh-token", now.Add(time.Hour))
	_, err := existingApp.DMStart(context.Background(), "default", "existing", domain.DMStartInputV1{ListingIDOrURL: "1234567890", ContactName: "[REDACTED_CONTACT]", Message: "[REDACTED_FIRST]", DryRun: true})
	if domain.ExitCode(err) != 7 {
		t.Fatalf("existing conversation err=%#v exit=%d", err, domain.ExitCode(err))
	}

	var creates, sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/conversations"):
			_, _ = writer.Write([]byte(`{"data":[]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/api/ads/1234567890.json":
			_, _ = writer.Write(listingFixture)
		case request.Method == http.MethodPost && strings.Contains(request.URL.Path, "/create-conversation/"):
			creates.Add(1)
			_, _ = writer.Write(createdFixture)
		case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/conversations/conv-created"):
			sends.Add(1)
			writer.WriteHeader(http.StatusBadRequest)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	application, secrets, database := authTestApp(t, server, now)
	authSeedSession(t, secrets, database, "access-token", "refresh-token", now.Add(time.Hour))
	input := domain.DMStartInputV1{ListingIDOrURL: "1234567890", ContactName: "[REDACTED_CONTACT]", Message: "[REDACTED_FIRST]", DryRun: true}
	planned, err := application.DMStart(context.Background(), "default", "start-plan", input)
	if err != nil {
		t.Fatal(err)
	}
	if planned.Data["creation_visibility"] != "may_be_visible_to_seller" {
		t.Fatalf("planned=%#v", planned.Data)
	}
	confirmationID := planned.Data["confirmation_id"].(string)
	input.DryRun = false
	input.Confirm = confirmationID
	_, err = application.DMStart(context.Background(), "default", "start-confirm", input)
	stored, stateErr := database.ConfirmationPlan(context.Background(), confirmationID)
	if err == nil || creates.Load() != 1 || sends.Load() != 1 || stateErr != nil || stored.State != state.ConfirmationStateFailedDefinite {
		t.Fatalf("err=%#v creates=%d sends=%d stored=%#v stateErr=%v", err, creates.Load(), sends.Load(), stored, stateErr)
	}
}

type messagingScriptTransport struct {
	listBody   []byte
	openBody   []byte
	sendBody   []byte
	sendErr    error
	sendCancel context.CancelFunc
	sends      atomic.Int32
	opens      atomic.Int32
}

func (t *messagingScriptTransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	switch {
	case request.Class == kleinanzeigen.ExternalSend:
		t.sends.Add(1)
		if t.sendCancel != nil {
			t.sendCancel()
		}
		if t.sendErr != nil {
			return kleinanzeigen.Response{}, t.sendErr
		}
		return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: append([]byte(nil), t.sendBody...)}, nil
	case request.Method == http.MethodPut:
		t.opens.Add(1)
		return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: append([]byte(nil), t.openBody...)}, nil
	case request.Method == http.MethodGet:
		return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: append([]byte(nil), t.listBody...)}, nil
	default:
		return kleinanzeigen.Response{StatusCode: http.StatusNotFound}, nil
	}
}

func messagingTestApp(t *testing.T, transport kleinanzeigen.Transport, now time.Time) (*App, *state.DB) {
	t.Helper()
	database, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	secrets := &authTestSecrets{values: map[string]string{}}
	authSeedSession(t, secrets, database, "access-token", "refresh-token", now.Add(time.Hour))
	return New(Dependencies{Transport: transport, State: database, Secrets: secrets, Clock: &authTestClock{now: now}}), database
}
