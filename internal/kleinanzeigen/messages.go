package kleinanzeigen

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

const messageResponseLimit int64 = 10 << 20

var messageIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

type AuthenticatedDo func(context.Context, Request) (Response, error)

type MessageClient struct {
	accountID string
	do        AuthenticatedDo
}

type ConversationSummary struct {
	ID                 string
	ListingID          string
	ListingTitle       string
	ListingStatus      string
	Role               string
	SellerName         string
	BuyerName          string
	Counterparty       string
	Unread             bool
	UnreadKnown        bool
	UnreadMessageCount int
	UnreadCountKnown   bool
	MessageCount       int
	MessageCountKnown  bool
	ReceivedAt         time.Time
	ReceivedRaw        string
	Preview            string
}

type ConversationPage struct {
	Conversations []ConversationSummary
	Warnings      []domain.WarningV1
}

type Message struct {
	ID          string
	Direction   string
	Kind        string
	ReceivedAt  time.Time
	ReceivedRaw string
	Text        string
	Raw         json.RawMessage
}

type OpenedConversation struct {
	Conversation ConversationSummary
	Messages     []Message
	Warnings     []domain.WarningV1
}

func NewMessageClient(accountID string, do AuthenticatedDo) (*MessageClient, error) {
	if !messageIdentifierPattern.MatchString(accountID) || do == nil {
		return nil, &domain.Error{Code: domain.CodeUnavailable, Message: "message gateway client is unavailable"}
	}
	return &MessageClient{accountID: accountID, do: do}, nil
}

func ValidateConversationID(id string) error {
	if !messageIdentifierPattern.MatchString(id) {
		return &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "invalid conversation ID"}
	}
	return nil
}

func (c *MessageClient) ListConversations(ctx context.Context, page, size int) (ConversationPage, error) {
	if c == nil || c.do == nil || page < 0 || size < 1 || size > 100 {
		return ConversationPage{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "conversation page must use a non-negative page and size between 1 and 100"}
	}
	response, err := c.do(ctx, Request{
		Host: HostGateway, Method: http.MethodGet,
		Path:             "/messagebox/api/users/" + c.accountID + "/conversations",
		Query:            map[string][]string{"page": {strconv.Itoa(page)}, "size": {strconv.Itoa(size)}},
		MaxResponseBytes: messageResponseLimit, Class: StableRead,
	})
	if err != nil {
		return ConversationPage{}, err
	}
	return ParseConversations(response.Body)
}

func (c *MessageClient) OpenConversation(ctx context.Context, conversationID string) (OpenedConversation, error) {
	if c == nil || c.do == nil {
		return OpenedConversation{}, &domain.Error{Code: domain.CodeUnavailable, Message: "message gateway client is unavailable"}
	}
	if err := ValidateConversationID(conversationID); err != nil {
		return OpenedConversation{}, err
	}
	response, err := c.do(ctx, Request{
		Host: HostGateway, Method: http.MethodPut,
		Path:             "/messagebox/api/users/" + c.accountID + "/conversations/" + conversationID,
		Query:            map[string][]string{"contentWarnings": {"true"}},
		MaxResponseBytes: messageResponseLimit, Class: StateTouchingRead, OneShot: true,
	})
	if err != nil {
		return OpenedConversation{}, err
	}
	opened, err := ParseConversation(response.Body)
	if err != nil {
		return OpenedConversation{}, err
	}
	if opened.Conversation.ID == "" {
		opened.Conversation.ID = conversationID
	} else if opened.Conversation.ID != conversationID {
		return OpenedConversation{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "opened conversation ID did not match the request"}
	}
	return opened, nil
}

func (c *MessageClient) MarkConversationsRead(ctx context.Context, ids []string) error {
	if c == nil || c.do == nil || len(ids) == 0 || len(ids) > 100 {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "mark-read requires between 1 and 100 conversation IDs"}
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if err := ValidateConversationID(id); err != nil {
			return err
		}
		if _, duplicate := seen[id]; duplicate {
			return &domain.Error{Code: domain.CodeInvalidInput, Message: "conversation IDs must be unique"}
		}
		seen[id] = struct{}{}
	}
	joined := strings.Join(ids, ",")
	if len(joined) > 8192 {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "combined conversation IDs are too large"}
	}
	_, err := c.do(ctx, Request{
		Host: HostGateway, Method: http.MethodPost,
		Path:             "/messagebox/api/users/" + c.accountID + "/conversations/read",
		Query:            map[string][]string{"ids": {joined}},
		MaxResponseBytes: 1 << 20, Class: AccountMutation, OneShot: true,
	})
	return err
}

