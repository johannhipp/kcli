package kleinanzeigen

import (
	"net/url"
	"strings"
)

var (
	listingSensitiveKeys = map[string]bool{"authorization": true, "access_token": true, "refresh_token": true, "id_token": true, "email": true, "password": true, "client_secret": true, "message": true, "message_body": true, "authorization_code": true, "code_verifier": true, "oauth_state": true}
)

func listingRedact(value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		sensitiveNamedValue := listingSensitiveNamedValue(current)
		for childKey, child := range current {
			local := strings.ToLower(listingLocalName(childKey))
			if listingSensitiveKey(childKey) || sensitiveNamedValue && (local == "value" || local == "values") {
				out[childKey] = "[REDACTED]"
			} else {
				out[childKey] = listingRedact(child)
			}
		}
		return out
	case []any:
		out := make([]any, len(current))
		for index, child := range current {
			out[index] = listingRedact(child)
		}
		return out
	case string:
		return listingRedactString(current)
	default:
		return current
	}
}

func listingSensitiveNamedValue(object map[string]any) bool {
	for _, field := range []string{"name", "key"} {
		if value, ok := listingFieldString(object, field); ok && listingSensitiveKey(value) {
			return true
		}
	}
	return false
}

func listingSensitiveKey(key string) bool {
	local := strings.ToLower(listingLocalName(key))
	return listingSensitiveKeys[local] || strings.HasSuffix(local, "-email") || strings.HasSuffix(local, "_email")
}

func listingRedactString(value string) string {
	value = RedactText(value)
	if !strings.Contains(value, "?") {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	query := parsed.Query()
	changed := false
	for key := range query {
		if listingSensitiveKeys[strings.ToLower(key)] {
			query.Set(key, "REDACTED")
			changed = true
		}
	}
	if changed {
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return value
}
