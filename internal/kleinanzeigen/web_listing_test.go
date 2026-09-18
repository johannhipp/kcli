package kleinanzeigen

import "testing"

func TestParseWebListingPublicFields(t *testing.T) {
	raw, err := parseWebListing([]byte(`<html><h1 id="viewad-title">Bike &amp; basket</h1><h2 id="viewad-price">1.250,50 € VB</h2><span id="viewad-locality">10115 Berlin</span><p id="viewad-description-text">First line<br><br>Second line</p><div id="viewad-extra-info"><span>06.09.2026</span></div><li class="addetailslist--detail">Zustand<span class="addetailslist--detail--value">Gut</span></li><div id="viewad-contact"><span class="userprofile-vip"><a href="/s-bestandsliste.html?userId=42">Example</a></span></div><img itemprop="image" data-imgsrc="https://img.kleinanzeigen.de/example.jpg"><img itemprop="image" src="https://img.kleinanzeigen.de/example.jpg"><span class="boxedarticle--details--shipping">Nur Abholung</span></html>`), "https://www.kleinanzeigen.de/s-anzeige/bike/123456-217-3331")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := ListingParse(raw)
	if err != nil {
		t.Fatal(err)
	}
	l := detail.Listing
	if l.ID != "123456" || l.Title != "Bike & basket" || l.Description != "First line\n\nSecond line" {
		t.Fatalf("wrong identity/text: %#v", l)
	}
	if l.AmountCents == nil || *l.AmountCents != 125050 || l.PriceType != "NEGOTIABLE" {
		t.Fatalf("wrong price: %#v", l)
	}
	if l.Location == nil || l.Location.Postcode != "10115" || l.PostedAt != "2026-09-06" {
		t.Fatalf("wrong metadata: %#v", l)
	}
	if len(l.Attributes) != 1 || l.Attributes[0].Value != "Gut" {
		t.Fatalf("attributes: %#v", l.Attributes)
	}
	if l.Seller.ID != "42" || l.Seller.Name != "Example" || len(l.Media) != 1 {
		t.Fatalf("seller/media: %#v", l)
	}
	if l.Availability != "unknown" || l.Pickup == nil || !*l.Pickup || l.Shipping == nil || *l.Shipping.Available {
		t.Fatalf("invented availability or wrong pickup: %#v", l)
	}
}

func TestParseWebListingMissingTitle(t *testing.T) {
	if _, err := parseWebListing([]byte(`<html><h1>Sign in</h1></html>`), "https://www.kleinanzeigen.de/s-anzeige/bike/123456-217-3331"); err == nil {
		t.Fatal("accepted non-detail page")
	}
}

func TestParseWebListingMissingFieldsRemainUnknown(t *testing.T) {
	raw, err := parseWebListing([]byte(`<h1 id="viewad-title">Example</h1>`), "https://www.kleinanzeigen.de/s-anzeige/bike/123456-217-3331")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := ListingParse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Listing.AmountCents != nil || detail.Listing.Pickup != nil || detail.Listing.Location != nil || detail.Listing.Availability != "unknown" {
		t.Fatalf("invented fields: %#v", detail.Listing)
	}
}

func TestParseWebListingCommercialSellerWithoutProfileLink(t *testing.T) {
	body := `<h1 id="viewad-title">Example bicycle</h1><div id="viewad-contact"><span class="userprofile-vip">Example Shop</span><span class="userprofile-vip-details-text">Gewerblicher Nutzer</span></div><div id="viewad-commercial-policy-documents" data-loaded="false" data-user-id="98765" style="display:none;"></div>`
	raw, err := parseWebListing([]byte(body), "https://www.kleinanzeigen.de/s-anzeige/bike/123456-217-3331")
	if err != nil {
		t.Fatal(err)
	}
	detail, err := ListingParse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Seller.ID != "98765" || detail.Seller.Name != "Example Shop" || detail.Seller.PosterType != "COMMERCIAL" {
		t.Fatalf("missing commercial seller: %#v", detail.Seller)
	}
	if detail.Seller.ProfileURL != "" {
		t.Fatal("invented unobserved profile URL")
	}
}
