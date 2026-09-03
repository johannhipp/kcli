package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
)

func TestDMListGetPreservesContextOrdersHistoryAndStoresNoBodies(t *testing.T) {
	now := time.Date(2026, 9, 3, 13, 0, 0, 0, time.UTC)
	listFixture := dmAppFixture(t, "conversations.redacted.json")
	unavailableFixture := dmAppFixture(t, "conversations-unavailable.redacted.json")
	messageFixture := dmAppFixture(t, "conversation-messages.redacted.json")
	var unavailable atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer access-token" {
			t.Fatalf("authorization header missing")
		}
		switch {
		case request.Method == http.MethodGet && strings.HasSuffix(request.URL.Path, "/conversations"):
			if unavailable.Load() {
				_, _ = writer.Write(unavailableFixture)
			} else {
				_, _ = writer.Write(listFixture)
			}
		case request.Method == http.MethodPut && strings.HasSuffix(request.URL.Path, "/conversations/conv-new"):
			_, _ = writer.Write(messageFixture)
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()
	application, secrets, database := authTestApp(t, server, now)
	authSeedSession(t, secrets, database, "access-token", "refresh-token", now.Add(time.Hour))

	listed, err := application.DMList(context.Background(), "default", "request-list", domain.DMListInputV1{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Data) != 2 || listed.Data[0].ID != "conv-new" || listed.Data[0].Role != "BUYER" || listed.Data[0].Counterparty != "[REDACTED_COUNTERPARTY_A]" || !listed.Data[0].Unread || listed.Data[0].UnreadMessageCount != 2 {
		t.Fatalf("listed=%#v", listed.Data)
	}
	unavailable.Store(true)
	updated, err := application.DMList(context.Background(), "default", "request-unavailable", domain.DMListInputV1{})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Data) != 1 || updated.Data[0].ListingTitle != "[REDACTED_LISTING_TITLE]" || updated.Data[0].Counterparty != "[REDACTED_COUNTERPARTY_A]" || updated.Data[0].ListingStatus != "UNAVAILABLE" {
		t.Fatalf("preserved unavailable context=%#v", updated.Data)
	}

	got, err := application.DMGet(context.Background(), "default", "request-get", domain.DMGetInputV1{ConversationID: "conv-new"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Data["account_state_touching"] != true || got.Data["side_effect"] != "account-state" {
		t.Fatalf("side effect labels=%#v", got.Data)
	}
	messages, ok := got.Data["messages"].([]DMMessageV1)
	if !ok || len(messages) != 3 {
		t.Fatalf("messages=%#v", got.Data["messages"])
	}
	if messages[0].Kind != "ATTACHMENT" || messages[1].Kind != "SYSTEM" || messages[2].ID != "message-new" {
		t.Fatalf("message order=%#v", messages)
	}
	if messages[0].ID != "" || len(messages[0].Raw) == 0 || messages[1].Direction != "SIDEWAYS" {
		t.Fatalf("message normalization=%#v", messages)
	}
	var persisted string
	if err := database.SQL().QueryRowContext(context.Background(), `SELECT group_concat(summary_json || ':' || remote_fingerprint || ':' || ifnull(content_digest, ''), '|') FROM conversations LEFT JOIN messages USING(account_hash, conversation_id)`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(persisted, "[REDACTED_MESSAGE]") {
		t.Fatalf("message body persisted: %q", persisted)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "access-token") || strings.Contains(string(encoded), "person@example.invalid") {
		t.Fatalf("secret in output: %s", encoded)
	}
}

func TestDMMarkReadDryRunAndAmbiguousOutcomeDoesNotRepeat(t *testing.T) {
	now := time.Date(2026, 9, 3, 13, 0, 0, 0, time.UTC)
	listFixture := dmAppFixture(t, "conversations.redacted.json")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write(listFixture)
	}))
	defer server.Close()
	application, secrets, database := authTestApp(t, server, now)
	authSeedSession(t, secrets, database, "access-token", "refresh-token", now.Add(time.Hour))
	if _, err := application.DMList(context.Background(), "default", "seed", domain.DMListInputV1{}); err != nil {
		t.Fatal(err)
	}

	base := application.Transport.(*authTestHTTPTransport)
	ambiguous := &dmAmbiguousTransport{base: base}
	application.Transport = ambiguous
	dry, err := application.DMMarkRead(context.Background(), "default", "dry", domain.DMMarkReadInputV1{ConversationIDs: []string{"conv-new"}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Data["network_call"] != false || dry.Data["would_mark_read"] != true || ambiguous.posts.Load() != 0 {
		t.Fatalf("dry=%#v posts=%d", dry.Data, ambiguous.posts.Load())
	}
	application.Transport = base
	confirmed, err := application.DMMarkRead(context.Background(), "default", "confirmed", domain.DMMarkReadInputV1{ConversationIDs: []string{"conv-new"}})
	if err != nil || confirmed.Data["outcome"] != "confirmed" {
		t.Fatalf("confirmed=%#v err=%v", confirmed.Data, err)
	}
	summary, err := database.ConversationSummary(context.Background(), strings.Repeat("d", 64), "conv-new")
	if err != nil || summary.Unread || summary.UnreadMessageCount != 0 {
		t.Fatalf("confirmed summary=%#v err=%v", summary, err)
	}
	if _, err := application.DMList(context.Background(), "default", "restore-unread", domain.DMListInputV1{}); err != nil {
		t.Fatal(err)
	}
	application.Transport = ambiguous
	_, err = application.DMMarkRead(context.Background(), "default", "ambiguous", domain.DMMarkReadInputV1{ConversationIDs: []string{"conv-new"}})
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeAmbiguousExternalState {
		t.Fatalf("error=%#v", err)
	}
	if ambiguous.posts.Load() != 1 {
		t.Fatalf("mark-read attempts=%d", ambiguous.posts.Load())
	}
	summary, err = database.ConversationSummary(context.Background(), strings.Repeat("d", 64), "conv-new")
	if err != nil || !summary.Unread || summary.UnreadMessageCount != 2 {
		t.Fatalf("summary=%#v err=%v", summary, err)
	}
}

type dmAmbiguousTransport struct {
	base  *authTestHTTPTransport
	posts atomic.Int32
}

func (t *dmAmbiguousTransport) AuthConfiguration() kleinanzeigen.AuthConfiguration {
	return t.base.AuthConfiguration()
}

func (t *dmAmbiguousTransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	if request.Method == http.MethodPost && strings.HasSuffix(request.Path, "/conversations/read") {
		t.posts.Add(1)
		return kleinanzeigen.Response{}, errors.New("response lost after request write")
	}
	return t.base.Do(request)
}

func dmAppFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}
