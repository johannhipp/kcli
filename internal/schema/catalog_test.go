package schema

import (
	"encoding/json"
	"github.com/johannhipp/kcli/internal/domain"
	"reflect"
	"testing"
)

func TestRawResponseSchemaAcceptsJSONValues(t *testing.T) {
	type response struct {
		Raw json.RawMessage `json:"raw"`
	}
	schema, err := infer(reflect.TypeFor[response](), "response")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"unknown":true}`, `["future",1]`, `"text"`, `null`} {
		var value any
		if err := json.Unmarshal([]byte(`{"raw":`+raw+`}`), &value); err != nil {
			t.Fatal(err)
		}
		if err := resolved.Validate(value); err != nil {
			t.Errorf("raw %s rejected: %v", raw, err)
		}
	}
}

func TestFilterOverlayMatchesEnumAndStringValidation(t *testing.T) {
	static, err := infer(reflect.TypeFor[domain.SearchInputV1](), "SearchInputV1")
	if err != nil {
		t.Fatal(err)
	}
	result := OverlayFilters(static, []domain.FilterV1{
		{Key: "condition", Type: "enum", Classification: "accepted", SupportedValues: []domain.FilterSupportedValueV1{{Value: "used"}}},
		{Key: "features", Type: "string", Classification: "provisional", SupportedValues: []domain.FilterSupportedValueV1{{Value: "light"}}},
		{Key: "future", Type: "unknown", Classification: "unsupported-client"},
	})
	resolved, err := result.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		key, value string
		valid      bool
	}{
		{"condition", "used", true}, {"condition", "invented", false},
		{"features", "compact", true}, {"future", "value", false}, {"invented", "value", false},
	} {
		input := map[string]any{"filters": []any{map[string]any{"key": tc.key, "value": tc.value}}}
		if err := resolved.Validate(input); (err == nil) != tc.valid {
			t.Errorf("%s=%s: %v", tc.key, tc.value, err)
		}
	}
	if len(result.Extra["x-kcli-filter-metadata"].([]domain.FilterV1)) != 3 {
		t.Fatal("unknown metadata was lost")
	}
}
