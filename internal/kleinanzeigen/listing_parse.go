package kleinanzeigen

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/johannhipp/kcli/internal/domain"
)

func ListingFetch(ctx context.Context, transport Transport, reference string) (ListingDetail, error) {
	if transport == nil {
		return ListingDetail{}, &domain.Error{Code: domain.CodeUnavailable, Message: "listing transport is unavailable"}
	}
	id, err := ListingReferenceID(reference)
	if err != nil {
		return ListingDetail{}, err
	}
	response, err := transport.Do(Request{Context: ctx, Host: HostMain, Method: http.MethodGet, Path: "/api/ads/" + id + ".json", MaxResponseBytes: JSONResponseLimit, Class: VolatileRead})
	if err != nil {
		return ListingDetail{}, err
	}
	if response.StatusCode == http.StatusNotFound {
		return ListingDetail{}, &domain.Error{Code: domain.CodeUnavailable, Message: "listing is unavailable", Details: map[string]any{"listing_id": id, "availability": "unavailable", "status": response.StatusCode}}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return ListingDetail{}, ResponseError(response)
	}
	detail, err := ListingParse(response.Body)
	if err != nil {
		return ListingDetail{}, err
	}
	if detail.Listing.ID == "" {
		detail.Listing.ID = id
		detail.Normalized["id"] = id
	}
	if detail.Listing.ID != id {
		return ListingDetail{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "listing detail ID did not match the requested listing", Details: map[string]any{"requested_id": id, "returned_id": detail.Listing.ID}}
	}
	return detail, nil
}

func ListingParse(raw []byte) (ListingDetail, error) {
	decoded, err := DecodeJSON(raw)
	if err != nil {
		return ListingDetail{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "decode listing detail", Cause: err}
	}
	original := decoded
	decoded = HTMLUnescapeDocumentedFields(UnwrapValues(decoded))
	ad, ok := listingAdObject(decoded)
	if !ok {
		return ListingDetail{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "listing detail did not contain an ad object"}
	}
	detail := ListingDetail{
		Normalized: make(map[string]any),
		Warnings:   []domain.WarningV1{},
		Listing: domain.ListingV1{
			Labels:           []string{},
			ContractWarnings: []any{},
			Attributes:       []domain.ListingAttributeV1{},
			Media:            []domain.ListingMediaV1{},
		},
	}
	detail.Listing.ID, _ = listingFieldString(ad, "id")
	detail.Listing.Title, _ = listingFieldString(ad, "title")
	detail.Listing.Description, _ = listingFieldString(ad, "description")
	if detail.Listing.ID == "" || detail.Listing.Title == "" {
		return ListingDetail{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "listing detail omitted its required ID or title"}
	}

	for _, name := range []string{"id", "title", "description", "category", "ad-type", "listing-type", "poster-type", "status", "labels", "start-date-time", "end-date-time", "edit-date-time", "edited-date-time", "last-update-date-time", "view-count", "contract-warnings", "ad-address", "distance", "shipping", "pickup", "attributes", "pictures", "contact-name", "contact-name-initials", "user-id", "seller-account-type", "user-since-date-time", "account-age", "user-rating", "userBadges", "company-name", "company-details"} {
		if value, found := listingField(ad, name); found {
			detail.Normalized[name] = value
		}
	}
	detail.Normalized["id"] = detail.Listing.ID
	detail.Normalized["title"] = detail.Listing.Title
	if detail.Listing.Description != "" {
		detail.Normalized["description"] = detail.Listing.Description
	}

	detail.Listing.Category = listingCategory(ad)
	detail.Listing.AdType, _ = listingFieldString(ad, "ad-type")
	detail.Listing.ListingType, _ = listingFieldString(ad, "listing-type")
	detail.Listing.Status, _ = listingFieldString(ad, "status")
	detail.Listing.Labels = listingStringSliceField(ad, "labels")
	detail.Listing.PostedAt = listingFirstFieldString(ad, "start-date-time", "posted-date-time")
	detail.Listing.EditedAt = listingFirstFieldString(ad, "edit-date-time", "edited-date-time", "last-update-date-time", "modification-date-time", "updated-date-time")
	detail.Listing.EndsAt, _ = listingFieldString(ad, "end-date-time")
	detail.Listing.ViewCount = listingInt64Field(ad, "view-count")
	detail.Listing.ContractWarnings = listingAnySliceField(ad, "contract-warnings")

	if price, found := listingField(ad, "price"); found {
		detail.Normalized["price"] = price
		if priceObject, isObject := price.(map[string]any); isObject {
			detail.Listing.Amount, _ = listingFieldString(priceObject, "amount")
			detail.Listing.PriceType, _ = listingFieldString(priceObject, "price-type")
			if cents, exact := listingExactCents(detail.Listing.Amount); exact {
				detail.Listing.AmountCents = &cents
			}
		}
	}
	detail.Listing.Location = listingLocation(ad)
	detail.Listing.Pickup = listingBoolField(ad, "pickup")
	detail.Listing.Shipping = listingShipping(ad)
	detail.Listing.Attributes = listingAttributes(ad, &detail.Warnings)
	detail.Listing.PosterType, _ = listingFieldString(ad, "poster-type")

	links := listingLinks(ad)
	for _, link := range links {
		relation, _ := listingFieldString(link, "rel")
		href, _ := listingLinkURL(link)
		if relation == "self-public-website" {
			if _, err := ListingReferenceID(href); err == nil {
				detail.Listing.URL = href
				detail.Normalized["url"] = href
			} else {
				detail.Warnings = append(detail.Warnings, ContractWarning("link.self-public-website", href))
			}
		}
	}
	detail.Listing.Availability = listingAvailability(detail.Listing.Status)
	detail.Normalized["availability"] = detail.Listing.Availability
	detail.Media = listingPictures(ad, &detail.Warnings)
	detail.Listing.Media = append([]domain.ListingMediaV1(nil), detail.Media...)
	detail.Normalized["media"] = listingMediaMaps(detail.Media)
	detail.Seller = listingSeller(ad, links)
	detail.Listing.Seller = detail.Seller
	detail.Normalized["seller"] = detail.Seller.Public
	detail.Listing.AdditionalFields = listingAdditionalFields(ad)

	redacted := listingRedact(original)
	redactedRaw, err := json.Marshal(redacted)
	if err != nil {
		return ListingDetail{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "encode redacted listing evidence", Cause: err}
	}
	detail.Raw = redactedRaw
	return detail, nil
}

