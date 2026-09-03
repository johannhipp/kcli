package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/app"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/output"
	"github.com/johannhipp/kcli/internal/secret"
	"github.com/johannhipp/kcli/internal/state"
)

func TestDMCommandsEmitSingleJSONEnvelopeAndSideEffectDiagnostics(t *testing.T) {
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
		"default\x00" + app.AuthSecretAccessToken:  "access-token",
		"default\x00" + app.AuthSecretRefreshToken: "refresh-token",
		"default\x00" + app.AuthSecretEmail:        "redacted@example.invalid",
		"default\x00" + app.AuthSecretExpiry:       now.Add(time.Hour).Format(time.RFC3339Nano),
	}}
	transport := &dmCLITransport{
		list: dmCLIFixture(t, "conversations.redacted.json"),
		get:  dmCLIFixture(t, "conversation-messages.redacted.json"),
	}
	var stdout, stderr bytes.Buffer
	runtime := &Runtime{
		Context: ctx, Stdout: &stdout, Stderr: &stderr, RequestID: "request-dm", Profile: "default",
		Encoder: output.Encoder{Format: output.FormatJSON},
		Core:    app.New(app.Dependencies{Transport: transport, State: database, Secrets: secrets}),
	}
	if err := (&DMListCmd{PageSize: 50, Limit: 50}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	assertSingleDMJSON(t, stdout.Bytes(), "kcli.conversations/v1")
	if stderr.Len() != 0 {
		t.Fatalf("list diagnostics=%q", stderr.String())
	}

	stdout.Reset()
	if err := (&DMGetCmd{ConversationID: "conv-new"}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	getEnvelope := assertSingleDMJSON(t, stdout.Bytes(), "kcli.conversation/v1")
	data, _ := getEnvelope["data"].(map[string]any)
	if data["account_state_touching"] != true || data["side_effect"] != "account-state" || !strings.Contains(stderr.String(), "account-state-touching") {
		t.Fatalf("get data=%#v diagnostics=%q", data, stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if err := (&DMMarkReadCmd{ConversationIDs: []string{"conv-new"}, DryRun: true}).Run(runtime); err != nil {
		t.Fatal(err)
	}
	markEnvelope := assertSingleDMJSON(t, stdout.Bytes(), "kcli.dm-mark-read/v1")
	markData, _ := markEnvelope["data"].(map[string]any)
	if markData["dry_run"] != true || markData["network_call"] != false || !strings.Contains(stderr.String(), "no account-state mutation") {
		t.Fatalf("mark data=%#v diagnostics=%q", markData, stderr.String())
	}
}

type dmCLISecrets struct{ values map[string]string }

func (s *dmCLISecrets) Get(profile, name string) (string, error) {
	value, ok := s.values[profile+"\x00"+name]
	if !ok {
		return "", secret.ErrNotFound
	}
	return value, nil
}
func (s *dmCLISecrets) Set(profile, name, value string) error {
	s.values[profile+"\x00"+name] = value
	return nil
}
func (s *dmCLISecrets) Delete(profile, name string) error {
	delete(s.values, profile+"\x00"+name)
	return nil
}

type dmCLITransport struct {
	list []byte
	get  []byte
}

func (t *dmCLITransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	if request.Method == "GET" {
		return kleinanzeigen.Response{StatusCode: 200, Body: append([]byte(nil), t.list...)}, nil
	}
	return kleinanzeigen.Response{StatusCode: 200, Body: append([]byte(nil), t.get...)}, nil
}

func assertSingleDMJSON(t *testing.T, raw []byte, schema string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(raw))
	var envelope map[string]any
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["schema"] != schema {
		t.Fatalf("schema=%#v envelope=%#v", envelope["schema"], envelope)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		t.Fatalf("output contains more than one JSON value: err=%v value=%#v", err, trailing)
	}
	return envelope
}

func dmCLIFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}
