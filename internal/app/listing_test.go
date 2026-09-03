package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/state"
)

type listingAppClock struct{ now time.Time }

func (c listingAppClock) Now() time.Time { return c.now }
func (c listingAppClock) Sleep(ctx context.Context, duration time.Duration) error {
	return nil
}

type listingAppTransport struct {
	body     []byte
	status   int
	requests []kleinanzeigen.Request
}

func (t *listingAppTransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	t.requests = append(t.requests, request)
	return kleinanzeigen.Response{StatusCode: t.status, Body: append([]byte(nil), t.body...)}, nil
}

func listingAppFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func listingTestApp(t *testing.T) (*App, *listingAppTransport, *state.DB) {
	t.Helper()
	ctx := context.Background()
	database, err := state.Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	transport := &listingAppTransport{status: http.StatusOK, body: listingAppFixture(t, "listing.json")}
	clock := listingAppClock{now: time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)}
	return New(Dependencies{Transport: transport, State: database, Clock: clock}), transport, database
}

func TestListingUseCasesNormalizeEnumerateOpenAndIndexSeller(t *testing.T) {
	app, transport, database := listingTestApp(t)
	defer database.Close()
	ctx := context.Background()
	listing, err := app.ListingGet(ctx, domain.ListingGetInputV1{IDOrURL: "1234567890", Raw: true}, "req-listing")
	if err != nil {
		t.Fatal(err)
	}
	if listing.Schema != "kcli.listing/v1" || listing.Data.ID != "1234567890" || listing.Data.Availability != "available" || listing.Data.AmountCents == nil || *listing.Data.AmountCents != 1995 || len(listing.Raw) == 0 {
		t.Fatalf("listing output=%#v", listing)
	}
	if string(listing.Raw) == "" || !json.Valid(listing.Raw) {
		t.Fatalf("raw=%q", listing.Raw)
	}
	images, err := app.ListingImages(ctx, domain.ListingImagesInputV1{IDOrURL: "1234567890", MaxBytes: 1024}, "req-images")
	if err != nil || len(images.Data) != 3 || images.Data[0]["relation"] != "XXL" {
		t.Fatalf("images=%#v err=%v", images, err)
	}
	for _, request := range transport.requests {
		if request.Host == kleinanzeigen.HostMedia {
			t.Fatalf("enumeration fetched media: %#v", request)
		}
	}
	opened, err := app.ListingOpen(ctx, domain.ListingOpenInputV1{IDOrURL: "1234567890"}, "req-open")
	if err != nil || opened.Data["url"] != listing.Data.URL {
		t.Fatalf("open=%#v err=%v", opened, err)
	}
	seller, err := database.SellerByID(ctx, "987654321")
	if err != nil || seller.DisplayName != "Händler Änne" || seller.Source != "listing" || seller.Completeness != "direct" {
		t.Fatalf("indexed seller=%#v err=%v", seller, err)
	}
	known, err := database.SellerListings(ctx, "987654321", 25)
	if err != nil || len(known) != 1 || known[0].ListingID != "1234567890" {
		t.Fatalf("known listings=%#v err=%v", known, err)
	}
}

func TestSellerLocalCommandsAreScopeLabeledAndNFKCFolded(t *testing.T) {
	app, _, database := listingTestApp(t)
	defer database.Close()
	ctx := context.Background()
	if _, err := app.ListingGet(ctx, domain.ListingGetInputV1{IDOrURL: "1234567890"}, "seed"); err != nil {
		t.Fatal(err)
	}
	direct, err := app.SellerGet(ctx, domain.SellerGetInputV1{Listing: "1234567890"}, "direct")
	if err != nil || direct.Source != "listing" || direct.Data.Source != "listing" || direct.Data.Completeness != "direct" || direct.ObservedAt.IsZero() {
		t.Fatalf("direct=%#v err=%v", direct, err)
	}
	profile, err := app.SellerGet(ctx, domain.SellerGetInputV1{IDOrURL: "https://api.kleinanzeigen.de/api/users/987654321"}, "profile")
	if err != nil || profile.Source != "profile-link" || profile.Data.Completeness != "best-effort" {
		t.Fatalf("profile=%#v err=%v", profile, err)
	}
	search, err := app.SellerSearch(ctx, domain.SellerSearchInputV1{Name: "ＨÄNDLER ÄNNE", Match: "exact"}, "search")
	if err != nil || len(search.Data) != 1 || search.Data[0].Source != "local-index" || search.Data[0].Completeness != "best-effort" || len(search.Warnings) < 2 {
		t.Fatalf("search=%#v err=%v", search, err)
	}
	listings, err := app.SellerListings(ctx, domain.SellerListingsInputV1{IDOrURL: "987654321", Limit: 10}, "listings")
	if err != nil || listings.Completeness != domain.CompletenessKnownOnly || len(listings.Data) != 1 || listings.Data[0].ID != "1234567890" || listings.ObservedAt.IsZero() {
		t.Fatalf("listings=%#v err=%v", listings, err)
	}
}

func TestSellerMissSaysNotInLocalIndex(t *testing.T) {
	app, _, database := listingTestApp(t)
	defer database.Close()
	_, err := app.SellerGet(context.Background(), domain.SellerGetInputV1{IDOrURL: "111"}, "miss")
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeNotFound || typed.Details["reason"] != "not_in_local_index" || typed.Details["scope"] != "local-index" {
		t.Fatalf("miss error=%#v", err)
	}
}

func TestListingRequiresStateAnd404IsUnavailable(t *testing.T) {
	transport := &listingAppTransport{status: http.StatusNotFound, body: listingAppFixture(t, "listing-unavailable.json")}
	withoutState := New(Dependencies{Transport: transport})
	if _, err := withoutState.ListingGet(context.Background(), domain.ListingGetInputV1{IDOrURL: "123"}, "missing-state"); err == nil || len(transport.requests) != 0 {
		t.Fatalf("missing state did not fail closed: err=%v requests=%d", err, len(transport.requests))
	}
	database, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	withState := New(Dependencies{Transport: transport, State: database})
	_, err = withState.ListingGet(context.Background(), domain.ListingGetInputV1{IDOrURL: "123"}, "unavailable")
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeUnavailable || domain.ExitCode(err) != 4 || len(transport.requests) != 1 {
		t.Fatalf("404 error=%#v requests=%d", err, len(transport.requests))
	}
}
