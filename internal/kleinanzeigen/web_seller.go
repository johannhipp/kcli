package kleinanzeigen

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"golang.org/x/net/html"
)

// PublicSeller reads the public inventory/profile page for an exact seller ID.
// It does not search names or infer identities from listing text.
func (t *WebTransport) PublicSeller(ctx context.Context, id string) (domain.SellerV1, error) {
	if !numericID(id) {
		return domain.SellerV1{}, webInvalid("seller ID must contain decimal digits only")
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	target := publicWebOrigin + "/s-bestandsliste.html?" + url.Values{"userId": {id}}.Encode()
	response, _, err := t.fetch(ctx, target)
	if err != nil {
		return domain.SellerV1{}, err
	}
	if response.StatusCode != 200 {
		return domain.SellerV1{}, webResponseError(response)
	}
	doc, err := parseWebDocument(response.Body)
	if err != nil {
		return domain.SellerV1{}, err
	}
	seller := domain.SellerV1{ID: id, ProfileURL: target, Source: "public-web", Completeness: domain.CompletenessBestEffort, Badges: []string{}, Public: map[string]any{"profile-url": target, "user-id": id}}
	webWalk(doc, func(n *html.Node) {
		if webListingClass(n, "userprofile--name") {
			seller.Name = webText(n)
		}
		if webListingClass(n, "userbadge-tag") {
			seller.Badges = append(seller.Badges, webText(n))
		}
		if webListingClass(n, "userprofile-details-text") {
			text := webText(n)
			if strings.HasPrefix(text, "Aktiv seit ") {
				seller.AccountAge = text
			}
			switch text {
			case "Privater Nutzer":
				seller.PosterType = "PRIVATE"
			case "Gewerblicher Nutzer":
				seller.PosterType = "COMMERCIAL"
			}
		}
	})
	if seller.Name == "" {
		return domain.SellerV1{}, webContract("public seller page omitted its profile name")
	}
	seller.Public["contact-name"] = seller.Name
	seller.Public["userBadges"] = seller.Badges
	if seller.AccountAge != "" {
		seller.Public["account-age"] = seller.AccountAge
	}
	if seller.PosterType != "" {
		seller.Public["poster-type"] = seller.PosterType
	}
	return seller, nil
}
