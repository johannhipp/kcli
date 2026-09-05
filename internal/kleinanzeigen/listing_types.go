package kleinanzeigen

import (
	"encoding/json"
	"io"

	"github.com/johannhipp/kcli/internal/domain"
)

type ListingMedia = domain.ListingMediaV1

type ListingSeller = domain.SellerV1

type ListingDetail struct {
	Listing    domain.ListingV1
	Normalized map[string]any
	Media      []ListingMedia
	Seller     ListingSeller
	Raw        json.RawMessage
	Warnings   []domain.WarningV1
}

type MediaResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       io.ReadCloser
}
