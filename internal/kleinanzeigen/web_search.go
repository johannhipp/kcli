package kleinanzeigen

import (
	"context"
	"encoding/json"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/johannhipp/kcli/internal/domain"
)

var webRangeValuePattern = regexp.MustCompile(`^(?:[0-9]+(?:\.[0-9]+)?)?,(?:[0-9]+(?:\.[0-9]+)?)?$`)

func (t *WebTransport) search(ctx context.Context, query map[string][]string) (Response, error) {
	if size := firstQuery(query, "size"); size != "" && size != "25" {
		return Response{}, webInvalid("website pages have a server-controlled size; use page-size 25 and limit to bound output")
	}
	if firstQuery(query, "pictureRequired") == "true" {
		return Response{}, webInvalid("picture-required has no verified anonymous website encoding")
	}
	page, err := strconv.Atoi(firstQuery(query, "page"))
	if firstQuery(query, "page") == "" {
		page = 0
		err = nil
	}
	if err != nil || page < 0 || page > 100 {
		return Response{}, webInvalid("invalid website page number")
	}
	keyQuery := url.Values(searchCloneQuery(query))
	keyQuery.Del("page")
	cacheKey := keyQuery.Encode()
	t.mu.Lock()
	target, known := t.pages[cacheKey][page]
	t.mu.Unlock()
	if known && target == "" {
		return Response{StatusCode: 200, Body: []byte(`{"ads":{"ad":[],"paging":{"nextPage":null}}}`)}, nil
	}
	if !known {
		// Later pages are reached only through links actually supplied by the site.
		// A standalone page request starts with page one to discover those links.
		form, suffixes, err := t.searchForm(ctx, query)
		if err != nil {
			return Response{}, err
		}
		response, finalURL, err := t.fetch(ctx, publicWebOrigin+"/s-suchanfrage.html?"+form.Encode())
		if err != nil {
			return Response{}, err
		}
		if response.StatusCode != http.StatusOK {
			return Response{}, webResponseError(response)
		}
		if len(suffixes) > 0 {
			u, _ := url.Parse(finalURL)
			u.Path += strings.Join(suffixes, "")
			u.RawPath = ""
			response, finalURL, err = t.fetch(ctx, u.String())
			if err != nil {
				return Response{}, err
			}
			if response.StatusCode != http.StatusOK {
				return Response{}, webResponseError(response)
			}
		}
		if err := validateWebFilterState(response.Body, query); err != nil {
			return Response{}, err
		}
		normalized, links, err := parseWebSearch(response.Body, 0)
		if err != nil {
			return Response{}, err
		}
		t.rememberSearch(cacheKey, 0, finalURL, links, normalized)
		if page == 0 {
			response.Body = normalized
			return response, nil
		}
		t.mu.Lock()
		target, known = t.pages[cacheKey][page]
		t.mu.Unlock()
		if !known {
			return Response{}, webInvalid("requested page is not linked by the website; use paginate from page 0")
		}
		if target == "" {
			return Response{StatusCode: 200, Body: []byte(`{"ads":{"ad":[],"paging":{"nextPage":null}}}`)}, nil
		}
	}
	response, finalURL, err := t.fetch(ctx, target)
	if err != nil {
		return Response{}, err
	}
	if response.StatusCode != http.StatusOK {
		return Response{}, webResponseError(response)
	}
	if err := validateWebFilterState(response.Body, query); err != nil {
		return Response{}, err
	}
	normalized, links, err := parseWebSearch(response.Body, page)
	if err != nil {
		return Response{}, err
	}
	t.rememberSearch(cacheKey, page, finalURL, links, normalized)
	response.Body = normalized
	return response, nil
}
func (t *WebTransport) rememberSearch(key string, page int, finalURL string, links map[int]string, normalized []byte) {
	parsed, _ := SearchParsePage(normalized)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.pages[key] == nil {
		t.pages[key] = map[int]string{}
	}
	t.pages[key][page] = finalURL
	for number, link := range links {
		t.pages[key][number] = link
	}
	if _, exists := links[page+1]; !exists {
		t.pages[key][page+1] = ""
	}
	for _, listing := range parsed.Listings {
		t.listingURLs[listing.Summary.ID] = listing.Summary.URL
	}
}
func (t *WebTransport) searchForm(ctx context.Context, query map[string][]string) (url.Values, []string, error) {
	form := url.Values{}
	fixed := map[string]string{"q": "keywords", "categoryId": "categoryId", "locationId": "locationId", "distance": "radius", "minPrice": "minPrice", "maxPrice": "maxPrice"}
	known := map[string]bool{"size": true, "page": true, "pictureRequired": true, "adType": true, "sortType": true}
	for source, destination := range fixed {
		known[source] = true
		if value := firstQuery(query, source); value != "" {
			form.Set(destination, value)
		}
	}
	if value := firstQuery(query, "adType"); value != "" {
		mapped := map[string]string{"OFFERED": "OFFER", "WANTED": "WANTED"}[value]
		if mapped == "" {
			return nil, nil, webInvalid("unsupported listing type")
		}
		form.Set("adType", mapped)
	}
	if value := firstQuery(query, "sortType"); value != "" {
		mapped := map[string]string{"DATE_DESCENDING": "SORTING_DATE", "PRICE_ASCENDING": "PRICE_AMOUNT", "PRICE_DESCENDING": "PRICE_AMOUNT_DESC", "DISTANCE_ASCENDING": "DISTANCE"}[value]
		if mapped == "" {
			return nil, nil, webInvalid("unsupported sort order")
		}
		form.Set("sortingField", mapped)
	}
	keys := []string{}
	for key := range query {
		if !known[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return form, nil, nil
	}
	if t.database == nil {
		return nil, nil, webInvalid("dynamic filters require cached category metadata")
	}
	cached, err := t.database.ListFilters(ctx, firstQuery(query, "categoryId"))
	if err != nil {
		return nil, nil, err
	}
	definitions := map[string]map[string]any{}
	for _, item := range cached {
		var raw map[string]any
		if json.Unmarshal(item.RawJSON, &raw) == nil {
			definitions[item.Key] = raw
		}
	}
	suffixes := []string{}
	for _, key := range keys {
		definition := definitions[key]
		if definition == nil || len(query[key]) != 1 {
			return nil, nil, webInvalid("dynamic filter is missing verified metadata or has multiple values")
		}
		value := query[key][0]
		switch webString(definition["web-kind"]) {
		case "enum":
			accepted := false
			for _, option := range webArray(definition["supported-value"]) {
				if webString(webObject(option)["value"]) == value {
					accepted = true
				}
			}
			if !accepted || strings.ContainsAny(value, "+/:?#") {
				return nil, nil, webInvalid("filter value is not an advertised website choice")
			}
			suffixes = append(suffixes, "+"+key+":"+value)
		case "range":
			if !webRangeValuePattern.MatchString(value) || value == "," {
				return nil, nil, webInvalid("range filter requires MIN,MAX; either bound may be empty")
			}
			bounds := strings.Split(value, ",")
			kind := webString(definition["web-value-type"])
			if kind != "INT" && kind != "UNFORMATTED_INT" && kind != "DECIMAL" {
				return nil, nil, webInvalid("range value type is not a verified website encoding")
			}
			if strings.Contains(kind, "INT") && (strings.Contains(bounds[0], ".") || strings.Contains(bounds[1], ".")) {
				return nil, nil, webInvalid("this range requires integer bounds")
			}
			if bounds[0] != "" && bounds[1] != "" {
				lo, _ := new(big.Rat).SetString(bounds[0])
				hi, _ := new(big.Rat).SetString(bounds[1])
				if lo.Cmp(hi) > 0 {
					return nil, nil, webInvalid("range minimum exceeds maximum")
				}
			}
			form["attributeMap["+key+"]"] = bounds
		case "boolean":
			if value != "true" && value != "false" {
				return nil, nil, webInvalid("boolean filter requires true or false")
			}
			if value == "true" {
				form.Add("clickableOptions", key)
			}
		default:
			return nil, nil, webInvalid("dynamic filter has no verified website encoding")
		}
	}
	return form, suffixes, nil
}
func firstQuery(query map[string][]string, key string) string {
	if len(query[key]) == 0 {
		return ""
	}
	return query[key][0]
}
func webInvalid(message string) error {
	return &domain.Error{Code: domain.CodeInvalidInput, Message: message}
}
