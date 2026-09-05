package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestProjectionBeforeJSONEncoding(t *testing.T) {
	value := map[string]any{"schema": "example/v1", "data": map[string]any{"id": "42", "title": "kept", "other": "removed"}}
	var out bytes.Buffer
	if err := (Encoder{Format: FormatJSON, Fields: []string{"data.id", "data.title"}}).Encode(&out, value); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	data := got["data"].(map[string]any)
	if len(data) != 2 || data["id"] != "42" || data["title"] != "kept" {
		t.Fatalf("unexpected projection: %#v", got)
	}
	// An absent optional field is tolerated (omitted), not a fatal error.
	var allowed bytes.Buffer
	if err := (Encoder{Format: FormatJSON, Fields: []string{"data.missing"}}).Encode(&allowed, value); err != nil {
		t.Fatalf("absent optional field should be omitted: %v", err)
	}
	if got := allowed.String(); !strings.Contains(got, `"data": {}`) {
		t.Fatalf("absent field should project to empty object, got %s", got)
	}
}
func TestRawRedaction(t *testing.T) {
	value := map[string]any{"access_token": "secret-token", "nested": map[string]any{"email": "person@example.invalid", "safe": "contact person@example.invalid"}}
	var out bytes.Buffer
	if err := (Encoder{Format: FormatRaw}).Encode(&out, value); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, forbidden := range []string{"secret-token", "person@example.invalid"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("raw output leaked %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "[REDACTED]") || !strings.Contains(text, "[REDACTED_EMAIL]") {
		t.Fatalf("missing redaction markers: %s", text)
	}
}
func TestFiniteNDJSONEndsWithSummary(t *testing.T) {
	value := map[string]any{"request_id": "req_test", "observed_at": "2026-09-03T00:00:00Z", "data": []any{map[string]any{"id": "1"}, map[string]any{"id": "2"}}, "next": nil, "warnings": []any{}}
	var out bytes.Buffer
	if err := (Encoder{Format: FormatNDJSON}).Encode(&out, value); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || !strings.Contains(lines[2], `"schema":"kcli.summary/v1"`) {
		t.Fatalf("unexpected NDJSON: %q", lines)
	}
}
func TestTableEscapesTerminalControls(t *testing.T) {
	var out bytes.Buffer
	if err := (Encoder{Format: FormatTable}).Encode(&out, map[string]any{"data": []any{map[string]any{"title": "bad\x1b[31m\nnext"}}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "\nnext") {
		t.Fatalf("control sequence leaked: %q", out.String())
	}
}
