package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/state"
)

const searchMaxScannedIDs = 1000

// Search resolves a reproducible search specification, fetches pages serially,
// and indexes public sellers encountered in successfully decoded pages.
func (a *App) Search(ctx context.Context, input domain.SearchInputV1) (domain.SearchOutputV1, error) {
	canonical, err := searchCanonicalInput(input)
	if err != nil {
		return domain.SearchOutputV1{}, err
	}
	if (canonical.Radius > 0 || canonical.Sort == "distance-asc") && canonical.Location == "" {
		return domain.SearchOutputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "radius and distance-asc sort require a location"}
	}
	if a == nil || a.Transport == nil {
		return domain.SearchOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "search transport is unavailable"}
	}
	if a.State == nil {
		return domain.SearchOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "search state is unavailable"}
	}
	if a.Clock == nil {
		return domain.SearchOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "search clock is unavailable"}
	}
	metadata := kleinanzeigen.NewMetadataService(a.Transport, a.State)
	warnings := []domain.WarningV1{}
	if canonical.Category != "" {
		resolved, resolveErr := metadata.Category(ctx, canonical.Category)
		if resolveErr != nil {
			return domain.SearchOutputV1{}, resolveErr
		}
		canonical.Category = resolved.Data.ID
		warnings = append(warnings, resolved.Warnings...)
	}
	if canonical.Location != "" {
		resolved, resolveErr := searchResolveLocation(ctx, metadata, canonical.Location)
		if resolveErr != nil {
			return domain.SearchOutputV1{}, resolveErr
		}
		canonical.Location = resolved.Data.ID
		warnings = append(warnings, resolved.Warnings...)
	}
	query, filterWarnings, err := searchBuildQuery(ctx, metadata, &canonical)
	if err != nil {
		return domain.SearchOutputV1{}, err
	}
	warnings = append(warnings, filterWarnings...)

	observedAt := a.Clock.Now().UTC()
	listings := make([]domain.ListingSummaryV1, 0, canonical.Limit)
	seen := make(map[string]struct{})
	pageNumber := canonical.Page
	fetched := 0
	noNewPages := 0
	stopReason := ""
	var lastTotal int
	var totalKnown bool
	var continuation *kleinanzeigen.SearchContinuation
	truncated := false
	for {
		query["page"] = []string{strconv.Itoa(pageNumber)}
		page, fetchErr := kleinanzeigen.SearchAds(ctx, a.Transport, query)
		if fetchErr != nil {
			return domain.SearchOutputV1{}, fetchErr
		}
		continuation = page.Continuation
		warnings = append(warnings, page.Warnings...)
		fetched += len(page.Listings)
		if page.TotalKnown {
			lastTotal = page.Total
			totalKnown = true
		}
		if err := a.State.SearchUpsertSellers(ctx, searchSellerRecords(page.Listings, observedAt)); err != nil {
			return domain.SearchOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "index search sellers", Cause: err}
		}
		newIDs := 0
		for index, listing := range page.Listings {
			if _, duplicate := seen[listing.Summary.ID]; duplicate {
				continue
			}
			seen[listing.Summary.ID] = struct{}{}
			newIDs++
			if searchExcluded(listing, canonical.Exclusions) {
				continue
			}
			summary := listing.Summary
			summary.ObservedAt = observedAt
			listings = append(listings, summary)
			if len(listings) == canonical.Limit {
				truncated = index+1 < len(page.Listings)
				break
			}
		}
		if newIDs == 0 {
			noNewPages++
		} else {
			noNewPages = 0
		}
		switch {
		case len(listings) >= canonical.Limit:
			stopReason = "bound"
		case len(page.Listings) == 0:
			stopReason = "empty_page"
		case noNewPages >= 2:
			stopReason = "two_pages_no_new_ids"
		case len(seen) >= searchMaxScannedIDs:
			stopReason = "scan_bound"
		case page.TotalKnown && (pageNumber+1)*canonical.PageSize >= page.Total:
			stopReason = "known_total"
		case continuation != nil && continuation.Next == nil:
			stopReason = "end_of_results"
		case !canonical.Paginate:
			stopReason = "single_page"
		}
		if stopReason != "" {
			break
		}
		pageNumber++
		if continuation != nil && continuation.Next != nil {
			pageNumber = *continuation.Next
		}
	}

	if truncated {
		warnings = append(warnings, domain.WarningV1{Code: "page_truncated", Message: "result limit truncated a website page; rerun this page with a larger limit before advancing", Details: map[string]any{"page": pageNumber}})
	}
	metadataDetails := map[string]any{"stop_reason": stopReason, "canonical_input": canonical}
	if totalKnown {
		metadataDetails["total"] = lastTotal
	}
	warnings = append(warnings, domain.WarningV1{Code: "search_metadata", Message: "search completed at a bounded stop condition", Details: metadataDetails})
	envelope := Envelope(a.Clock, "kcli.search-results/v1", "", transportSource(a.Transport), listings)
	envelope.ObservedAt = observedAt
	envelope.Page = &domain.PageV1{Number: canonical.Page, Size: canonical.PageSize, Fetched: fetched, Returned: len(listings)}
	envelope.Raw = nil
	envelope.Warnings = warnings
	for _, warning := range warnings {
		if warning.Code != "search_metadata" {
			envelope.Completeness = domain.CompletenessPartial
			break
		}
	}
	if stopReason == "scan_bound" {
		envelope.Completeness = domain.CompletenessPartial
	}
	if !truncated && continuation != nil && continuation.Next != nil {
		next := strconv.Itoa(*continuation.Next)
		envelope.Next = &next
	} else if !truncated && continuation == nil && !canonical.Paginate && stopReason == "single_page" {
		next := strconv.Itoa(canonical.Page + 1)
		envelope.Next = &next
	}
	return domain.SearchOutputV1{Envelope: envelope}, nil
}

func searchSellerRecords(listings []kleinanzeigen.SearchListing, observedAt time.Time) []state.SearchSellerRecord {
	records := make([]state.SearchSellerRecord, 0, len(listings))
	for _, listing := range listings {
		if listing.Seller.ID == "" {
			continue
		}
		records = append(records, state.SearchSellerRecord{SellerID: listing.Seller.ID, DisplayName: listing.Seller.Name, PublicJSON: listing.Seller.Raw, Source: "search", Completeness: string(domain.CompletenessBestEffort), ListingID: listing.Summary.ID, ListingTitle: listing.Summary.Title, ListingStatus: listing.Status, ListingURL: listing.Summary.URL, ObservedAt: observedAt})
	}
	return records
}

func searchExcluded(listing kleinanzeigen.SearchListing, exclusions []string) bool {
	haystack := searchFold(listing.Summary.Title + "\n" + listing.Description)
	for _, exclusion := range exclusions {
		if strings.Contains(haystack, searchFold(exclusion)) {
			return true
		}
	}
	return false
}
