package app

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/state"
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

var (
	sellerIDPattern           = regexp.MustCompile(`^[0-9]+$`)
	sellerAPIPathPattern      = regexp.MustCompile(`^/api/users/([0-9]+)(?:\.json)?/?$`)
	sellerProviderPathPattern = regexp.MustCompile(`^/s-anbieter/[^/]+/([0-9]+)/?$`)
)

func (a *App) SellerGet(ctx context.Context, input domain.SellerGetInputV1, requestID string) (domain.SellerOutputV1, error) {
	if a == nil || a.State == nil || a.Clock == nil {
		return domain.SellerOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "seller runtime dependencies are unavailable"}
	}
	if (input.IDOrURL == "") == (input.Listing == "") {
		return domain.SellerOutputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "provide exactly one seller reference or listing reference"}
	}
	if input.Listing != "" {
		detail, observedAt, err := a.listingDetail(ctx, input.Listing)
		if err != nil {
			return domain.SellerOutputV1{}, err
		}
		if detail.Seller.ID == "" || detail.Seller.Name == "" {
			return domain.SellerOutputV1{}, &domain.Error{Code: domain.CodeNotFound, Message: "listing detail did not expose a seller"}
		}
		data := domain.SellerV1{ID: detail.Seller.ID, Name: detail.Seller.Name, Source: "listing", Completeness: string(domain.CompletenessDirect)}
		envelope := Envelope(a.Clock, "kcli.seller/v1", requestID, "listing", data)
		envelope.ObservedAt = observedAt
		envelope.Completeness = domain.CompletenessDirect
		envelope.Warnings = sellerRefreshWarnings(detail.Listing.ID)
		return domain.SellerOutputV1{Envelope: envelope}, nil
	}
	id, profileLink, err := sellerReferenceID(input.IDOrURL)
	if err != nil {
		return domain.SellerOutputV1{}, err
	}
	snapshot, err := a.State.SellerByID(ctx, id)
	if err == sql.ErrNoRows {
		return domain.SellerOutputV1{}, sellerLocalMiss(id)
	}
	if err != nil {
		return domain.SellerOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "read local seller index", Cause: err}
	}
	source := "local-index"
	if profileLink {
		source = "profile-link"
	}
	data := domain.SellerV1{ID: snapshot.ID, Name: snapshot.DisplayName, Source: source, Completeness: string(domain.CompletenessBestEffort)}
	envelope := Envelope(a.Clock, "kcli.seller/v1", requestID, source, data)
	envelope.ObservedAt = snapshot.ObservedAt
	envelope.Completeness = domain.CompletenessBestEffort
	envelope.Warnings = sellerRefreshWarnings("")
	return domain.SellerOutputV1{Envelope: envelope}, nil
}

func (a *App) SellerSearch(ctx context.Context, input domain.SellerSearchInputV1, requestID string) (domain.SellerListOutputV1, error) {
	if a == nil || a.State == nil || a.Clock == nil {
		return domain.SellerListOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "seller runtime dependencies are unavailable"}
	}
	match := input.Match
	if match == "" {
		match = "contains"
	}
	folded := sellerFoldName(input.Name)
	if folded == "" || len(input.Name) > 256 || !utf8.ValidString(input.Name) || strings.IndexFunc(input.Name, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 || (match != "exact" && match != "contains") {
		return domain.SellerListOutputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "invalid seller name search"}
	}
	snapshots, info, err := a.State.SearchSellers(ctx, folded, match, 25)
	if err != nil {
		return domain.SellerListOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "search local seller index", Cause: err}
	}
	items := make([]domain.SellerV1, 0, len(snapshots))
	for _, snapshot := range snapshots {
		items = append(items, domain.SellerV1{ID: snapshot.ID, Name: snapshot.DisplayName, Source: "local-index", Completeness: string(domain.CompletenessBestEffort)})
	}
	envelope := Envelope(a.Clock, "kcli.sellers/v1", requestID, "local-index", items)
	envelope.Completeness = domain.CompletenessBestEffort
	if !info.NewestObservedAt.IsZero() {
		envelope.ObservedAt = info.NewestObservedAt
	}
	envelope.Warnings = []domain.WarningV1{sellerIndexWarning(info), sellerRefreshWarnings("")[0]}
	return domain.SellerListOutputV1{Envelope: envelope}, nil
}

