package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/state"
)

type publicSellerTestTransport struct {
	*listingAppTransport
	sellerCalls    []string
	inventoryCalls []string
	seller         domain.SellerV1
	page           kleinanzeigen.SearchPage
	err            error
}

func (t *publicSellerTestTransport) PublicSeller(_ context.Context, id string) (domain.SellerV1, error) {
	t.sellerCalls = append(t.sellerCalls, id)
	return t.seller, t.err
}
func (t *publicSellerTestTransport) PublicSellerListings(_ context.Context, id string) (kleinanzeigen.SearchPage, error) {
	t.inventoryCalls = append(t.inventoryCalls, id)
	return t.page, t.err
}

func TestPublicSellerFallbackAndCachedHit(t *testing.T) {
	app, base, database := listingTestApp(t)
	defer database.Close()
	ctx := context.Background()
	if _, err := app.ListingGet(ctx, domain.ListingGetInputV1{IDOrURL: "1234567890"}, "seed"); err != nil {
		t.Fatal(err)
	}
	transport := &publicSellerTestTransport{listingAppTransport: base, seller: domain.SellerV1{ID: "42", Name: "Public Example", Source: "public-web", Completeness: domain.CompletenessBestEffort, Public: map[string]any{"contact-name": "Public Example"}}}
	app.Transport = transport
	cached, err := app.SellerGet(ctx, domain.SellerGetInputV1{IDOrURL: "987654321"}, "cached")
	if err != nil || len(transport.sellerCalls) != 0 || cached.Source != "local-index" || cached.Data.Name != "Händler Änne" {
		t.Fatalf("cached seller unexpectedly reached public transport: result=%#v err=%v calls=%v", cached, err, transport.sellerCalls)
	}
	fresh, err := app.SellerGet(ctx, domain.SellerGetInputV1{IDOrURL: "https://www.kleinanzeigen.de/s-bestandsliste.html?userId=42"}, "fresh")
	if err != nil || len(transport.sellerCalls) != 1 || transport.sellerCalls[0] != "42" || fresh.Data.ID != "42" || fresh.Source != "public-web" || fresh.Completeness != domain.CompletenessBestEffort || fresh.Data.ObservedAt.IsZero() {
		t.Fatalf("public fallback failed: result=%#v err=%v calls=%v", fresh, err, transport.sellerCalls)
	}
	found, err := app.SellerSearch(ctx, domain.SellerSearchInputV1{Name: "Public Example", Match: "exact"}, "rediscover")
	if err != nil || len(found.Data) != 1 || found.Data[0].ID != "42" {
		t.Fatalf("public profile was not indexed: result=%#v err=%v", found, err)
	}
	if _, err := app.SellerGet(ctx, domain.SellerGetInputV1{IDOrURL: "42"}, "cached-public"); err != nil || len(transport.sellerCalls) != 1 {
		t.Fatalf("known public profile refetched: err=%v calls=%v", err, transport.sellerCalls)
	}

}

func TestPublicSellerInventoryIsOneBoundedPage(t *testing.T) {
	app, base, database := listingTestApp(t)
	defer database.Close()
	transport := &publicSellerTestTransport{listingAppTransport: base, page: kleinanzeigen.SearchPage{Listings: []kleinanzeigen.SearchListing{
		{Summary: domain.ListingSummaryV1{ID: "100", Title: "First", Source: "public-web"}},
		{Summary: domain.ListingSummaryV1{ID: "101", Title: "Second", Source: "public-web"}},
	}, Total: 500, TotalKnown: true, Warnings: []domain.WarningV1{{Code: "fixture_warning", Message: "retained"}}}}
	app.Transport = transport
	result, err := app.SellerListings(context.Background(), domain.SellerListingsInputV1{IDOrURL: "42", Limit: 1}, "inventory")
	if err != nil || len(transport.inventoryCalls) != 1 || transport.inventoryCalls[0] != "42" || len(result.Data) != 1 || result.Data[0].ID != "100" || result.Data[0].ObservedAt.IsZero() || result.Source != "public-web" || result.Completeness != domain.CompletenessBestEffort {
		t.Fatalf("inventory bound or source lost: result=%#v err=%v calls=%v", result, err, transport.inventoryCalls)
	}
	if len(result.Warnings) != 2 || result.Warnings[0].Code != "fixture_warning" || result.Warnings[1].Code != "bounded_inventory" {
		t.Fatalf("inventory completeness warning missing: %#v", result.Warnings)
	}
	_, err = app.SellerListings(context.Background(), domain.SellerListingsInputV1{IDOrURL: "42", Limit: -1}, "invalid")
	if err == nil || len(transport.inventoryCalls) != 1 {
		t.Fatal("invalid bound reached website")
	}
}

func TestPublicSellerErrorsAreNotRetriedOrHiddenByCache(t *testing.T) {
	app, base, database := listingTestApp(t)
	defer database.Close()
	limited := &domain.Error{Code: domain.CodeRateLimited, Message: "fixture rate limit"}
	transport := &publicSellerTestTransport{listingAppTransport: base, err: limited}
	app.Transport = transport
	_, err := app.SellerGet(context.Background(), domain.SellerGetInputV1{IDOrURL: "42"}, "profile")
	if !errors.Is(err, limited) || len(transport.sellerCalls) != 1 {
		t.Fatalf("seller failure changed or retried: err=%v calls=%v", err, transport.sellerCalls)
	}
	_, err = app.SellerListings(context.Background(), domain.SellerListingsInputV1{IDOrURL: "42"}, "inventory")
	if !errors.Is(err, limited) || len(transport.inventoryCalls) != 1 {
		t.Fatalf("inventory failure changed or retried: err=%v calls=%v", err, transport.inventoryCalls)
	}
}

