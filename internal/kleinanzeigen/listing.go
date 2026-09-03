package kleinanzeigen

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/johannhipp/kcli/internal/domain"
)

var (
	listingIDPattern        = regexp.MustCompile(`^[0-9]+$`)
	listingURLSuffixPattern = regexp.MustCompile(`^/s-anzeige/[^/]+/([0-9]+)-[0-9]+-[0-9]+/?$`)
	listingSensitiveKeys    = map[string]bool{"authorization": true, "access_token": true, "refresh_token": true, "id_token": true, "email": true, "password": true, "client_secret": true, "message": true, "message_body": true, "authorization_code": true, "code_verifier": true, "oauth_state": true}
)

type ListingMedia struct {
	Index     int
	Relation  string
	URL       string
	Width     int
	Height    int
	SizeLabel string
}

type ListingSeller struct {
	ID         string
	Name       string
	ProfileURL string
	Public     map[string]any
}

type ListingDetail struct {
	Listing    domain.ListingV1
	Normalized map[string]any
	Media      []ListingMedia
	Seller     ListingSeller
	Raw        json.RawMessage
	Warnings   []domain.WarningV1
}

func ListingReferenceID(reference string) (string, error) {
	if reference == "" || len(reference) > 1024 || !utf8.ValidString(reference) || strings.IndexFunc(reference, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "invalid listing reference"}
	}
	if listingIDPattern.MatchString(reference) {
		return reference, nil
	}
	lower := strings.ToLower(reference)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%2e") || strings.Contains(reference, `\`) {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "invalid listing URL"}
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Port() != "" {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "invalid listing URL"}
	}
	decodedPath, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil || strings.IndexFunc(decodedPath, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "listing URL path contains encoded control characters"}
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "kleinanzeigen.de" && host != "www.kleinanzeigen.de" {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "listing URL host is not allowed"}
	}
	match := listingURLSuffixPattern.FindStringSubmatch(parsed.EscapedPath())
	if len(match) != 2 {
		return "", &domain.Error{Code: domain.CodeInvalidIdentifier, Message: "listing URL path is not a recognized public listing form"}
	}
	return match[1], nil
}

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
	detail := ListingDetail{Normalized: make(map[string]any), Warnings: []domain.WarningV1{}}
	detail.Listing.ID, _ = listingFieldString(ad, "id")
	detail.Listing.Title, _ = listingFieldString(ad, "title")
	detail.Listing.Description, _ = listingFieldString(ad, "description")
	if detail.Listing.ID == "" || detail.Listing.Title == "" {
		return ListingDetail{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "listing detail omitted its required ID or title"}
	}

	for _, name := range []string{"id", "title", "description", "category", "ad-type", "poster-type", "status", "labels", "start-date-time", "end-date-time", "view-count", "contract-warnings", "ad-address", "distance", "shipping", "pickup", "attributes", "pictures", "contact-name", "contact-name-initials", "user-id", "seller-account-type", "user-since-date-time", "user-rating", "userBadges", "company-name", "company-details"} {
		if value, found := listingField(ad, name); found {
			detail.Normalized[name] = value
		}
	}
	detail.Normalized["id"] = detail.Listing.ID
	detail.Normalized["title"] = detail.Listing.Title
	if detail.Listing.Description != "" {
		detail.Normalized["description"] = detail.Listing.Description
	}

	if price, found := listingField(ad, "price"); found {
		detail.Normalized["price"] = price
		if priceObject, isObject := price.(map[string]any); isObject {
			detail.Listing.Amount, _ = listingFieldString(priceObject, "amount")
			if cents, exact := listingExactCents(detail.Listing.Amount); exact {
				detail.Listing.AmountCents = &cents
			}
		}
	}
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
	status, _ := listingFieldString(ad, "status")
	detail.Listing.Availability = listingAvailability(status)
	detail.Normalized["availability"] = detail.Listing.Availability
	detail.Media = listingPictures(ad, &detail.Warnings)
	detail.Normalized["media"] = listingMediaMaps(detail.Media)
	detail.Seller = listingSeller(ad, links)
	detail.Normalized["seller"] = detail.Seller.Public

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

type MediaResponse struct {
	StatusCode int
	Headers    map[string][]string
	Body       io.ReadCloser
}