func (a *App) SellerListings(ctx context.Context, input domain.SellerListingsInputV1, requestID string) (domain.SellerListingsOutputV1, error) {
	if a == nil || a.State == nil || a.Clock == nil {
		return domain.SellerListingsOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "seller runtime dependencies are unavailable"}
	}
	if input.Limit == 0 {
		input.Limit = 25
	}
	if input.Limit < 1 || input.Limit > 1000 {
		return domain.SellerListingsOutputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "seller listings limit must be between 1 and 1000"}
	}
	id, _, err := sellerReferenceID(input.IDOrURL)
	if err != nil {
		return domain.SellerListingsOutputV1{}, err
	}
	seller, err := a.State.SellerByID(ctx, id)
	if err == sql.ErrNoRows {
		return domain.SellerListingsOutputV1{}, sellerLocalMiss(id)
	}
	if err != nil {
		return domain.SellerListingsOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "read local seller index", Cause: err}
	}
	known, err := a.State.SellerListings(ctx, id, input.Limit)
	if err != nil {
		return domain.SellerListingsOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "read locally linked seller listings", Cause: err}
	}
	items := make([]domain.ListingSummaryV1, 0, len(known))
	for _, item := range known {
		items = append(items, domain.ListingSummaryV1{ID: item.ListingID, Title: item.Title, URL: item.URL})
	}
	info, err := a.State.SellerIndexInfo(ctx)
	if err != nil {
		return domain.SellerListingsOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "read seller index horizon", Cause: err}
	}
	envelope := Envelope(a.Clock, "kcli.seller-listings/v1", requestID, "local-index", items)
	envelope.ObservedAt = seller.ObservedAt
	envelope.Completeness = domain.CompletenessKnownOnly
	envelope.Warnings = []domain.WarningV1{sellerIndexWarning(info), {Code: "known_only", Message: "results include only listings encountered by this local profile", Details: map[string]any{"seller_id": id}}}
	return domain.SellerListingsOutputV1{Envelope: envelope}, nil
}

func sellerReferenceID(reference string) (string, bool, error) {
	if reference == "" || len(reference) > 1024 || !utf8.ValidString(reference) || strings.IndexFunc(reference, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", false, &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "invalid seller reference"}
	}
	if sellerIDPattern.MatchString(reference) {
		return reference, false, nil
	}
	lower := strings.ToLower(reference)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%2e") || strings.Contains(reference, `\`) {
		return "", false, &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "invalid seller URL"}
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" || parsed.Port() != "" {
		return "", false, &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "invalid seller URL"}
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil || strings.IndexFunc(decodedPath, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", false, &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "seller URL path contains encoded control characters"}
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "api.kleinanzeigen.de" && parsed.RawQuery == "" {
		if match := sellerAPIPathPattern.FindStringSubmatch(parsed.EscapedPath()); len(match) == 2 {
			return match[1], true, nil
		}
	}
	if host != "kleinanzeigen.de" && host != "www.kleinanzeigen.de" {
		return "", false, &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "seller URL host is not allowed"}
	}
	if match := sellerProviderPathPattern.FindStringSubmatch(parsed.EscapedPath()); len(match) == 2 && parsed.RawQuery == "" {
		return match[1], true, nil
	}
	if parsed.EscapedPath() == "/s-bestandsliste.html" {
		query, err := url.ParseQuery(parsed.RawQuery)
		if err == nil && len(query) == 1 && len(query["userId"]) == 1 && sellerIDPattern.MatchString(query.Get("userId")) {
			return query.Get("userId"), true, nil
		}
	}
	return "", false, &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "seller URL path is not a recognized profile or company form"}
}

func sellerFoldName(value string) string {
	return cases.Fold().String(norm.NFKC.String(strings.TrimSpace(value)))
}

func sellerLocalMiss(id string) error {
	return &domain.Error{Code: domain.CodeNotFound, Message: "seller is not in the local index", Details: map[string]any{"seller_id": id, "reason": "not_in_local_index", "scope": "local-index"}}
}

func sellerRefreshWarnings(listingID string) []domain.WarningV1 {
	command := "kcli seller get --listing LISTING_ID_OR_URL"
	if listingID != "" {
		command = fmt.Sprintf("kcli seller get --listing %s", listingID)
	}
	return []domain.WarningV1{{Code: "seller_refresh_source", Message: "seller data can be refreshed from a listing detail", Details: map[string]any{"command": command}}}
}

func sellerIndexWarning(info state.SellerIndexInfo) domain.WarningV1 {
	details := map[string]any{"index_size": info.Size, "retention_days": 30, "maximum_sellers": 10000}
	if !info.OldestObservedAt.IsZero() {
		details["index_horizon"] = info.OldestObservedAt.UTC().Format(time.RFC3339Nano)
	}
	if !info.NewestObservedAt.IsZero() {
		details["last_observation"] = info.NewestObservedAt.UTC().Format(time.RFC3339Nano)
	}
	return domain.WarningV1{Code: "local_seller_index_scope", Message: "results cover only sellers encountered by this local profile", Details: details}
}
