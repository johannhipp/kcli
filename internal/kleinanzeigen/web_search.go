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
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"golang.org/x/net/html"
)

var webPagePattern = regexp.MustCompile(`/seite:([0-9]+)(?:/|$)`)
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

func parseWebSearch(body []byte, pageNumber int) ([]byte, map[int]string, error) {
	doc, err := parseWebDocument(body)
	if err != nil {
		return nil, nil, err
	}
	props, err := webProps(doc)
	if err != nil {
		return nil, nil, err
	}
	rows := []any{}
	seen := map[string]bool{}
	found := false
	for _, p := range props {
		result, exists := p["resultAds"]
		if !exists {
			continue
		}
		found = true
		results, valid := result.([]any)
		if !valid {
			return nil, nil, webContract("website search results had an unexpected collection shape")
		}
		for _, row := range results {
			object := webObject(webObject(row)["organicAdPreview"])
			if object == nil {
				continue
			}
			id := webString(object["id"])
			title := webString(object["title"])
			reference := webString(object["seoLink"])
			if !numericID(id) || title == "" {
				return nil, nil, webContract("website search record omitted its identity")
			}
			if seen[id] {
				continue
			}
			seen[id] = true
			absolute := publicWebOrigin + reference
			if strings.HasPrefix(reference, "https://") {
				absolute = reference
			}
			returnedID, e := ListingReferenceID(absolute)
			if e != nil || returnedID != id {
				return nil, nil, webContract("website search record contained an invalid listing link")
			}
			if _, e = webURL(absolute); e != nil {
				return nil, nil, e
			}
			amount := ""
			if match := webListingPrice.FindString(webString(object["price"])); match != "" {
				amount = strings.ReplaceAll(strings.ReplaceAll(match, ".", ""), ",", ".")
			}
			rows = append(rows, map[string]any{"id": id, "title": title, "description": object["description"], "price": map[string]any{"amount": amount}, "status": "unknown", "user-id": object["userId"], "poster-type": object["posterType"], "link": []any{map[string]any{"rel": "self-public-website", "href": absolute}}, "web-preview": object})
		}
	}
	if !found {
		empty := false
		webWalk(doc, func(n *html.Node) {
			if webAttr(n, "id") == "saved-search-empty-result" {
				empty = true
			}
		})
		if !empty {
			legacy, recognized, legacyErr := webLegacySearch(doc)
			if legacyErr != nil {
				return nil, nil, legacyErr
			}
			if !recognized {
				return nil, nil, webContract("website omitted structured search results")
			}
			rows = legacy
		}
	}
	links := map[int]string{}
	webWalk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "a" {
			return
		}
		href := webAttr(n, "href")
		match := webPagePattern.FindStringSubmatch(href)
		if len(match) != 2 {
			return
		}
		page, _ := strconv.Atoi(match[1])
		if page < 2 {
			return
		}
		u, e := url.Parse(publicWebOrigin)
		if e != nil {
			return
		}
		target, e := u.Parse(href)
		if e != nil {
			return
		}
		if _, e = webURL(target.String()); e != nil {
			return
		}
		links[page-1] = target.String()
	})
	continuation := SearchContinuation{}
	if _, exists := links[pageNumber+1]; exists {
		next := pageNumber + 1
		continuation.Next = &next
	}
	encoded, err := webJSON(map[string]any{"ads": map[string]any{"ad": rows, "paging": continuation}})
	return encoded, links, err
}