func ListingAllowMediaURL(transport Transport, raw string) error {
	allower, ok := transport.(interface{ AllowMediaURL(string) error })
	if !ok {
		return nil
	}
	return allower.AllowMediaURL(raw)
}

func (t *MobileTransport) AllowMediaURL(raw string) error {
	allower, ok := t.base.(interface{ AllowMediaURL(string) error })
	if !ok {
		return fmt.Errorf("underlying transport cannot allow media URLs")
	}
	return allower.AllowMediaURL(raw)
}

func listingAdObject(value any) (map[string]any, bool) {
	if object, ok := value.(map[string]any); ok {
		if _, hasID := listingField(object, "id"); hasID {
			if _, hasTitle := listingField(object, "title"); hasTitle {
				return object, true
			}
		}
		for key, child := range object {
			if listingLocalName(key) == "ad" {
				if ad, ok := child.(map[string]any); ok {
					return ad, true
				}
			}
		}
		for _, child := range object {
			if ad, ok := listingAdObject(child); ok {
				return ad, true
			}
		}
	}
	if array, ok := value.([]any); ok {
		for _, child := range array {
			if ad, found := listingAdObject(child); found {
				return ad, true
			}
		}
	}
	return nil, false
}

func listingField(object map[string]any, name string) (any, bool) {
	for key, value := range object {
		if listingLocalName(key) == name {
			return value, true
		}
	}
	return nil, false
}

func listingLocalName(key string) string {
	if index := strings.LastIndex(key, "}"); index >= 0 {
		return key[index+1:]
	}
	if index := strings.LastIndex(key, "/"); index >= 0 {
		return key[index+1:]
	}
	return key
}

func listingFieldString(object map[string]any, name string) (string, bool) {
	value, ok := listingField(object, name)
	if !ok {
		return "", false
	}
	return StringValue(value)
}

func listingLinks(object map[string]any) []map[string]any {
	value, ok := listingField(object, "link")
	if !ok {
		return nil
	}
	items, err := NormalizeSingleton(value)
	if err != nil {
		return nil
	}
	links := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if link, ok := item.(map[string]any); ok {
			links = append(links, link)
		}
	}
	return links
}

func listingLinkURL(link map[string]any) (string, bool) {
	for _, name := range []string{"href", "url", "value"} {
		if value, ok := listingFieldString(link, name); ok && value != "" {
			return value, true
		}
	}
	return "", false
}

func listingFirstFieldString(object map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := listingFieldString(object, name); ok && value != "" {
			return value
		}
	}
	return ""
}