func (t *HTTPTransport) OpenMedia(input Request) (MediaResponse, error) {
	if input.Host != HostMedia {
		return MediaResponse{}, fmt.Errorf("streaming media requires the media host")
	}
	requestURL, err := t.requestURL(input)
	if err != nil {
		return MediaResponse{}, err
	}
	if input.Context == nil {
		input.Context = context.Background()
	}
	request, err := http.NewRequestWithContext(input.Context, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return MediaResponse{}, fmt.Errorf("create media request: %w", err)
	}
	for key, values := range input.Headers {
		if strings.ContainsAny(key, "\r\n") {
			return MediaResponse{}, fmt.Errorf("invalid header name")
		}
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n") {
				return MediaResponse{}, fmt.Errorf("invalid header value")
			}
			request.Header.Add(key, value)
		}
	}
	client := *t.client
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if t.redirectAllowed(HostMedia, next.URL) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	response, err := client.Do(request)
	if err != nil {
		return MediaResponse{}, ConnectError(err)
	}
	return MediaResponse{StatusCode: response.StatusCode, Headers: cloneHeaders(response.Header), Body: response.Body}, nil
}

func (t *MobileTransport) OpenMedia(input Request) (MediaResponse, error) {
	if input.Context == nil {
		input.Context = context.Background()
	}
	requestContext, cancel := requestDeadline(input.Context, HostMedia, input.Timeout)
	input.Context = requestContext
	input.Host = HostMedia
	input.Method = http.MethodGet
	input.Headers = t.mobileHeaders(HostMedia, input.Headers)
	if err := t.reserve(requestContext, HostMedia, false); err != nil {
		cancel()
		return MediaResponse{}, err
	}
	if opener, ok := t.base.(interface {
		OpenMedia(Request) (MediaResponse, error)
	}); ok {
		response, err := opener.OpenMedia(input)
		if err != nil {
			cancel()
			return MediaResponse{}, err
		}
		response.Body = &listingCancelReadCloser{ReadCloser: response.Body, cancel: cancel}
		return response, nil
	}
	response, err := t.base.Do(input)
	if err != nil {
		cancel()
		return MediaResponse{}, err
	}
	return MediaResponse{StatusCode: response.StatusCode, Headers: response.Headers, Body: &listingCancelReadCloser{ReadCloser: io.NopCloser(bytes.NewReader(response.Body)), cancel: cancel}}, nil
}

type listingCancelReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (r *listingCancelReadCloser) Close() error {
	err := r.ReadCloser.Close()
	r.cancel()
	return err
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

func listingMediaMaps(media []ListingMedia) []map[string]any {
	out := make([]map[string]any, 0, len(media))
	for _, item := range media {
		entry := map[string]any{"index": item.Index, "relation": item.Relation, "url": item.URL}
		if item.Width > 0 {
			entry["width"] = item.Width
		}
		if item.Height > 0 {
			entry["height"] = item.Height
		}
		if item.SizeLabel != "" {
			entry["size_label"] = item.SizeLabel
		}
		out = append(out, entry)
	}
	return out
}

func listingSeller(ad map[string]any, links []map[string]any) ListingSeller {
	id, _ := listingFieldString(ad, "user-id")
	name, _ := listingFieldString(ad, "contact-name")
	public := make(map[string]any)
	for _, field := range []string{"user-id", "contact-name", "contact-name-initials", "seller-account-type", "poster-type", "user-since-date-time", "user-rating", "userBadges", "company-name", "company-details"} {
		if value, ok := listingField(ad, field); ok {
			public[field] = value
		}
	}
	profileURL := ""
	for _, link := range links {
		relation, _ := listingFieldString(link, "rel")
		if relation == "self-user" {
			profileURL, _ = listingLinkURL(link)
			if profileURL != "" {
				public["profile-url"] = profileURL
			}
		}
	}
	return ListingSeller{ID: id, Name: name, ProfileURL: profileURL, Public: public}
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

func listingRedact(value any) any {
	switch current := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(current))
		for childKey, child := range current {
			if listingSensitiveKey(childKey) {
				out[childKey] = "[REDACTED]"
			} else {
				out[childKey] = listingRedact(child)
			}
		}
		return out
	case []any:
		out := make([]any, len(current))
		for index, child := range current {
			out[index] = listingRedact(child)
		}
		return out
	case string:
		return listingRedactString(current)
	default:
		return current
	}
}

func listingSensitiveKey(key string) bool {
	local := strings.ToLower(listingLocalName(key))
	return listingSensitiveKeys[local] || strings.HasSuffix(local, "-email") || strings.HasSuffix(local, "_email")
}

func listingRedactString(value string) string {
	value = RedactText(value)
	if !strings.Contains(value, "?") {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	query := parsed.Query()
	changed := false
	for key := range query {
		if listingSensitiveKeys[strings.ToLower(key)] {
			query.Set(key, "REDACTED")
			changed = true
		}
	}
	if changed {
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return value
}
