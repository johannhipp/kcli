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
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	Price        string       `json:"price,omitempty"`
	PriceCents   *int64       `json:"price_cents,omitempty"`
	URL          string       `json:"url"`
	Status       string       `json:"status"`
	Source       string       `json:"source"`
	Completeness Completeness `json:"completeness"`
	ObservedAt   time.Time    `json:"observed_at"`
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
type ListingCategoryV1 struct {
	ID     string         `json:"id,omitempty"`
	Path   string         `json:"path,omitempty"`
	Label  string         `json:"label,omitempty"`
	Public map[string]any `json:"public,omitempty"`
}

type ListingLocationV1 struct {
	Label       string         `json:"label,omitempty"`
	Postcode    string         `json:"postcode,omitempty"`
	Latitude    string         `json:"latitude,omitempty"`
	Longitude   string         `json:"longitude,omitempty"`
	Distance    string         `json:"distance,omitempty"`
	Approximate bool           `json:"approximate"`
	Public      map[string]any `json:"public,omitempty"`
}

type ListingShippingV1 struct {
	Available *bool          `json:"available,omitempty"`
	Cost      string         `json:"cost,omitempty"`
	Public    map[string]any `json:"public,omitempty"`
}

type ListingAttributeV1 struct {
	Name   string         `json:"name,omitempty"`
	Label  string         `json:"label,omitempty"`
	Value  any            `json:"value,omitempty"`
	Values []any          `json:"values"`
	Public map[string]any `json:"public"`
}

type ListingMediaV1 struct {
	Index     int    `json:"index"`
	Relation  string `json:"relation"`
	URL       string `json:"url"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
	SizeLabel string `json:"size_label,omitempty"`
}

type SellerRatingV1 struct {
	Score  string         `json:"score,omitempty"`
	Count  *int64         `json:"count,omitempty"`
	Public map[string]any `json:"public,omitempty"`
}

type SellerCompanyV1 struct {
	Name    string         `json:"name,omitempty"`
	Details map[string]any `json:"details,omitempty"`
}

type SellerV1 struct {
	ID              string           `json:"id"`
	Name            string           `json:"name"`
	ContactInitials string           `json:"contact_initials,omitempty"`
	ProfileURL      string           `json:"profile_url,omitempty"`
	AccountType     string           `json:"account_type,omitempty"`
	AccountSince    string           `json:"account_since,omitempty"`
	AccountAge      string           `json:"account_age,omitempty"`
	PosterType      string           `json:"poster_type,omitempty"`
	Rating          *SellerRatingV1  `json:"rating,omitempty"`
	Badges          []string         `json:"badges"`
	Company         *SellerCompanyV1 `json:"company,omitempty"`
	Public          map[string]any   `json:"public"`
	Source          string           `json:"source"`
	Completeness    Completeness     `json:"completeness"`
	ObservedAt      time.Time        `json:"observed_at"`
}

type ListingV1 struct {
	ID               string               `json:"id"`
	Title            string               `json:"title"`
	Description      string               `json:"description,omitempty"`
	URL              string               `json:"url"`
	Category         *ListingCategoryV1   `json:"category,omitempty"`
	AdType           string               `json:"ad_type,omitempty"`
	ListingType      string               `json:"listing_type,omitempty"`
	Status           string               `json:"status,omitempty"`
	Labels           []string             `json:"labels"`
	PostedAt         string               `json:"posted_at,omitempty"`
	EditedAt         string               `json:"edited_at,omitempty"`
	EndsAt           string               `json:"ends_at,omitempty"`
	ViewCount        *int64               `json:"view_count,omitempty"`
	ContractWarnings []any                `json:"contract_warnings"`
	Amount           string               `json:"amount,omitempty"`
	AmountCents      *int64               `json:"amount_cents,omitempty"`
	PriceType        string               `json:"price_type,omitempty"`
	Location         *ListingLocationV1   `json:"location,omitempty"`
	Pickup           *bool                `json:"pickup,omitempty"`
	Shipping         *ListingShippingV1   `json:"shipping,omitempty"`
	Attributes       []ListingAttributeV1 `json:"attributes"`
	Media            []ListingMediaV1     `json:"media"`
	PosterType       string               `json:"poster_type,omitempty"`
	Seller           SellerV1             `json:"seller"`
	Availability     string               `json:"availability"`
	AdditionalFields map[string]any       `json:"additional_fields,omitempty"`
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
