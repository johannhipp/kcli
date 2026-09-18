package kleinanzeigen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/johannhipp/kcli/internal/domain"

	"golang.org/x/net/html"
)

func parseWebDocument(body []byte) (*html.Node, error) { return html.Parse(bytes.NewReader(body)) }
func webAttr(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}
func webWalk(node *html.Node, visit func(*html.Node)) {
	visit(node)
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		webWalk(child, visit)
	}
}
func webText(node *html.Node) string {
	var out strings.Builder
	webWalk(node, func(n *html.Node) {
		if n.Type == html.TextNode {
			out.WriteString(n.Data)
			out.WriteByte(' ')
		}
	})
	return strings.Join(strings.Fields(out.String()), " ")
}

// Only decode the hydration components needed for anonymous discovery. Other
// islands can contain login URLs and page tokens and are never retained.
func webProps(doc *html.Node) ([]map[string]any, error) {
	var out []map[string]any
	var firstErr error
	webWalk(doc, func(n *html.Node) {
		if firstErr != nil || n.Type != html.ElementNode || n.Data != "astro-island" {
			return
		}
		component := webAttr(n, "component-url")
		relevant := false
		for _, name := range []string{"SearchForm.", "ImpressionTracker.", "CollapsibleMenuItems.", "RenderBrowse", "SuggestedInput", "MonthYear"} {
			if strings.Contains(component, name) {
				relevant = true
			}
		}
		raw := webAttr(n, "props")
		// Range component filenames have changed independently of their prop contract.
		if strings.Contains(raw, `"rangeItem"`) || strings.Contains(raw, `"attributeName"`) {
			relevant = true
		}
		if !relevant || raw == "" {
			return
		}
		var tagged map[string]any
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&tagged); err != nil {
			firstErr = fmt.Errorf("decode website hydration: %w", err)
			return
		}
		props := make(map[string]any, len(tagged))
		for key, value := range tagged {
			decoded, err := webAstro(value)
			if err != nil {
				firstErr = err
				return
			}
			props[key] = decoded
		}
		out = append(out, props)
	})
	if firstErr != nil {
		return nil, &domain.Error{Code: domain.CodeUpstreamContract, Message: "public website hydration could not be decoded", Cause: firstErr}
	}
	return out, nil
}
func webAstro(value any) (any, error) {
	tagged, ok := value.([]any)
	if !ok || len(tagged) == 0 {
		return nil, fmt.Errorf("invalid website hydration value")
	}
	tag, ok := tagged[0].(json.Number)
	if !ok {
		return nil, fmt.Errorf("invalid website hydration tag")
	}
	if len(tagged) == 1 {
		return nil, nil
	}
	value = tagged[1]
	switch string(tag) {
	case "0":
		if object, ok := value.(map[string]any); ok {
			out := make(map[string]any, len(object))
			for key, item := range object {
				decoded, err := webAstro(item)
				if err != nil {
					return nil, err
				}
				out[key] = decoded
			}
			return out, nil
		}
		return value, nil
	case "1":
		values, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("invalid website hydration array")
		}
		out := make([]any, 0, len(values))
		for _, item := range values {
			decoded, err := webAstro(item)
			if err != nil {
				return nil, err
			}
			out = append(out, decoded)
		}
		return out, nil
	case "3", "6", "7":
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported website hydration tag %s", tag)
	}
}
func webString(value any) string         { text, _ := StringValue(value); return text }
func webObject(value any) map[string]any { object, _ := value.(map[string]any); return object }
func webArray(value any) []any           { array, _ := value.([]any); return array }
