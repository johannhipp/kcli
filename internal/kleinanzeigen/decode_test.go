package kleinanzeigen

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeJSONRejectsMalformedAndTrailingValues(t *testing.T) {
	for _, input := range [][]byte{[]byte(`{"broken":`), []byte(`{} {}`)} {
		if _, err := DecodeJSON(input); err == nil {
			t.Fatalf("accepted malformed input %q", input)
		}
	}
}

func TestUnwrapSingletonNumericAndHTML(t *testing.T) {
	decoded, err := DecodeJSON([]byte(`{"item":{"value":{"value":{"id":"00123","title":"A &amp; B"}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	unwrapped := HTMLUnescapeDocumentedFields(UnwrapValues(decoded))
	item, _ := FindKey(unwrapped, "item")
	rows, err := NormalizeSingleton(item)
	if err != nil || len(rows) != 1 {
		t.Fatalf("singleton normalization: %#v %v", rows, err)
	}
	object := rows[0].(map[string]any)
	if object["id"] != "00123" || object["title"] != "A & B" {
		t.Fatalf("unexpected normalization: %#v", object)
	}
	number, err := DecodeJSON([]byte(`12345678901234567890`))
	if err != nil {
		t.Fatal(err)
	}
	if number.(json.Number).String() != "12345678901234567890" {
		t.Fatalf("number lost precision: %v", number)
	}
}

func TestDecodeUniqueJSONRejectsDuplicateKeysRecursively(t *testing.T) {
	var destination map[string]any
	if err := DecodeUniqueJSON([]byte(`{"grant":{"code":"one","code":"two"}}`), &destination); err == nil || !strings.Contains(err.Error(), "duplicate JSON key") {
		t.Fatalf("expected duplicate rejection, got %v", err)
	}
	if err := DecodeUniqueJSON([]byte(`{"grant":{"code":"one"}}`), &destination); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeSingletonRejectsScalar(t *testing.T) {
	if _, err := NormalizeSingleton("not-a-collection"); err == nil {
		t.Fatal("accepted scalar collection")
	}
}
