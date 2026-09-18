package kleinanzeigen

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/net/html"
)

func parseWebCategories(body []byte) ([]byte, error) {
	doc, err := parseWebDocument(body)
	if err != nil {
		return nil, err
	}
	props, err := webProps(doc)
	if err != nil {
		return nil, err
	}
	var convert func([]any) []any
	convert = func(rows []any) []any {
		out := make([]any, 0, len(rows))
		for _, row := range rows {
			object := webObject(row)
			id := webString(object["id"])
			label := webString(object["categoryName"])
			if !numericID(id) || label == "" {
				continue
			}
			out = append(out, map[string]any{"id": id, "localized-name": label, "category": convert(webArray(object["children"]))})
		}
		return out
	}
	for _, p := range props {
		if rows, ok := p["categories"]; ok {
			categories := convert(webArray(rows))
			if len(categories) == 0 {
				return nil, webContract("website category tree contained no identities")
			}
			return webJSON(map[string]any{"category": categories})
		}
	}
	return nil, webContract("website omitted its category tree")
}
func parseWebLocations(body []byte) ([]byte, error) {
	var locations map[string]string
	if err := json.Unmarshal(body, &locations); err != nil {
		return nil, webContract("website location suggestions had an unexpected shape")
	}
	keys := make([]string, 0, len(locations))
	for key := range locations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]any, 0, len(keys))
	for _, key := range keys {
		id := strings.TrimPrefix(key, "_")
		if !numericID(id) || locations[key] == "" {
			continue
		}
		rows = append(rows, map[string]any{"id": id, "localized-name": locations[key]})
	}
	return webJSON(map[string]any{"location": rows})
}

var webFilterLinkPattern = regexp.MustCompile(`\+([^:+/?]+):([^+/ ?]+)`)

func parseWebFilters(body []byte) ([]byte, error) {
	doc, err := parseWebDocument(body)
	if err != nil {
		return nil, err
	}
	props, err := webProps(doc)
	if err != nil {
		return nil, err
	}
	definitions := map[string]any{}
	values := map[string]map[string]string{}
	hasForm := false
	addLink := func(raw, label string) {
		for _, match := range webFilterLinkPattern.FindAllStringSubmatch(raw, -1) {
			key, err := url.PathUnescape(match[1])
			if err != nil || validateQueryKey(key) != nil {
				continue
			}
			value, err := url.PathUnescape(match[2])
			if err != nil || value == "" {
				continue
			}
			if values[key] == nil {
				values[key] = map[string]string{}
			}
			if values[key][value] == "" {
				values[key][value] = label
			}
		}
	}
	var menus func(any)
	menus = func(value any) {
		for _, v := range webArray(value) {
			item := webObject(v)
			addLink(webString(item["url"]), webString(item["localizedName"]))
			menus(item["menuItems"])
			menus(item["hiddenMenuItems"])
		}
	}
	for _, p := range props {
		if _, ok := p["searchUrl"]; ok {
			hasForm = true
		}
		menus(p["menuItems"])
		for _, field := range []string{"rangeItem", "item", "month", "year"} {
			item := webObject(p[field])
			key := webString(item["attributeId"])
			if key == "" || validateQueryKey(key) != nil {
				continue
			}
			valueType := webString(item["attributeType"])
			typeName := "string"
			if valueType != "INT" && valueType != "UNFORMATTED_INT" && valueType != "DECIMAL" {
				typeName = "unknown"
			}
			definitions[key] = map[string]any{"type": typeName, "search-param": "attributeMap[" + key + "]", "search-style": "eq", "localized-label": webString(p["attributeName"]), "web-kind": "range", "web-value-type": valueType, "web-format": "MIN,MAX (either bound may be empty)"}
		}
	}
	if !hasForm {
		return nil, webContract("website omitted its search form; filter contract is unknown")
	}
	webWalk(doc, func(n *html.Node) {
		if n.Type != html.ElementNode {
			return
		}
		if n.Data == "a" {
			addLink(webAttr(n, "href"), webText(n))
		}
		if n.Data == "input" && webAttr(n, "name") == "clickableOptions" && webAttr(n, "type") == "checkbox" {
			key := webAttr(n, "value")
			if key == "" || validateQueryKey(key) != nil {
				return
			}
			definitions[key] = map[string]any{"type": "boolean", "search-param": "clickableOptions", "search-style": "eq", "localized-label": key, "web-kind": "boolean"}
		}
	})
	for key, choices := range values {
		supported := make([]any, 0, len(choices))
		keys := make([]string, 0, len(choices))
		for value := range choices {
			keys = append(keys, value)
		}
		sort.Strings(keys)
		for _, value := range keys {
			supported = append(supported, map[string]any{"value": value, "localized-label": choices[value]})
		}
		definitions[key] = map[string]any{"type": "string", "search-param": "web-link", "search-style": "eq", "localized-label": key, "supported-value": supported, "web-kind": "enum"}
	}
	return webJSON(map[string]any{"ads-search-options": definitions})
}