func listingStringValues(value any) []string {
	items, ok := value.([]any)
	if !ok {
		items = []any{value}
	}
	values := make([]string, 0, len(items))
	for _, item := range items {
		if text, valid := StringValue(item); valid {
			values = append(values, text)
		}
	}
	return values
}

func listingStringSliceField(object map[string]any, name string) []string {
	value, ok := listingField(object, name)
	if !ok {
		return []string{}
	}
	return listingStringValues(value)
}

func listingAnySliceField(object map[string]any, name string) []any {
	value, ok := listingField(object, name)
	if !ok || value == nil {
		return []any{}
	}
	if items, isArray := value.([]any); isArray {
		return append([]any(nil), items...)
	}
	return []any{value}
}

func listingInt64Field(object map[string]any, name string) *int64 {
	value, ok := listingField(object, name)
	if !ok {
		return nil
	}
	text, ok := StringValue(value)
	if !ok {
		return nil
	}
	integer, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return nil
	}
	return &integer
}

func listingBoolValue(value any) (*bool, bool) {
	switch current := value.(type) {
	case bool:
		result := current
		return &result, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(current))
		if err != nil {
			return nil, false
		}
		return &parsed, true
	default:
		return nil, false
	}
}

func listingBoolField(object map[string]any, name string) *bool {
	value, ok := listingField(object, name)
	if !ok {
		return nil
	}
	result, _ := listingBoolValue(value)
	return result
}

func listingPublicMap(value map[string]any) map[string]any {
	redacted, ok := listingRedact(value).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return redacted
}