// PublicSellerListings fetches one public inventory page; it does not imply a
// global seller directory or a complete inventory beyond that bounded page.
func (t *WebTransport) PublicSellerListings(ctx context.Context, id string) (SearchPage, error) {
	if !numericID(id) {
		return SearchPage{}, webInvalid("seller ID must contain decimal digits only")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	response, _, err := t.fetch(ctx, publicWebOrigin+"/s-bestandsliste.html?"+url.Values{"userId": {id}}.Encode())
	if err != nil {
		return SearchPage{}, err
	}
	if response.StatusCode != 200 {
		return SearchPage{}, webResponseError(response)
	}
	raw, _, err := parseWebSearch(response.Body, 0)
	if err != nil {
		return SearchPage{}, err
	}
	page, err := SearchParsePage(raw)
	doc, _ := parseWebDocument(response.Body)
	name := ""
	webWalk(doc, func(n *html.Node) {
		if webListingClass(n, "userprofile--name") {
			name = webText(n)
		}
	})
	for i := range page.Listings {
		page.Listings[i].Summary.Source = "public-web"
		page.Listings[i].Seller.ID = id
		page.Listings[i].Seller.Name = name
		page.Listings[i].Seller.Raw, _ = json.Marshal(map[string]any{"user-id": id, "contact-name": name})
	}
	return page, err
}

func webLegacySearch(doc *html.Node) ([]any, bool, error) {
	rows := []any{}
	recognized := false
	var firstErr error
	webWalk(doc, func(n *html.Node) {
		if webAttr(n, "id") == "page-searchresults-adtable" {
			recognized = true
		}
		if n.Type != html.ElementNode || n.Data != "article" || !webListingClass(n, "aditem") {
			return
		}
		id := webAttr(n, "data-adid")
		reference := webAttr(n, "data-href")
		target := publicWebOrigin + reference
		if strings.HasPrefix(reference, "https://") {
			target = reference
		}
		returnedID, err := ListingReferenceID(target)
		if err != nil || returnedID != id {
			firstErr = webContract("public inventory contains an invalid listing link")
			return
		}
		if _, err = webURL(target); err != nil {
			firstErr = err
			return
		}
		title, description, price := "", "", ""
		webWalk(n, func(child *html.Node) {
			if child.Data == "h2" {
				title = webText(child)
			}
			if webListingClass(child, "aditem-main--middle--description") {
				description = webText(child)
			}
			if webListingClass(child, "aditem-main--middle--price-shipping--price") {
				price = webText(child)
			}
		})
		if title == "" {
			firstErr = webContract("public inventory listing omitted title")
			return
		}
		amount := ""
		if match := webListingPrice.FindString(price); match != "" {
			amount = strings.ReplaceAll(strings.ReplaceAll(match, ".", ""), ",", ".")
		}
		rows = append(rows, map[string]any{"id": id, "title": title, "description": description, "price": map[string]any{"amount": amount}, "status": "unknown", "link": []any{map[string]any{"rel": "self-public-website", "href": target}}})
	})
	return rows, recognized, firstErr
}

// A successful HTTP response is not proof a filter was applied. Require the
// website's selected form state to echo every requested dynamic filter.
func validateWebFilterState(body []byte, query map[string][]string) error {
	common := map[string]bool{"q": true, "categoryId": true, "locationId": true, "distance": true, "minPrice": true, "maxPrice": true, "adType": true, "sortType": true, "page": true, "size": true, "pictureRequired": true}
	dynamic := map[string]string{}
	for key, values := range query {
		if !common[key] && len(values) == 1 {
			dynamic[key] = values[0]
		}
	}
	if len(dynamic) == 0 {
		return nil
	}
	doc, err := parseWebDocument(body)
	if err != nil {
		return err
	}
	props, err := webProps(doc)
	if err != nil {
		return err
	}
	var form map[string]any
	for _, p := range props {
		if _, ok := p["searchUrl"]; ok {
			form = p
			break
		}
	}
	if form == nil {
		return webContract("filtered search omitted its selected form state")
	}
	selected := map[string]string{}
	for _, name := range []string{"attributeMap", "nonRangeAttributeMap", "globalFilters"} {
		for key, value := range webObject(form[name]) {
			selected[key] = webString(value)
		}
	}
	clicked := map[string]bool{}
	for _, value := range webArray(form["clickableOptions"]) {
		clicked[webString(value)] = true
	}
	for key, expected := range dynamic {
		if strings.HasSuffix(key, "_b") && (expected == "true" || expected == "false") {
			if clicked[key] != (expected == "true") {
				return webContract("website did not apply requested boolean filter " + key)
			}
			continue
		}
		actual, present := selected[key]
		// The public condition links use the German key; the observed form
		// state echoes its English alias. No other aliases are inferred.
		if !present && key == "global.zustand" {
			actual, present = selected["global.condition"]
		}
		if !present && strings.HasPrefix(key, "global.") {
			actual, present = selected[strings.TrimPrefix(key, "global.")]
		}
		if !present || strings.TrimSuffix(actual, ",") != strings.TrimSuffix(expected, ",") {
			return webContract("website did not apply requested filter " + key)
		}
	}
	return nil
}
