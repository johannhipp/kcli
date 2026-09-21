package kleinanzeigen

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

var webPagePattern = regexp.MustCompile(`/seite:([0-9]+)(?:/|$)`)

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
