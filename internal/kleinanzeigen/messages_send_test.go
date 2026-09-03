package kleinanzeigen

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestMessageSendAndStartRequestsAreOneShotWithoutWarningSuppression(t *testing.T) {
	warningFixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", "message-send-warning.redacted.json"))
	if err != nil {
		t.Fatal(err)
	}
	createdFixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", "conversation-created.redacted.json"))
	if err != nil {
		t.Fatal(err)
	}
	requests := make([]Request, 0, 3)
	responses := [][]byte{nil, warningFixture, createdFixture}
	client, err := NewMessageClient("987", func(ctx context.Context, request Request) (Response, error) {
		request.Context = ctx
		requests = append(requests, request)
		body := responses[len(requests)-1]
		return Response{StatusCode: http.StatusOK, Body: append([]byte(nil), body...)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	message := "[REDACTED_MESSAGE_SENTINEL]"
	result, err := client.SendMessage(context.Background(), "conv-one", message)
	if err != nil || result.WarningCode != "" {
		t.Fatalf("send=%#v err=%v", result, err)
	}
	warning, err := client.SendMessage(context.Background(), "conv-one", message)
	if err != nil || warning.WarningCode != "warnEmail" {
		t.Fatalf("warning=%#v err=%v", warning, err)
	}
	conversationID, err := client.CreateConversation(context.Background(), "123456", "[REDACTED_CONTACT]")
	if err != nil || conversationID != "conv-created" {
		t.Fatalf("conversation=%q err=%v", conversationID, err)
	}
	if len(requests) != 3 {
		t.Fatalf("requests=%d", len(requests))
	}
	for index, request := range requests {
		if !request.OneShot {
			t.Fatalf("request %d was not one-shot", index)
		}
	}
	for index := range 2 {
		request := requests[index]
		if request.Method != http.MethodPost || request.Class != ExternalSend || request.Host != HostGateway || len(request.Query) != 0 || request.Headers["Content-Type"][0] != "application/json" {
			t.Fatalf("send request %d=%#v", index, request)
		}
		var payload map[string]string
		if err := json.Unmarshal(request.Body, &payload); err != nil || payload["message"] != message || len(payload) != 1 {
			t.Fatalf("send payload=%#v err=%v", payload, err)
		}
	}
	create := requests[2]
	if create.Method != http.MethodPost || create.Class != ExternalCreate || create.Host != HostMain || create.Path != "/api/users/987/create-conversation/123456" || create.Query["contactName"][0] != "[REDACTED_CONTACT]" || len(create.Body) != 0 {
		t.Fatalf("create=%#v", create)
	}
}
