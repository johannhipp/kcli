package kleinanzeigen

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestConversationWrapperVariantsAndRoles(t *testing.T) {
	page := parseDMFixture(t, "conversations.redacted.json", ParseConversations)
	if len(page.Conversations) != 2 || len(page.Warnings) != 1 {
		t.Fatalf("conversations=%d warnings=%#v", len(page.Conversations), page.Warnings)
	}
	buyer, seller := page.Conversations[0], page.Conversations[1]
	if buyer.Counterparty != "[REDACTED_COUNTERPARTY_A]" || buyer.Role != "BUYER" || !buyer.Unread || buyer.UnreadMessageCount != 2 {
		t.Fatalf("buyer=%#v", buyer)
	}
	if seller.Counterparty != "[REDACTED_COUNTERPARTY_B]" || seller.Role != "SELLER" || seller.Unread {
		t.Fatalf("seller=%#v", seller)
	}
	singleton := parseDMFixture(t, "conversations-singleton.redacted.json", ParseConversations)
	if len(singleton.Conversations) != 1 || singleton.Conversations[0].ID != "conv-single" || !singleton.Conversations[0].Unread {
		t.Fatalf("singleton=%#v", singleton)
	}
	for _, raw := range [][]byte{
		[]byte(`{"data":[{"id":"array"}]}`),
		[]byte(`{"data":{"conversations":{"id":"nested"}}}`),
		[]byte(`{"conversations":{"id":"top"}}`),
	} {
		variant, err := ParseConversations(raw)
		if err != nil || len(variant.Conversations) != 1 {
			t.Fatalf("variant=%s page=%#v err=%v", raw, variant, err)
		}
	}
}

func TestMessageWrappersKindsAndDirections(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", "conversation-messages.redacted.json"))
	if err != nil {
		t.Fatal(err)
	}
	opened, err := ParseConversation(body)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Conversation.ID != "conv-new" || len(opened.Messages) != 3 {
		t.Fatalf("opened=%#v", opened)
	}
	if opened.Messages[0].Direction != "OUT" || opened.Messages[1].Direction != "IN" || opened.Messages[1].ID != "" {
		t.Fatalf("messages=%#v", opened.Messages)
	}
	if len(opened.Messages[0].Raw) != 0 || len(opened.Messages[1].Raw) == 0 || opened.Messages[2].Direction != "SIDEWAYS" {
		t.Fatalf("raw/unknown normalization=%#v", opened.Messages)
	}
	top, err := ParseConversation([]byte(`{"id":"one","messages":{"id":"m","boundness":"IN","text":"[REDACTED]"}}`))
	if err != nil || len(top.Messages) != 1 || top.Messages[0].Direction != "IN" {
		t.Fatalf("top=%#v err=%v", top, err)
	}
}

func TestConversationGatewayRequests(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests++
		switch requests {
		case 1:
			if request.Method != http.MethodGet || request.URL.Path != "/messagebox/api/users/987/conversations" || request.URL.Query().Get("page") != "2" || request.URL.Query().Get("size") != "50" {
				t.Fatalf("list request=%s %s", request.Method, request.URL.String())
			}
			_, _ = writer.Write([]byte(`{"data":[]}`))
		case 2:
			if request.Method != http.MethodPut || request.URL.Path != "/messagebox/api/users/987/conversations/conv-one" || request.URL.Query().Get("contentWarnings") != "true" {
				t.Fatalf("open request=%s %s", request.Method, request.URL.String())
			}
			_, _ = writer.Write([]byte(`{"messages":[]}`))
		case 3:
			if request.Method != http.MethodPost || request.URL.Path != "/messagebox/api/users/987/conversations/read" || request.URL.Query().Get("ids") != "conv-one,conv-two" {
				t.Fatalf("mark request=%s %s", request.Method, request.URL.String())
			}
			writer.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	transport := newHTTPTransport(server.Client(), map[Host]*url.URL{HostGateway: base})
	client, err := NewMessageClient("987", func(ctx context.Context, request Request) (Response, error) {
		request.Context = ctx
		return transport.Do(request)
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListConversations(context.Background(), 2, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := client.OpenConversation(context.Background(), "conv-one"); err != nil {
		t.Fatal(err)
	}
	if err := client.MarkConversationsRead(context.Background(), []string{"conv-one", "conv-two"}); err != nil {
		t.Fatal(err)
	}
	if requests != 3 {
		t.Fatalf("requests=%d", requests)
	}
}

func parseDMFixture(t *testing.T, name string, parse func([]byte) (ConversationPage, error)) ConversationPage {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", name))
	if err != nil {
		t.Fatal(err)
	}
	page, err := parse(body)
	if err != nil {
		t.Fatal(err)
	}
	return page
}
