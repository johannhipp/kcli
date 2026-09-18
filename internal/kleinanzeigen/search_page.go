package kleinanzeigen

import "github.com/johannhipp/kcli/internal/domain"

// SearchContinuation distinguishes a known terminal page (Next is nil) from a
// legacy response that does not report pagination (Continuation is nil).
type SearchContinuation struct {
	Next *int `json:"nextPage"`
}

// SearchPage is one decoded page from /api/ads.json.
type SearchPage struct {
	Listings     []SearchListing
	Total        int
	TotalKnown   bool
	Continuation *SearchContinuation
	Warnings     []domain.WarningV1
}