func ParseConversations(raw []byte) (ConversationPage, error) {
	decoded, err := DecodeJSON(raw)
	if err != nil {
		return ConversationPage{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "conversation list response was not valid JSON", Cause: err}
	}
	decoded = HTMLUnescapeDocumentedFields(UnwrapValues(decoded))
	collection, ok := conversationCollection(decoded)
	if !ok {
		return ConversationPage{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "conversation list response did not contain conversations"}
	}
	rows, err := NormalizeSingleton(collection)
	if err != nil {
		return ConversationPage{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "conversation list had an unexpected shape", Cause: err}
	}
	page := ConversationPage{Conversations: []ConversationSummary{}, Warnings: []domain.WarningV1{}}
	for _, row := range rows {
		object, objectOK := row.(map[string]any)
		if !objectOK {
			page.Warnings = append(page.Warnings, ContractWarning("conversations", row))
			continue
		}
		summary := parseConversationSummary(object)
		if summary.ID == "" || !messageIdentifierPattern.MatchString(summary.ID) {
			page.Warnings = append(page.Warnings, domain.WarningV1{Code: "conversation_missing_id", Message: "a conversation without a usable ID was skipped"})
			continue
		}
		page.Conversations = append(page.Conversations, summary)
	}
	return page, nil
}

func ParseConversation(raw []byte) (OpenedConversation, error) {
	decoded, err := DecodeJSON(raw)
	if err != nil {
		return OpenedConversation{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "conversation response was not valid JSON", Cause: err}
	}
	decoded = HTMLUnescapeDocumentedFields(UnwrapValues(decoded))
	root, ok := decoded.(map[string]any)
	if !ok {
		return OpenedConversation{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "conversation response had an unexpected shape"}
	}
	conversationObject := root
	if data, exists := directMessageField(root, "data"); exists {
		if object, objectOK := data.(map[string]any); objectOK {
			conversationObject = object
		}
	}
	if nested, exists := directMessageField(conversationObject, "conversation"); exists {
		if object, objectOK := nested.(map[string]any); objectOK {
			conversationObject = object
		}
	}
	opened := OpenedConversation{Conversation: parseConversationSummary(conversationObject), Messages: []Message{}, Warnings: []domain.WarningV1{}}
	messagesValue, exists := directMessageField(root, "messages")
	if !exists {
		if data, dataExists := directMessageField(root, "data"); dataExists {
			if dataObject, dataOK := data.(map[string]any); dataOK {
				messagesValue, exists = directMessageField(dataObject, "messages")
			}
		}
	}
	if !exists {
		return OpenedConversation{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "conversation response did not contain messages"}
	}
	rows, err := NormalizeSingleton(messagesValue)
	if err != nil {
		return OpenedConversation{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "conversation messages had an unexpected shape", Cause: err}
	}
	for _, row := range rows {
		object, objectOK := row.(map[string]any)
		if !objectOK {
			opened.Warnings = append(opened.Warnings, ContractWarning("messages", row))
			continue
		}
		message := parseMessage(object)
		if message.Direction != "IN" && message.Direction != "OUT" {
			opened.Warnings = append(opened.Warnings, domain.WarningV1{Code: "message_unknown_direction", Message: "a message had no recognized direction"})
		}
		opened.Messages = append(opened.Messages, message)
	}
	return opened, nil
}

func conversationCollection(decoded any) (any, bool) {
	root, ok := decoded.(map[string]any)
	if !ok {
		if _, array := decoded.([]any); array {
			return decoded, true
		}
		return nil, false
	}
	if value, exists := directMessageField(root, "conversations"); exists {
		return value, true
	}
	data, exists := directMessageField(root, "data")
	if !exists {
		return nil, false
	}
	if _, array := data.([]any); array {
		return data, true
	}
	if object, objectOK := data.(map[string]any); objectOK {
		for _, key := range []string{"conversations", "items"} {
			if value, found := directMessageField(object, key); found {
				return value, true
			}
		}
		if _, hasID := directMessageField(object, "id"); hasID {
			return object, true
		}
	}
	return nil, false
}

