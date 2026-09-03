package domain

import (
	"encoding/json"
	"time"
)

type Completeness string

const (
	CompletenessComplete   Completeness = "complete"
	CompletenessPartial    Completeness = "partial"
	CompletenessDirect     Completeness = "direct"
	CompletenessKnownOnly  Completeness = "known-only"
	CompletenessBestEffort Completeness = "best-effort"
)

type WarningV1 struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

type PageV1 struct {
	Number   int `json:"number"`
	Size     int `json:"size"`
	Fetched  int `json:"fetched"`
	Returned int `json:"returned"`
}

type Envelope[T any] struct {
	Schema       string          `json:"schema"`
	RequestID    string          `json:"request_id"`
	Source       string          `json:"source"`
	ObservedAt   time.Time       `json:"observed_at"`
	Completeness Completeness    `json:"completeness"`
	Data         T               `json:"data"`
	Page         *PageV1         `json:"page,omitempty"`
	Next         *string         `json:"next"`
	Warnings     []WarningV1     `json:"warnings"`
	Raw          json.RawMessage `json:"raw,omitempty"`
}

type ListingSummaryV1 struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	Price      string `json:"price,omitempty"`
	PriceCents *int64 `json:"price_cents,omitempty"`
	URL        string `json:"url"`
}
type CategoryV1 struct {
	ID       string          `json:"id"`
	Path     string          `json:"path"`
	Label    string          `json:"label"`
	ParentID string          `json:"parent_id,omitempty"`
	Raw      json.RawMessage `json:"raw"`
}
type LocationV1 struct {
	ID    string          `json:"id"`
	Label string          `json:"label"`
	Raw   json.RawMessage `json:"raw"`
}
type FilterSupportedValueV1 struct {
	Value string `json:"value"`
	Label string `json:"label"`
}
type FilterV1 struct {
	CategoryID      string                   `json:"category_id"`
	Key             string                   `json:"key"`
	Label           string                   `json:"label,omitempty"`
	Type            string                   `json:"type"`
	SearchParam     string                   `json:"search_param"`
	SearchStyle     string                   `json:"search_style"`
	SupportedValues []FilterSupportedValueV1 `json:"supported_values"`
	Classification  string                   `json:"classification"`
	Proof           string                   `json:"proof"`
	Raw             json.RawMessage          `json:"raw"`
}
type ListingV1 struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Description  string `json:"description,omitempty"`
	URL          string `json:"url"`
	Amount       string `json:"amount,omitempty"`
	AmountCents  *int64 `json:"amount_cents,omitempty"`
	Availability string `json:"availability"`
}
type SellerV1 struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Source       string `json:"source"`
	Completeness string `json:"completeness"`
}
type ConversationV1 struct {
	ID           string `json:"id"`
	ListingID    string `json:"listing_id,omitempty"`
	Unread       bool   `json:"unread"`
	MessageCount int    `json:"message_count"`
	Preview      string `json:"preview,omitempty"`
}
type MessageV1 struct {
	ID             string    `json:"id,omitempty"`
	ConversationID string    `json:"conversation_id"`
	Direction      string    `json:"direction"`
	ReceivedAt     time.Time `json:"received_at"`
	Text           string    `json:"text"`
}

type SearchOutputV1 struct{ Envelope[[]ListingSummaryV1] }
type CategoryListOutputV1 struct{ Envelope[[]CategoryV1] }
type CategoryOutputV1 struct{ Envelope[CategoryV1] }
type LocationOutputV1 struct{ Envelope[[]LocationV1] }
type FilterListOutputV1 struct{ Envelope[[]FilterV1] }
type FilterOutputV1 struct{ Envelope[FilterV1] }
type ListingOutputV1 struct{ Envelope[ListingV1] }
type ListingImagesOutputV1 struct{ Envelope[[]map[string]any] }
type ListingOpenOutputV1 struct{ Envelope[map[string]string] }
type SellerOutputV1 struct{ Envelope[SellerV1] }
type SellerListOutputV1 struct{ Envelope[[]SellerV1] }
type SellerListingsOutputV1 struct{ Envelope[[]ListingSummaryV1] }
type AuthOutputV1 struct{ Envelope[map[string]any] }
type DMListOutputV1 struct{ Envelope[[]ConversationV1] }
type DMOutputV1 struct{ Envelope[map[string]any] }
type SchemaOutputV1 struct{ Envelope[any] }
type ConfigOutputV1 struct{ Envelope[any] }
type DoctorOutputV1 struct{ Envelope[[]DoctorCheckV1] }
type CompletionOutputV1 struct{ Envelope[string] }
type VersionOutputV1 struct{ Envelope[any] }

type DoctorCheckV1 struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type ErrorEnvelopeV1 struct {
	Schema     string         `json:"schema"`
	Code       ErrorCode      `json:"code"`
	Message    string         `json:"message"`
	Retryable  bool           `json:"retryable"`
	RequestID  string         `json:"request_id,omitempty"`
	RetryAfter string         `json:"retry_after,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}
