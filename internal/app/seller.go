package app

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
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
		data := detail.Seller
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
		if remote, ok := a.Transport.(interface {
			PublicSeller(context.Context, string) (domain.SellerV1, error)
		}); ok {
			data, err := remote.PublicSeller(ctx, id)
			if err != nil {
				return domain.SellerOutputV1{}, err
			}
			data.ObservedAt = a.Clock.Now().UTC()
			public, err := json.Marshal(data.Public)
			if err != nil {
				return domain.SellerOutputV1{}, err
			}
			if err := a.State.UpsertPublicSeller(ctx, state.SellerSnapshot{ID: data.ID, FoldedName: sellerFoldName(data.Name), DisplayName: data.Name, PublicJSON: public, Source: "public-web", Completeness: string(domain.CompletenessBestEffort), ObservedAt: data.ObservedAt}); err != nil {
				return domain.SellerOutputV1{}, err
			}
			envelope := Envelope(a.Clock, "kcli.seller/v1", requestID, "public-web", data)
			envelope.Completeness = domain.CompletenessBestEffort
			return domain.SellerOutputV1{Envelope: envelope}, nil
		}
		return domain.SellerOutputV1{}, sellerLocalMiss(id)
	}
	if err != nil {
		return domain.SellerOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "read local seller index", Cause: err}
	}
	source := "local-index"
	if profileLink {
		source = "profile-link"
	}
	data, err := sellerSnapshotData(snapshot, source, domain.CompletenessBestEffort)
	if err != nil {
		return domain.SellerOutputV1{}, err
	}
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
		item, decodeErr := sellerSnapshotData(snapshot, "local-index", domain.CompletenessBestEffort)
		if decodeErr != nil {
			return domain.SellerListOutputV1{}, decodeErr
		}
		items = append(items, item)
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
	if remote, ok := a.Transport.(interface {
		PublicSellerListings(context.Context, string) (kleinanzeigen.SearchPage, error)
	}); ok {
		page, err := remote.PublicSellerListings(ctx, id)
		if err != nil {
			return domain.SellerListingsOutputV1{}, err
		}
		now := a.Clock.Now().UTC()
		if err := a.State.SearchUpsertSellers(ctx, searchSellerRecords(page.Listings, now)); err != nil {
			return domain.SellerListingsOutputV1{}, err
		}
		items := make([]domain.ListingSummaryV1, 0, len(page.Listings))
		for _, listing := range page.Listings {
			if len(items) >= input.Limit {
				break
			}
			item := listing.Summary
			item.ObservedAt = now
			items = append(items, item)
		}
		envelope := Envelope(a.Clock, "kcli.seller-listings/v1", requestID, "public-web", items)
		envelope.Completeness = domain.CompletenessBestEffort
		envelope.Warnings = append(page.Warnings, domain.WarningV1{Code: "bounded_inventory", Message: "results cover one public seller inventory page, not an exhaustive inventory"})
		return domain.SellerListingsOutputV1{Envelope: envelope}, nil
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
		items = append(items, domain.ListingSummaryV1{
			ID:           item.ListingID,
			Title:        item.Title,
			URL:          item.URL,
			Status:       item.Status,
			Source:       "local-index",
			Completeness: domain.CompletenessKnownOnly,
			ObservedAt:   item.ObservedAt,
		})
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

func sellerSnapshotData(snapshot state.SellerSnapshot, source string, completeness domain.Completeness) (domain.SellerV1, error) {
	decoder := json.NewDecoder(bytes.NewReader(snapshot.PublicJSON))
	decoder.UseNumber()
	public := make(map[string]any)
	if err := decoder.Decode(&public); err != nil {
		return domain.SellerV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "decode public seller snapshot", Cause: err}
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return domain.SellerV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "public seller snapshot contains trailing data"}
	}
	data := domain.SellerV1{
		ID:           snapshot.ID,
		Name:         snapshot.DisplayName,
		Badges:       sellerPublicStrings(public["userBadges"]),
		Public:       public,
		Source:       source,
		Completeness: completeness,
		ObservedAt:   snapshot.ObservedAt,
	}
	data.ContactInitials = sellerPublicString(public, "contact-name-initials")
	data.ProfileURL = sellerPublicString(public, "profile-url")
	data.AccountType = sellerPublicString(public, "seller-account-type")
	data.AccountSince = sellerPublicString(public, "user-since-date-time")
	data.AccountAge = sellerPublicString(public, "account-age")
	data.PosterType = sellerPublicString(public, "poster-type")
	if value, ok := public["user-rating"]; ok {
		rating := &domain.SellerRatingV1{}
		if object, isObject := value.(map[string]any); isObject {
			rating.Public = object
			rating.Score = sellerPublicFirstString(object, "score", "average", "value")
			rating.Count = sellerPublicInt64(object["count"])
		} else {
			rating.Score, _ = kleinanzeigen.StringValue(value)
		}
		data.Rating = rating
	}
	companyName := sellerPublicString(public, "company-name")
	companyValue, hasCompany := public["company"]
	companyDetails, hasDetails := public["company-details"]
	if companyName != "" || hasCompany || hasDetails {
		company := &domain.SellerCompanyV1{Name: companyName}
		if object, isObject := companyValue.(map[string]any); isObject {
			company.Details = sellerCopyPublic(object)
			if company.Name == "" {
				company.Name = sellerPublicFirstString(object, "name", "company-name")
			}
		} else if hasCompany {
			company.Details = map[string]any{"value": companyValue}
		}
		if object, isObject := companyDetails.(map[string]any); isObject {
			if company.Details == nil {
				company.Details = make(map[string]any)
			}
			for key, value := range object {
				company.Details[key] = value
			}
		} else if hasDetails {
			if company.Details == nil {
				company.Details = make(map[string]any)
			}
			company.Details["details"] = companyDetails
		}
		data.Company = company
	}
	return data, nil
}

func sellerPublicString(public map[string]any, name string) string {
	value, ok := public[name]
	if !ok {
		return ""
	}
	text, _ := kleinanzeigen.StringValue(value)
	return text
}

func sellerPublicFirstString(public map[string]any, names ...string) string {
	for _, name := range names {
		if value := sellerPublicString(public, name); value != "" {
			return value
		}
	}
	return ""
}

func sellerPublicStrings(value any) []string {
	if value == nil {
		return []string{}
	}
	items, ok := value.([]any)
	if !ok {
		items = []any{value}
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if text, valid := kleinanzeigen.StringValue(item); valid {
			result = append(result, text)
		}
	}
	return result
}

func sellerPublicInt64(value any) *int64 {
	text, ok := kleinanzeigen.StringValue(value)
	if !ok {
		return nil
	}
	integer, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil
	}
	return &integer
}

func sellerCopyPublic(public map[string]any) map[string]any {
	copy := make(map[string]any, len(public))
	for key, value := range public {
		copy[key] = value
	}
	return copy
}

// ValidateSellerReference shares the exact ID/profile grammar with CLI parsing.
func ValidateSellerReference(reference string) error {
	_, _, err := sellerReferenceID(reference)
	return err
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