func parseConversationSummary(object map[string]any) ConversationSummary {
	var summary ConversationSummary
	summary.ID, _ = messageStringField(object, "id")
	summary.ListingID = firstMessageString(object, "adId", "ad-id", "listingId", "listing-id")
	summary.ListingTitle = firstMessageString(object, "adTitle", "ad-title", "listingTitle", "listing-title")
	summary.ListingStatus = firstMessageString(object, "adStatus", "ad-status", "listingStatus", "listing-status")
	summary.Role = strings.ToUpper(firstMessageString(object, "role"))
	summary.SellerName = firstMessageString(object, "sellerName", "seller-name")
	summary.BuyerName = firstMessageString(object, "buyerName", "buyer-name")
	if summary.Role == "BUYER" {
		summary.Counterparty = summary.SellerName
	} else {
		summary.Counterparty = summary.BuyerName
	}
	summary.Unread, summary.UnreadKnown = messageBoolField(object, "unread")
	summary.UnreadMessageCount, summary.UnreadCountKnown = messageIntField(object, "unreadMessagesCount", "unread-messages-count")
	summary.MessageCount, summary.MessageCountKnown = messageIntField(object, "messageCount", "messagesCount", "message-count")
	if summary.MessageCount == 0 && summary.UnreadMessageCount > 0 {
		summary.MessageCount = summary.UnreadMessageCount
		summary.MessageCountKnown = summary.UnreadCountKnown
	}
	summary.ReceivedRaw = firstMessageString(object, "receivedDate", "received-date", "lastActivity", "last-activity")
	summary.ReceivedAt = parseMessageTime(summary.ReceivedRaw)
	summary.Preview = firstMessageString(object, "textShortTrimmed", "textShort", "preview")
	return summary
}

func parseMessage(object map[string]any) Message {
	message := Message{}
	message.ID = firstMessageString(object, "id", "messageId", "message-id")
	message.Direction = strings.ToUpper(firstMessageString(object, "boundness", "direction"))
	message.Kind = firstMessageString(object, "type", "kind", "messageType", "message-type")
	if message.Kind == "" {
		message.Kind = "text"
	}
	message.ReceivedRaw = firstMessageString(object, "receivedDate", "received-date", "createdAt", "created-at")
	message.ReceivedAt = parseMessageTime(message.ReceivedRaw)
	message.Text = firstMessageString(object, "text", "textShort", "title")
	if !recognizedMessageKind(message.Kind) {
		message.Raw, _ = json.Marshal(object)
	}
	return message
}

func recognizedMessageKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "text", "message", "plain_text", "plaintext":
		return true
	default:
		return false
	}
}

func parseMessageTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC()
	}
	if milliseconds, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.UnixMilli(milliseconds).UTC()
	}
	return time.Time{}
}

func directMessageField(object map[string]any, name string) (any, bool) {
	for key, value := range object {
		if key == name || strings.EqualFold(key, name) || strings.HasSuffix(key, "}"+name) || strings.HasSuffix(key, "/"+name) {
			return value, true
		}
	}
	return nil, false
}

func messageStringField(object map[string]any, name string) (string, bool) {
	value, ok := directMessageField(object, name)
	if !ok {
		return "", false
	}
	return StringValue(value)
}

func firstMessageString(object map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := messageStringField(object, name); ok {
			return value
		}
	}
	return ""
}

func messageBoolField(object map[string]any, name string) (bool, bool) {
	value, ok := directMessageField(object, name)
	if !ok {
		return false, false
	}
	value = UnwrapValues(value)
	switch current := value.(type) {
	case bool:
		return current, true
	case string:
		parsed, err := strconv.ParseBool(current)
		return parsed, err == nil
	default:
		return false, false
	}
}

func messageIntField(object map[string]any, names ...string) (int, bool) {
	for _, name := range names {
		value, ok := directMessageField(object, name)
		if !ok {
			continue
		}
		text, textOK := StringValue(value)
		if !textOK {
			continue
		}
		parsed, err := strconv.Atoi(text)
		if err == nil && parsed >= 0 {
			return parsed, true
		}
	}
	return 0, false
}

func (c ConversationSummary) Validate() error {
	if !messageIdentifierPattern.MatchString(c.ID) {
		return fmt.Errorf("invalid conversation ID")
	}
	return nil
}
