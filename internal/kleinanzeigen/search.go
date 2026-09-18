package kleinanzeigen

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

// SearchSeller is the public seller identity attached to a search result.
type SearchSeller struct {
	ID   string
	Name string
	Raw  json.RawMessage
}

// SearchListing is the normalized subset needed by search output, exclusions,
// and the local seller index.
type SearchListing struct {
	Summary     domain.ListingSummaryV1
	Description string
	Status      string
	Seller      SearchSeller
}

// SearchAds performs and decodes one bounded anonymous mobile search request.
func SearchAds(ctx context.Context, transport Transport, query map[string][]string) (SearchPage, error) {
	if transport == nil {
		return SearchPage{}, &domain.Error{Code: domain.CodeUnavailable, Message: "search transport is unavailable"}
	}
	requestContext, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	response, err := transport.Do(getJSONRequest(requestContext, "/api/ads.json", searchCloneQuery(query), StableRead, true))
	if err != nil {
		return SearchPage{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return SearchPage{}, ResponseError(response)
	}
	page, err := SearchParsePage(response.Body)
	for i := range page.Listings {
		page.Listings[i].Summary.Source = transportSource(transport)
	}
	return page, err
}

// SearchParsePage decodes object-or-array search results without retaining the
// full response body in public output.
func SearchParsePage(raw []byte) (SearchPage, error) {
	decoded, err := DecodeJSON(raw)
	if err != nil {
		return SearchPage{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "search response was not valid JSON", Cause: err}
	}
	decoded = HTMLUnescapeDocumentedFields(UnwrapValues(decoded))
	adsValue, ok := FindKey(decoded, "ads")
	if !ok {
		return SearchPage{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "search response did not contain ads"}
	}
	ads, ok := adsValue.(map[string]any)
	if !ok {
		return SearchPage{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "search ads had an unexpected shape"}
	}
	page := SearchPage{Listings: []SearchListing{}, Warnings: []domain.WarningV1{}}
	if pagingValue, exists := searchDirectField(ads, "paging"); exists {
		if paging, pagingOK := pagingValue.(map[string]any); pagingOK {
			if next, exists := paging["nextPage"]; exists {
				page.Continuation = &SearchContinuation{}
				if next != nil {
					number, ok := searchInt(next)
					if !ok || number < 0 {
						return SearchPage{}, webContract("search continuation has an invalid page number")
					}
					page.Continuation.Next = &number
				}
			}
			if totalValue, totalOK := searchDirectField(paging, "numFound"); totalOK {
				if total, parseOK := searchInt(totalValue); parseOK && total >= 0 {
					page.Total = total
					page.TotalKnown = true
				} else {
					page.Warnings = append(page.Warnings, ContractWarning("ads.paging.numFound", totalValue))
				}
			}
		}
	}
	collection, exists := searchDirectField(ads, "ad")
	if !exists || collection == nil {
		return page, nil
	}
	rows, err := NormalizeSingleton(collection)
	if err != nil {
		return SearchPage{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "search listing collection had an unexpected shape", Cause: err}
	}
	for _, row := range rows {
		object, rowOK := row.(map[string]any)
		if !rowOK {
			page.Warnings = append(page.Warnings, ContractWarning("ads.ad", row))
			continue
		}
		listing, parseErr := searchParseListing(object)
		if parseErr != nil {
			page.Warnings = append(page.Warnings, ContractWarning("ads.ad.identity", row))
			continue
		}
		page.Listings = append(page.Listings, listing)
	}
	return page, nil
}

func searchParseListing(object map[string]any) (SearchListing, error) {
	id, _ := searchStringField(object, "id")
	title, _ := searchStringField(object, "title")
	if id == "" || title == "" {
		return SearchListing{}, fmt.Errorf("listing identity is incomplete")
	}
	description, _ := searchStringField(object, "description")
	status, _ := searchFirstStringField(object, "status", "ad-status")
	if status == "" {
		status = "available"
	}
	amount := ""
	if priceValue, ok := searchDirectField(object, "price"); ok {
		if price, priceOK := priceValue.(map[string]any); priceOK {
			amount, _ = searchStringField(price, "amount")
		}
	}
	url := searchPublicURL(object)
	summary := domain.ListingSummaryV1{ID: id, Title: title, Price: amount, PriceCents: searchPriceCents(amount), URL: url, Status: status, Source: "mobile-api", Completeness: domain.CompletenessBestEffort}
	seller := searchParseSeller(object)
	return SearchListing{Summary: summary, Description: description, Status: status, Seller: seller}, nil
}

func searchParseSeller(object map[string]any) SearchSeller {
	id, _ := searchStringField(object, "user-id")
	name, _ := searchStringField(object, "contact-name")
	if id == "" {
		return SearchSeller{}
	}
	public := make(map[string]any)
	for _, key := range []string{"user-id", "contact-name", "contact-name-initials", "seller-account-type", "poster-type", "user-since-date-time", "account-age", "user-rating", "userBadges", "company", "company-name", "company-details"} {
		if value, ok := searchDirectField(object, key); ok {
			public[key] = value
		}
	}
	if profileURL := searchRelationURL(object, "self-user"); profileURL != "" {
		public["profile-url"] = profileURL
	}
	raw, _ := json.Marshal(public)
	return SearchSeller{ID: id, Name: name, Raw: raw}
}

func searchPublicURL(object map[string]any) string {
	return searchRelationURL(object, "self-public-website")
}

func searchRelationURL(object map[string]any, relation string) string {
	value, ok := searchDirectField(object, "link")
	if !ok {
		return ""
	}
	rows, err := NormalizeSingleton(value)
	if err != nil {
		return ""
	}
	for _, row := range rows {
		link, linkOK := row.(map[string]any)
		if !linkOK {
			continue
		}
		rel, _ := searchStringField(link, "rel")
		if rel != relation {
			continue
		}
		href, _ := searchFirstStringField(link, "href", "url")
		return href
	}
	return ""
}

func searchPriceCents(amount string) *int64 {
	if amount == "" || strings.HasPrefix(amount, "-") {
		return nil
	}
	parts := strings.Split(amount, ".")
	if len(parts) > 2 || len(parts[0]) == 0 {
		return nil
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || whole > (1<<63-1)/100 {
		return nil
	}
	fraction := int64(0)
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > 2 {
			return nil
		}
		fraction, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return nil
		}
		if len(parts[1]) == 1 {
			fraction *= 10
		}
	}
	cents := whole*100 + fraction
	return &cents
}

func searchDirectField(object map[string]any, name string) (any, bool) {
	for key, value := range object {
		if key == name || strings.HasSuffix(key, "}"+name) || strings.HasSuffix(key, "/"+name) {
			return value, true
		}
	}
	return nil, false
}

func searchStringField(object map[string]any, name string) (string, bool) {
	value, ok := searchDirectField(object, name)
	if !ok {
		return "", false
	}
	return StringValue(value)
}

func searchFirstStringField(object map[string]any, names ...string) (string, bool) {
	for _, name := range names {
		if value, ok := searchStringField(object, name); ok && value != "" {
			return value, true
		}
	}
	return "", false
}

func searchInt(value any) (int, bool) {
	text, ok := StringValue(value)
	if !ok {
		return 0, false
	}
	parsed, err := strconv.Atoi(text)
	return parsed, err == nil
}

func searchCloneQuery(query map[string][]string) map[string][]string {
	cloned := make(map[string][]string, len(query))
	for key, values := range query {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}