func TestSparsePublicInventoryPreservesSellerDetails(t *testing.T) {
	app, _, database := listingTestApp(t)
	defer database.Close()
	ctx := context.Background()
	if _, err := app.ListingGet(ctx, domain.ListingGetInputV1{IDOrURL: "1234567890"}, "seed"); err != nil {
		t.Fatal(err)
	}
	record := state.SearchSellerRecord{SellerID: "987654321", DisplayName: "", PublicJSON: json.RawMessage(`{"contact-name":"","user-rating":{"score":"","count":25},"company-name":null,"userBadges":[]}`), Source: "search", Completeness: "best-effort", ListingID: "111111", ListingTitle: "Other listing", ListingURL: "https://www.kleinanzeigen.de/s-anzeige/other/111111-217-1", ObservedAt: app.Clock.Now().Add(time.Second)}
	if err := database.SearchUpsertSellers(ctx, []state.SearchSellerRecord{record}); err != nil {
		t.Fatal(err)
	}
	result, err := app.SellerGet(ctx, domain.SellerGetInputV1{IDOrURL: "987654321"}, "after-preview")
	if err != nil || result.Data.Name != "Händler Änne" || result.Data.Rating == nil || result.Data.Rating.Score != "4.9" || result.Data.Rating.Count == nil || *result.Data.Rating.Count != 25 || len(result.Data.Badges) != 2 || result.Data.Company == nil || result.Data.Company.Name == "" {
		t.Fatalf("sparse preview erased richer metadata: %#v err=%v", result, err)
	}
	known, err := database.SellerListings(ctx, "987654321", 25)
	if err != nil || len(known) != 2 {
		t.Fatalf("new listing not indexed: %#v err=%v", known, err)
	}
}

func TestPublicSellerProfileRetentionAndValidation(t *testing.T) {
	app, _, database := listingTestApp(t)
	defer database.Close()
	ctx := context.Background()
	old := state.SellerSnapshot{ID: "1", FoldedName: "old", DisplayName: "Old", PublicJSON: json.RawMessage(`{}`), Source: "public-web", Completeness: "best-effort", ObservedAt: app.Clock.Now().Add(-31 * 24 * time.Hour)}
	if err := database.UpsertPublicSeller(ctx, old); err != nil {
		t.Fatal(err)
	}
	fresh := old
	fresh.ID = "2"
	fresh.ObservedAt = app.Clock.Now()
	if err := database.UpsertPublicSeller(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	search, err := app.SellerSearch(ctx, domain.SellerSearchInputV1{Name: "Old"}, "retention")
	if err != nil || len(search.Data) != 1 || search.Data[0].ID != "2" {
		t.Fatalf("profile retention failed: %#v err=%v", search, err)
	}
	fresh.DisplayName = ""
	if err := database.UpsertPublicSeller(ctx, fresh); err == nil {
		t.Fatal("invalid public seller snapshot accepted")
	}
}

func TestInventoryPreviewCannotRenameDetailedSeller(t *testing.T) {
	for _, source := range []string{"listing", "public-web"} {
		t.Run(source, func(t *testing.T) {
			app, base, database := listingTestApp(t)
			defer database.Close()
			ctx := context.Background()
			if err := database.UpsertPublicSeller(ctx, state.SellerSnapshot{ID: "42", FoldedName: sellerFoldName("Straße Name"), DisplayName: "Straße Name", PublicJSON: json.RawMessage(`{"contact-name":"Straße Name"}`), Source: source, Completeness: "direct", ObservedAt: app.Clock.Now()}); err != nil {
				t.Fatal(err)
			}
			app.Transport = &publicSellerTestTransport{listingAppTransport: base, page: kleinanzeigen.SearchPage{Listings: []kleinanzeigen.SearchListing{{Summary: domain.ListingSummaryV1{ID: "111111", Title: "Other listing", URL: "https://www.kleinanzeigen.de/s-anzeige/other/111111-217-1"}, Seller: kleinanzeigen.SearchSeller{ID: "42", Name: "Preview Alias", Raw: json.RawMessage(`{"contact-name":"Preview Alias"}`)}}}}}
			if _, err := app.SellerListings(ctx, domain.SellerListingsInputV1{IDOrURL: "42"}, "inventory"); err != nil {
				t.Fatal(err)
			}
			found, err := app.SellerSearch(ctx, domain.SellerSearchInputV1{Name: "Straße Name", Match: "exact"}, "rediscover")
			if err != nil || len(found.Data) != 1 || found.Data[0].Name != "Straße Name" || found.Data[0].Public["contact-name"] != "Straße Name" {
				t.Fatalf("inventory renamed encountered seller: result=%#v err=%v", found, err)
			}
			stored, err := database.SellerByID(ctx, "42")
			if err != nil || stored.Source != source || stored.Completeness != "direct" {
				t.Fatalf("inventory downgraded detailed seller provenance: %#v err=%v", stored, err)
			}
		})
	}
}
