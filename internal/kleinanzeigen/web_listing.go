package kleinanzeigen

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"golang.org/x/net/html"
)

// parseWebListing translates only publicly rendered detail fields into the
// normalized listing parser's input. An absent field remains unknown.
func parseWebListing(body []byte, finalURL string) ([]byte, error) {
	doc, err := parseWebDocument(body)
	if err != nil {
		return nil, err
	}
	id, err := ListingReferenceID(finalURL)
	if err != nil {
		return nil, err
	}
	ad := map[string]any{"id": id}
	if parsed, e := url.Parse(finalURL); e == nil {
		parts := strings.Split(parsed.Path[strings.LastIndex(parsed.Path, "/")+1:], "-")
		if len(parts) == 3 {
			ad["category"] = map[string]any{"id": parts[1]}
		}
	}
	links := []map[string]any{{"rel": "self-public-website", "href": finalURL}}
	pictures := []map[string]any{}
	attributes := []map[string]any{}
	images := map[string]bool{}
	webWalk(doc, func(n *html.Node) {
		switch webAttr(n, "id") {
		case "viewad-title":
			ad["title"] = webText(n)
		case "viewad-description-text":
			ad["description"] = webListingDescription(n)
		case "viewad-price":
			label := webText(n)
			ad["web-price-label"] = label
			price := map[string]any{}
			if match := webListingPrice.FindString(label); match != "" {
				price["amount"] = strings.ReplaceAll(strings.ReplaceAll(match, ".", ""), ",", ".")
			}
			switch {
			case strings.Contains(label, "VB"):
				price["price-type"] = "NEGOTIABLE"
			case strings.Contains(strings.ToLower(label), "zu verschenken"):
				price["price-type"] = "GIVE_AWAY"
			case strings.Contains(label, "€"):
				price["price-type"] = "FIXED"
			}
			ad["price"] = price
		case "viewad-locality":
			label := webText(n)
			location := map[string]any{"localized-label": label}
			if match := webListingPostcode.FindString(label); match != "" {
				location["zip-code"] = match
			}
			ad["ad-address"] = location
		case "viewad-extra-info":
			webWalk(n, func(child *html.Node) {
				if child.Data == "span" {
					if date, e := time.Parse("02.01.2006", webText(child)); e == nil {
						ad["start-date-time"] = date.Format("2006-01-02")
					}
				}
			})
		case "viewad-cntr-num":
			if count := strings.TrimSpace(webText(n)); count != "" {
				ad["view-count"] = count
			}
		case "viewad-commercial-policy-documents":
			// Commercial pages may render the seller name without a profile
			// anchor. The public legal-documents element identifies that seller.
			if sellerID := webAttr(n, "data-user-id"); numericID(sellerID) {
				ad["user-id"] = sellerID
			}
		case "viewad-contact":
			webWalk(n, func(child *html.Node) {
				if webListingClass(child, "userprofile-vip") {
					ad["contact-name"] = webText(child)
				}
				if webListingClass(child, "userbadge-tag") {
					badges, _ := ad["userBadges"].([]string)
					ad["userBadges"] = append(badges, webText(child))
				}
				if webListingClass(child, "userprofile-vip-details-text") {
					label := webText(child)
					if label == "Privater Nutzer" {
						ad["poster-type"] = "PRIVATE"
					}
					if label == "Gewerblicher Nutzer" {
						ad["poster-type"] = "COMMERCIAL"
					}
					if strings.HasPrefix(label, "Aktiv seit ") {
						ad["account-age"] = label
					}
				}
				if child.Data == "a" {
					href := webAttr(child, "href")
					parsed, e := url.Parse(href)
					if e == nil && parsed.Path == "/s-bestandsliste.html" && parsed.Query().Get("userId") != "" {
						ad["user-id"] = parsed.Query().Get("userId")
						links = append(links, map[string]any{"rel": "self-user", "href": "https://www.kleinanzeigen.de" + parsed.RequestURI()})
					}
				}
			})
		}
		if webListingClass(n, "addetailslist--detail") {
			var label strings.Builder
			value := ""
			for child := n.FirstChild; child != nil; child = child.NextSibling {
				if webListingClass(child, "addetailslist--detail--value") {
					value = webText(child)
				} else {
					label.WriteString(webText(child))
					label.WriteByte(' ')
				}
			}
			name := strings.Join(strings.Fields(label.String()), " ")
			if name != "" {
				attributes = append(attributes, map[string]any{"name": name, "localized-label": name, "value": value})
			}
		}
		if n.Data == "img" && webAttr(n, "itemprop") == "image" {
			src := webAttr(n, "data-imgsrc")
			if src == "" {
				src = webAttr(n, "src")
			}
			if src != "" && !images[src] {
				images[src] = true
				pictures = append(pictures, map[string]any{"link": []map[string]any{{"rel": "large", "href": src}}})
			}
		}
		if webListingClass(n, "boxedarticle--details--shipping") {
			label := webText(n)
			ad["web-shipping-label"] = label
			if label == "Nur Abholung" {
				ad["pickup"] = true
				ad["shipping"] = false
			}
		}
	})
	if title, _ := ad["title"].(string); title == "" {
		return nil, &domain.Error{Code: domain.CodeUpstreamContract, Message: "public listing page omitted its title"}
	}
	ad["link"] = links
	ad["pictures"] = map[string]any{"picture": pictures}
	ad["attributes"] = map[string]any{"attribute": attributes}
	return json.Marshal(ad)
}

var webListingPrice = regexp.MustCompile(`[0-9]+(?:\.[0-9]{3})*(?:,[0-9]{1,2})?`)
var webListingPostcode = regexp.MustCompile(`^[0-9]{5}\b`)

func webListingClass(n *html.Node, class string) bool {
	for _, item := range strings.Fields(webAttr(n, "class")) {
		if item == class {
			return true
		}
	}
	return false
}

func webListingDescription(n *html.Node) string {
	var result strings.Builder
	webWalk(n, func(child *html.Node) {
		if child.Type == html.TextNode {
			result.WriteString(child.Data)
		} else if child.Data == "br" {
			result.WriteByte('\n')
		}
	})
	lines := strings.Split(result.String(), "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
