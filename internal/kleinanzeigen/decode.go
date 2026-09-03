package kleinanzeigen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"strings"

	"github.com/johannhipp/kcli/internal/domain"
)

func DecodeJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode mobile API JSON: %w", err)
	}
	if err := requireEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

// DecodeUniqueJSON rejects duplicate object keys. Use it for OAuth and
// confirmation input, where last-key-wins decoding would be ambiguous.
func DecodeUniqueJSON(raw []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeUniqueValue(decoder)
	if err != nil {
		return err
	}
	if err := requireEOF(decoder); err != nil {
		return err
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("normalize JSON: %w", err)
	}
	strict := json.NewDecoder(bytes.NewReader(normalized))
	strict.UseNumber()
	if err := strict.Decode(destination); err != nil {
		return fmt.Errorf("decode JSON input: %w", err)
	}
	return nil
}

func decodeUniqueValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("decode JSON input: %w", err)
	}
	switch current := token.(type) {
	case json.Delim:
		switch current {
		case '{':
			object := make(map[string]any)
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, fmt.Errorf("decode JSON object key: %w", err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("JSON object key is not a string")
				}
				if _, exists := object[key]; exists {
					return nil, fmt.Errorf("duplicate JSON key %q", key)
				}
				value, err := decodeUniqueValue(decoder)
				if err != nil {
					return nil, err
				}
				object[key] = value
			}
			if _, err := decoder.Token(); err != nil {
				return nil, fmt.Errorf("close JSON object: %w", err)
			}
			return object, nil
		case '[':
			var array []any
			for decoder.More() {
				value, err := decodeUniqueValue(decoder)
				if err != nil {
					return nil, err
				}
				array = append(array, value)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, fmt.Errorf("close JSON array: %w", err)
			}
			return array, nil
		default:
			return nil, fmt.Errorf("unexpected JSON delimiter %q", current)
		}
	default:
		return current, nil
	}
}

func requireEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return nil
}

func UnwrapValues(value any) any {
	switch current := value.(type) {
	case map[string]any:
		if len(current) == 1 {
			if wrapped, ok := current["value"]; ok {
				return UnwrapValues(wrapped)
			}
		}
		out := make(map[string]any, len(current))
		for key, child := range current {
			out[key] = UnwrapValues(child)
		}
		return out
	case []any:
		out := make([]any, len(current))
		for index, child := range current {
			out[index] = UnwrapValues(child)
		}
		return out
	default:
		return current
	}
}

func NormalizeSingleton(value any) ([]any, error) {
	switch current := value.(type) {
	case nil:
		return []any{}, nil
	case []any:
		return current, nil
	case map[string]any:
		return []any{current}, nil
	default:
		return nil, fmt.Errorf("expected object or array, got %T", value)
	}
}

func HTMLUnescapeDocumentedFields(value any) any {
	return unescapeFields(value, "")
}

func unescapeFields(value any, key string) any {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for childKey, child := range current {
			out[childKey] = unescapeFields(child, childKey)
		}
		return out
	case []any:
		out := make([]any, len(current))
		for index, child := range current {
			out[index] = unescapeFields(child, key)
		}
		return out
	case string:
		if documentedTextField(key) {
			return html.UnescapeString(current)
		}
		return current
	default:
		return current
	}
}

func documentedTextField(key string) bool {
	switch strings.ToLower(key) {
	case "title", "description", "localized-name", "localized-label", "contact-name", "text", "textshort", "textshorttrimmed":
		return true
	default:
		return false
	}
}

func FindKey(value any, localName string) (any, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	for key, child := range object {
		if key == localName || strings.HasSuffix(key, "}"+localName) || strings.HasSuffix(key, "/"+localName) {
			return child, true
		}
	}
	for _, child := range object {
		if found, ok := FindKey(child, localName); ok {
			return found, true
		}
		if array, ok := child.([]any); ok {
			for _, element := range array {
				if found, ok := FindKey(element, localName); ok {
					return found, true
				}
			}
		}
	}
	return nil, false
}

func StringValue(value any) (string, bool) {
	value = UnwrapValues(value)
	switch current := value.(type) {
	case string:
		return current, true
	case json.Number:
		return current.String(), true
	default:
		return "", false
	}
}

func ContractWarning(field string, got any) domain.WarningV1 {
	return domain.WarningV1{Code: "upstream_contract_drift", Message: "mobile API response contained an unexpected shape", Details: map[string]any{"field": field, "type": fmt.Sprintf("%T", got)}}
}
