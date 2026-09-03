package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/app"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/output"
	"github.com/johannhipp/kcli/internal/state"
)

func TestDMReplyCommandResolvesExactMessageAndEmitsOneEnvelope(t *testing.T) {
	ctx := context.Background()
	database, err := state.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now().UTC()
	if err := database.AuthStoreAccount(ctx, strings.Repeat("e", 64), "987654"); err != nil {
		t.Fatal(err)
	}
	secrets := &dmCLISecrets{values: map[string]string{
		"default\x00" + app.AuthSecretAccessToken: "access-token", "default\x00" + app.AuthSecretRefreshToken: "refresh-token",
		"default\x00" + app.AuthSecretEmail: "redacted@example.invalid", "default\x00" + app.AuthSecretExpiry: now.Add(time.Hour).Format(time.RFC3339Nano),
	}}
	transport := &dmSendCLITransport{list: dmCLIFixture(t, "conversations.redacted.json"), listing: dmCLIFixture(t, "listing.json")}
	var stdout, stderr bytes.Buffer
	runtime := &Runtime{
		Context: ctx, Stdin: bytes.NewBuffer(nil), Stdout: &stdout, Stderr: &stderr,
		RequestID: "request-send", Profile: "default", Encoder: output.Encoder{Format: output.FormatJSON},
		Core: app.New(app.Dependencies{Transport: transport, State: database, Secrets: secrets}),
	}
	message := "[REDACTED_CLI_MESSAGE]\n"
	path := filepath.Join(t.TempDir(), "message.txt")
	if err := os.WriteFile(path, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&DMReplyCmd{ConversationID: "conv-new", MessageFile: path, DryRun: true}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	planEnvelope := assertSingleDMJSON(t, stdout.Bytes(), "kcli.dm-reply/v1")
	planData := planEnvelope["data"].(map[string]any)
	if planData["message_preview"] != message || planData["message_digest"] != state.DigestMessageContent(message) || !strings.Contains(stderr.String(), "digest-only local confirmation state") {
		t.Fatalf("plan=%#v stderr=%q", planData, stderr.String())
	}
	confirmationID := planData["confirmation_id"].(string)
	stdout.Reset()
	stderr.Reset()
	if err := (&DMReplyCmd{ConversationID: "conv-new", Message: message, Confirm: confirmationID}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	confirmed := assertSingleDMJSON(t, stdout.Bytes(), "kcli.dm-reply/v1")
	if confirmed["data"].(map[string]any)["outcome"] != "sent" || transport.sends.Load() != 1 || !strings.Contains(stderr.String(), "at most one external-send attempt") {
		t.Fatalf("confirmed=%#v sends=%d stderr=%q", confirmed, transport.sends.Load(), stderr.String())
	}
}

func TestDMStartCommandJSONInputAndVisibilityDiagnostic(t *testing.T) {
	ctx := context.Background()
	database, err := state.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Now().UTC()
	if err := database.AuthStoreAccount(ctx, strings.Repeat("f", 64), "987654"); err != nil {
		t.Fatal(err)
	}
	secrets := &dmCLISecrets{values: map[string]string{
		"default\x00" + app.AuthSecretAccessToken: "access-token", "default\x00" + app.AuthSecretRefreshToken: "refresh-token",
		"default\x00" + app.AuthSecretEmail: "redacted@example.invalid", "default\x00" + app.AuthSecretExpiry: now.Add(time.Hour).Format(time.RFC3339Nano),
	}}
	transport := &dmSendCLITransport{list: []byte(`{"data":[]}`), listing: dmCLIFixture(t, "listing.json")}
	input := map[string]any{"listing_id_or_url": "1234567890", "message": "[REDACTED_FIRST_MESSAGE]", "contact_name": "[REDACTED_CONTACT]"}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	runtime := &Runtime{
		Context: ctx, Stdin: bytes.NewReader(encoded), Stdout: &stdout, Stderr: &stderr,
		RequestID: "request-start", Profile: "default", Encoder: output.Encoder{Format: output.FormatJSON},
		Core: app.New(app.Dependencies{Transport: transport, State: database, Secrets: secrets}),
	}
	command := &DMStartCmd{ListingIDOrURL: "1234567890", Input: "-", DryRun: true}
	if err := command.Run(runtime); err != nil {
		t.Fatal(err)
	}
	envelope := assertSingleDMJSON(t, stdout.Bytes(), "kcli.dm-start/v1")
	data := envelope["data"].(map[string]any)
	if data["contact_name"] != "[REDACTED_CONTACT]" || data["creation_visibility"] != "may_be_visible_to_seller" || !strings.Contains(stderr.String(), "creating a conversation may be visible to the seller") {
		t.Fatalf("data=%#v stderr=%q", data, stderr.String())
	}

	_, err = dmStartCommandInput(&DMStartCmd{ListingIDOrURL: "1234567890", Input: "-", ContactName: "one", DryRun: true}, bytes.NewBufferString(`{"message":"[REDACTED]","contact_name":"two"}`))
	if err == nil {
		t.Fatal("conflicting --input and contact-name were accepted")
	}
	_, err = dmReplyCommandInput(&DMReplyCmd{ConversationID: "conv-new", Message: "one", MessageFile: "other", DryRun: true}, bytes.NewBuffer(nil))
	if err == nil {
		t.Fatal("ambiguous message sources were accepted")
	}
}

type dmSendCLITransport struct {
	list    []byte
	listing []byte
	sends   atomic.Int32
}

func (t *dmSendCLITransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	switch {
	case request.Class == kleinanzeigen.ExternalSend:
		t.sends.Add(1)
		return kleinanzeigen.Response{StatusCode: http.StatusNoContent}, nil
	case request.Host == kleinanzeigen.HostMain && request.Method == http.MethodGet:
		return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: append([]byte(nil), t.listing...)}, nil
	case request.Method == http.MethodGet:
		return kleinanzeigen.Response{StatusCode: http.StatusOK, Body: append([]byte(nil), t.list...)}, nil
	default:
		return kleinanzeigen.Response{StatusCode: http.StatusNotFound}, nil
	}
}