func listingCategory(ad map[string]any) *domain.ListingCategoryV1 {
	value, ok := listingField(ad, "category")
	if !ok {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	category := &domain.ListingCategoryV1{Public: listingPublicMap(object)}
	category.ID, _ = listingFieldString(object, "id")
	category.Label = listingFirstFieldString(object, "localized-name", "label", "name")
	category.Path = listingFirstFieldString(object, "localized-path", "path")
	if category.Path == "" {
		parent := listingFirstFieldString(object, "parent-name", "parent-label")
		switch {
		case parent != "" && category.Label != "":
			category.Path = parent + "/" + category.Label
		case category.Label != "":
			category.Path = category.Label
		}
	}
	return category
}

func listingLocation(ad map[string]any) *domain.ListingLocationV1 {
	value, ok := listingField(ad, "ad-address")
	if !ok {
		return nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	location := &domain.ListingLocationV1{
		Label:       listingFirstFieldString(object, "localized-label", "state", "city", "locality"),
		Postcode:    listingFirstFieldString(object, "zip-code", "postcode"),
		Latitude:    listingFirstFieldString(object, "latitude"),
		Longitude:   listingFirstFieldString(object, "longitude"),
		Approximate: true,
		Public:      listingPublicMap(object),
	}
	location.Distance, _ = listingFieldString(ad, "distance")
	return location
}

func listingShipping(ad map[string]any) *domain.ListingShippingV1 {
	value, ok := listingField(ad, "shipping")
	if !ok {
		return nil
	}
	if available, valid := listingBoolValue(value); valid {
		return &domain.ListingShippingV1{Available: available}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return &domain.ListingShippingV1{Public: map[string]any{"value": listingRedact(value)}}
	}
	shipping := &domain.ListingShippingV1{Public: listingPublicMap(object)}
	shipping.Available = listingBoolField(object, "available")
	shipping.Cost = listingFirstFieldString(object, "cost", "price")
	return shipping
}

func listingAttributeValues(value any) []any {
	if items, ok := value.([]any); ok {
		values := make([]any, len(items))
		for index, item := range items {
			values[index] = listingRedact(item)
		}
		return values
	}
	return []any{listingRedact(value)}
}

func listingAttributes(ad map[string]any, warnings *[]domain.WarningV1) []domain.ListingAttributeV1 {
	container, ok := listingField(ad, "attributes")
	if !ok {
		return []domain.ListingAttributeV1{}
	}
	object, ok := container.(map[string]any)
	if !ok {
		*warnings = append(*warnings, ContractWarning("attributes", container))
		return []domain.ListingAttributeV1{{Values: []any{}, Public: map[string]any{"value": listingRedact(container)}}}
	}
	value, ok := listingField(object, "attribute")
	if !ok {
		return []domain.ListingAttributeV1{}
	}
	items, err := NormalizeSingleton(value)
	if err != nil {
		*warnings = append(*warnings, ContractWarning("attributes.attribute", value))
		return []domain.ListingAttributeV1{{Values: []any{}, Public: map[string]any{"value": listingRedact(value)}}}
	}
	attributes := make([]domain.ListingAttributeV1, 0, len(items))
	for _, item := range items {
		attributeObject, valid := item.(map[string]any)
		if !valid {
			*warnings = append(*warnings, ContractWarning("attributes.attribute", item))
			values := listingAttributeValues(item)
			attribute := domain.ListingAttributeV1{Values: values, Public: map[string]any{"value": listingRedact(item)}}
			if len(values) > 0 {
				attribute.Value = values[0]
			}
			attributes = append(attributes, attribute)
			continue
		}
		attribute := domain.ListingAttributeV1{Public: listingPublicMap(attributeObject), Values: []any{}}
		attribute.Name, _ = listingFieldString(attributeObject, "name")
		attribute.Label = listingFirstFieldString(attributeObject, "localized-label", "label", "name")
		if attributeValue, found := listingField(attributeObject, "value"); found {
			attribute.Values = listingAttributeValues(attributeValue)
			if len(attribute.Values) > 0 {
				attribute.Value = attribute.Values[0]
			}
		}
		attributes = append(attributes, attribute)
	}
	return attributes
}

func listingAdditionalFields(ad map[string]any) map[string]any {
	known := map[string]bool{
		"id": true, "title": true, "description": true, "category": true, "price": true,
		"ad-type": true, "listing-type": true, "poster-type": true, "status": true,
		"labels": true, "start-date-time": true, "posted-date-time": true,
		"end-date-time": true, "edit-date-time": true, "edited-date-time": true,
		"last-update-date-time": true, "modification-date-time": true, "updated-date-time": true,
		"view-count": true, "contract-warnings": true, "ad-address": true, "distance": true,
		"shipping": true, "pickup": true, "attributes": true, "pictures": true, "link": true,
		"contact-name": true, "contact-name-initials": true, "user-id": true,
		"seller-account-type": true, "user-since-date-time": true, "account-age": true,
		"user-rating": true, "userBadges": true, "company-name": true, "company-details": true,
	}
	additional := make(map[string]any)
	for key, value := range ad {
		local := listingLocalName(key)
		if known[local] || listingSensitiveKey(local) {
			continue
		}
		additional[local] = listingRedact(value)
	}
	if len(additional) == 0 {
		return nil
	}
	return additional
}

func listingPictures(ad map[string]any, warnings *[]domain.WarningV1) []ListingMedia {
	picturesValue, ok := listingField(ad, "pictures")
	if !ok {
		return []ListingMedia{}
	}
	picturesObject, ok := picturesValue.(map[string]any)
	if !ok {
		*warnings = append(*warnings, ContractWarning("pictures", picturesValue))
		return []ListingMedia{}
	}
	pictureValue, ok := listingField(picturesObject, "picture")
	if !ok {
		return []ListingMedia{}
	}
	pictures, err := NormalizeSingleton(pictureValue)
	if err != nil {
		*warnings = append(*warnings, ContractWarning("pictures.picture", pictureValue))
		return []ListingMedia{}
	}
	media := make([]ListingMedia, 0)
	for index, item := range pictures {
		picture, ok := item.(map[string]any)
		if !ok {
			*warnings = append(*warnings, ContractWarning("pictures.picture", item))
			continue
		}
		width := listingIntegerField(picture, "width")
		height := listingIntegerField(picture, "height")
		size, _ := listingFieldString(picture, "size")
		for _, link := range listingLinks(picture) {
			relation, _ := listingFieldString(link, "rel")
			rawURL, _ := listingLinkURL(link)
			linkWidth := listingIntegerField(link, "width")
			if linkWidth == 0 {
				linkWidth = width
			}
			linkHeight := listingIntegerField(link, "height")
			if linkHeight == 0 {
				linkHeight = height
			}
			linkSize, _ := listingFieldString(link, "size")
			if linkSize == "" {
				linkSize = size
			}
			parsed, parseErr := url.Parse(rawURL)
			if relation == "" || parseErr != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" {
				*warnings = append(*warnings, ContractWarning("pictures.picture.link", link))
				continue
			}
			media = append(media, ListingMedia{Index: index, Relation: relation, URL: rawURL, Width: linkWidth, Height: linkHeight, SizeLabel: linkSize})
		}
	}
	return media
}

func listingIntegerField(object map[string]any, name string) int {
	value, ok := listingField(object, name)
	if !ok {
		return 0
	}
	text, ok := StringValue(value)
	if !ok {
		return 0
	}
	integer, _ := strconv.Atoi(text)
	return integer
}

func listingSeller(ad map[string]any, links []map[string]any) ListingSeller {
	seller := ListingSeller{
		Badges:       []string{},
		Public:       make(map[string]any),
		Source:       "listing",
		Completeness: domain.CompletenessDirect,
	}
	seller.ID, _ = listingFieldString(ad, "user-id")
	seller.Name, _ = listingFieldString(ad, "contact-name")
	seller.ContactInitials, _ = listingFieldString(ad, "contact-name-initials")
	seller.AccountType, _ = listingFieldString(ad, "seller-account-type")
	seller.AccountSince, _ = listingFieldString(ad, "user-since-date-time")
	seller.AccountAge, _ = listingFieldString(ad, "account-age")
	seller.PosterType, _ = listingFieldString(ad, "poster-type")
	seller.Badges = listingStringSliceField(ad, "userBadges")
	for _, field := range []string{"user-id", "contact-name", "contact-name-initials", "seller-account-type", "poster-type", "user-since-date-time", "account-age", "user-rating", "userBadges", "company", "company-name", "company-details"} {
		if value, ok := listingField(ad, field); ok {
			seller.Public[field] = listingRedact(value)
		}
	}
	if value, ok := listingField(ad, "user-rating"); ok {
		rating := &domain.SellerRatingV1{}
		if object, isObject := value.(map[string]any); isObject {
			rating.Public = listingPublicMap(object)
			rating.Score = listingFirstFieldString(object, "score", "average", "value")
			rating.Count = listingInt64Field(object, "count")
		} else {
			rating.Score, _ = StringValue(value)
		}
		seller.Rating = rating
	}
	companyName, hasCompanyName := listingFieldString(ad, "company-name")
	companyValue, hasCompany := listingField(ad, "company")
	companyDetails, hasCompanyDetails := listingField(ad, "company-details")
	if hasCompanyName || hasCompany || hasCompanyDetails {
		company := &domain.SellerCompanyV1{Name: companyName}
		switch current := companyValue.(type) {
		case map[string]any:
			company.Details = listingPublicMap(current)
			if company.Name == "" {
				company.Name = listingFirstFieldString(current, "name", "company-name")
			}
		case nil:
		default:
			company.Details = map[string]any{"value": listingRedact(current)}
		}
		if details, isObject := companyDetails.(map[string]any); isObject {
			if company.Details == nil {
				company.Details = make(map[string]any)
			}
			for key, value := range listingPublicMap(details) {
				company.Details[key] = value
			}
		} else if hasCompanyDetails {
			if company.Details == nil {
				company.Details = make(map[string]any)
			}
			company.Details["details"] = listingRedact(companyDetails)
		}
		seller.Company = company
	}
	for _, link := range links {
		relation, _ := listingFieldString(link, "rel")
		if relation == "self-user" {
			seller.ProfileURL, _ = listingLinkURL(link)
			if seller.ProfileURL != "" {
				seller.Public["profile-url"] = seller.ProfileURL
			}
		}
	}
	return seller
}

func listingAvailability(status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch {
	case normalized == "", normalized == "unknown":
		return "unknown"
	case strings.Contains(normalized, "reserved") || strings.Contains(normalized, "reserviert"):
		return "reserved"
	case strings.Contains(normalized, "expired") || strings.Contains(normalized, "abgelaufen"):
		return "expired"
	case strings.Contains(normalized, "deleted") || strings.Contains(normalized, "removed") || strings.Contains(normalized, "gelöscht"):
		return "deleted"
	case strings.Contains(normalized, "active") || strings.Contains(normalized, "available") || strings.Contains(normalized, "verfügbar"):
		return "available"
	default:
		return normalized
	}
}

func listingExactCents(amount string) (int64, bool) {
	value := strings.TrimSpace(amount)
	if strings.Count(value, ",")+strings.Count(value, ".") > 1 {
		return 0, false
	}
	value = strings.Replace(value, ",", ".", 1)
	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" || len(parts) == 2 && (len(parts[1]) < 1 || len(parts[1]) > 2) {
		return 0, false
	}
	for _, part := range parts {
		if part == "" || strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return 0, false
		}
	}
	euros, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || euros > (int64(^uint64(0)>>1)-99)/100 {
		return 0, false
	}
	fraction := int64(0)
	if len(parts) == 2 {
		fraction, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, false
		}
		if len(parts[1]) == 1 {
			fraction *= 10
		}
	}
	return euros*100 + fraction, true
}
