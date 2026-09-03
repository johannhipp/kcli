package kleinanzeigen

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/johannhipp/kcli/internal/domain"
)

type MessageSendResult struct {
	WarningCode string
}

// SendMessage makes exactly one external-send request. It deliberately omits
// the reference client's unverified warn* suppression query parameters.
func (c *MessageClient) SendMessage(ctx context.Context, conversationID, message string) (MessageSendResult, error) {
	if c == nil || c.do == nil {
		return MessageSendResult{}, &domain.Error{Code: domain.CodeUnavailable, Message: "message gateway client is unavailable"}
	}
	if err := ValidateConversationID(conversationID); err != nil {
		return MessageSendResult{}, err
	}
	if err := validateOutboundText(message); err != nil {
		return MessageSendResult{}, err
	}
	body, err := json.Marshal(struct {
		Message string `json:"message"`
	}{Message: message})
	if err != nil {
		return MessageSendResult{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "encode outbound message", Cause: err}
	}
	response, err := c.do(ctx, Request{
		Host: HostGateway, Method: http.MethodPost,
		Path:    "/messagebox/api/users/" + c.accountID + "/conversations/" + conversationID,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    body, MaxResponseBytes: 1 << 20, Class: ExternalSend, OneShot: true,
	})
	if err != nil {
		return MessageSendResult{}, err
	}
	return MessageSendResult{WarningCode: blockedWarningCode(response.Body)}, nil
}

// CreateConversation makes exactly one external-create request and returns the
// new conversation ID. The caller sends the first text in a separate operation.
func (c *MessageClient) CreateConversation(ctx context.Context, listingID, contactName string) (string, error) {
	if c == nil || c.do == nil {
		return "", &domain.Error{Code: domain.CodeUnavailable, Message: "message gateway client is unavailable"}
	}
	canonicalListingID, err := ListingReferenceID(listingID)
	if err != nil {
		return "", err
	}
	if !validContactName(contactName) {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "contact name is required until its platform default is verified"}
	}
	response, err := c.do(ctx, Request{
		Host: HostMain, Method: http.MethodPost,
		Path:             "/api/users/" + c.accountID + "/create-conversation/" + canonicalListingID,
		Query:            map[string][]string{"contactName": {contactName}},
		MaxResponseBytes: 1 << 20, Class: ExternalCreate, OneShot: true,
	})
	if err != nil {
		return "", err
	}
	id := conversationIDFromCreate(response.Body)
	if err := ValidateConversationID(id); err != nil {
		return "", &domain.Error{Code: domain.CodeUpstreamContract, Message: "create-conversation response did not contain a usable conversation ID", Details: map[string]any{"status": response.StatusCode, "create_may_have_succeeded": true}}
	}
	return id, nil
}

func validateOutboundText(message string) error {
	if message == "" || len(message) > 64<<10 || !utf8.ValidString(message) || strings.IndexByte(message, 0) >= 0 {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "message must be non-empty valid UTF-8 no larger than 65536 bytes"}
	}
	return nil
}

func validContactName(value string) bool {
	return value != "" && len(value) <= 256 && utf8.ValidString(value) && strings.IndexFunc(value, func(r rune) bool {
		return r == 0 || r < 0x20 || r == 0x7f
	}) < 0
}

func conversationIDFromCreate(raw []byte) string {
	decoded, err := DecodeJSON(raw)
	if err != nil {
		return ""
	}
	decoded = UnwrapValues(decoded)
	root, ok := decoded.(map[string]any)
	if !ok {
		return ""
	}
	for _, path := range [][]string{{"id"}, {"conversationId"}, {"conversation-id"}, {"conversation", "id"}, {"data", "id"}, {"data", "conversation", "id"}} {
		current := any(root)
		for _, key := range path {
			object, objectOK := current.(map[string]any)
			if !objectOK {
				current = nil
				break
			}
			current, _ = directMessageField(object, key)
		}
		if value := messageIdentifierValue(current); value != "" {
			return value
		}
	}
	return ""
}

func messageIdentifierValue(value any) string {
	var text string
	switch typed := value.(type) {
	case string:
		text = typed
	case json.Number:
		text = typed.String()
	}
	if messageIdentifierPattern.MatchString(text) {
		return text
	}
	return ""
}

func blockedWarningCode(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	decoded, err := DecodeJSON(raw)
	if err != nil {
		return ""
	}
	decoded = UnwrapValues(decoded)
	root, ok := decoded.(map[string]any)
	if !ok {
		return ""
	}
	if sent, known := messageBoolField(root, "sent"); known && sent {
		return ""
	}
	blocked := false
	for _, key := range []string{"blocked", "warningBlocked", "warning-blocked"} {
		if value, known := messageBoolField(root, key); known && value {
			blocked = true
		}
	}
	warning, warningPresent := directMessageField(root, "warning")
	warnings, warningsPresent := directMessageField(root, "warnings")
	if !blocked && !warningPresent && !warningsPresent {
		return ""
	}
	for _, value := range []any{warning, warnings, root} {
		if code := warningCode(value); code != "" {
			return code
		}
	}
	return "platform_content_warning"
}

func warningCode(value any) string {
	switch typed := value.(type) {
	case string:
		if validWarningCode(typed) {
			return typed
		}
	case map[string]any:
		for _, key := range []string{"code", "warningCode", "warning-code", "type"} {
			if candidate, ok := directMessageField(typed, key); ok {
				if text, textOK := candidate.(string); textOK && validWarningCode(text) {
					return text
				}
			}
		}
		for _, key := range []string{"warning", "warnings", "contentWarnings", "content-warnings"} {
			if nested, ok := directMessageField(typed, key); ok {
				if code := warningCode(nested); code != "" {
					return code
				}
			}
		}
	case []any:
		for _, item := range typed {
			if code := warningCode(item); code != "" {
				return code
			}
		}
	}
	return ""
}

func validWarningCode(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, r := range value {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || ((r == '.' || r == '_' || r == '-') && index > 0) {
			continue
		}
		return false
	}
	return true
}
