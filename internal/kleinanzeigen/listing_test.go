package kleinanzeigen

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/johannhipp/kcli/internal/domain"
)

type listingTestTransport struct {
	response Response
	requests []Request
}

func (t *listingTestTransport) Do(request Request) (Response, error) {
	t.requests = append(t.requests, request)
	return t.response, nil
}

func listingFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "..", "testdata", "api", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestListingReferenceIDStrictForms(t *testing.T) {
	valid := map[string]string{
		"1234567890": "1234567890",
		"https://www.kleinanzeigen.de/s-anzeige/vintage-lampe/1234567890-82-1234": "1234567890",
		"https://kleinanzeigen.de/s-anzeige/x/1234567890-1-2/":                    "1234567890",
	}
	for input, want := range valid {
		got, err := ListingReferenceID(input)
		if err != nil || got != want {
			t.Errorf("ListingReferenceID(%q)=(%q,%v), want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"abc", "123/../../etc", "https://evil.invalid/s-anzeige/x/123-1-2", "http://www.kleinanzeigen.de/s-anzeige/x/123-1-2", "https://www.kleinanzeigen.de/s-anzeige/x/123%2f-1-2", "https://user@www.kleinanzeigen.de/s-anzeige/x/123-1-2", "https://www.kleinanzeigen.de/s-anzeige/x/123-1-2?q=x", "https://www.kleinanzeigen.de/s-anzeige/x/123-1-2#x", "https://www.kleinanzeigen.de:443/s-anzeige/x/123-1-2"} {
		if _, err := ListingReferenceID(input); err == nil {
			t.Errorf("unsafe reference accepted: %q", input)
		}
	}
}

func TestListingParseCoversDetailSellerMediaAndRedactedRaw(t *testing.T) {
	detail, err := ListingParse(listingFixture(t, "listing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if detail.Listing.ID != "1234567890" || detail.Listing.Title != "Vintage & sichere Lampe" || detail.Listing.Amount != "19.95" || detail.Listing.AmountCents == nil || *detail.Listing.AmountCents != 1995 || detail.Listing.Availability != "available" {
		t.Fatalf("listing normalization=%#v", detail.Listing)
	}
	if detail.Listing.URL != "https://www.kleinanzeigen.de/s-anzeige/vintage-lampe/1234567890-82-1234" || detail.Seller.ID != "987654321" || detail.Seller.Name != "Händler Änne" || detail.Seller.ProfileURL == "" {
		t.Fatalf("identity/seller normalization=%#v %#v", detail.Listing, detail.Seller)
	}
	if len(detail.Media) != 3 || detail.Media[0].Relation != "XXL" || detail.Media[2].Index != 1 {
		t.Fatalf("media=%#v", detail.Media)
	}
	for _, field := range []string{"category", "ad-type", "poster-type", "status", "labels", "start-date-time", "end-date-time", "view-count", "contract-warnings", "ad-address", "distance", "shipping", "pickup", "attributes", "pictures", "seller", "media", "availability"} {
		if _, ok := detail.Normalized[field]; !ok {
			t.Errorf("known fixture field %q was not normalized", field)
		}
	}
	raw := string(detail.Raw)
	if !strings.Contains(raw, "unknown-top-level-field") || !strings.Contains(raw, "unknown-attribute-field") {
		t.Fatalf("unknown fields absent from raw evidence: %s", raw)
	}
	if strings.Contains(raw, "fixture-token-must-not-appear") || strings.Contains(raw, "fixture.user@example.invalid") || !strings.Contains(raw, "[REDACTED]") {
		t.Fatalf("raw evidence was not redacted: %s", raw)
	}
}

func TestListingFetch404IsUnavailableAndNeverRetried(t *testing.T) {
	transport := &listingTestTransport{response: Response{StatusCode: http.StatusNotFound, Body: listingFixture(t, "listing-unavailable.json")}}
	_, err := ListingFetch(context.Background(), transport, "1234567890")
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeUnavailable || domain.ExitCode(err) != 4 || typed.Retryable {
		t.Fatalf("unexpected unavailable error: %#v", err)
	}
	if len(transport.requests) != 1 || transport.requests[0].Path != "/api/ads/1234567890.json" || transport.requests[0].Class != VolatileRead {
		t.Fatalf("requests=%#v", transport.requests)
	}
}

func TestListingReserved200IsSuccessfulPartialState(t *testing.T) {
	transport := &listingTestTransport{response: Response{StatusCode: http.StatusOK, Body: listingFixture(t, "listing-reserved.json")}}
	detail, err := ListingFetch(context.Background(), transport, "1234567890")
	if err != nil || detail.Listing.Availability != "reserved" {
		t.Fatalf("detail=%#v err=%v", detail, err)
	}
}

func TestListingFetchHTTPContractAndCredentialFailClosed(t *testing.T) {
	fixture := listingFixture(t, "listing.json")
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		if request.Method != http.MethodGet || (request.URL.Path != "/api/ads/1234567890.json" && request.URL.Path != "/api/ads/999.json") || request.Header.Get("Authorization") == "" || request.Header.Get("X-EBAYK-APP") != "fixture-install" {
			t.Errorf("unexpected request: %s %s headers=%v", request.Method, request.URL.Path, RedactHeaders(request.Header))
		}
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path == "/api/ads/999.json" {
			writer.WriteHeader(http.StatusNotFound)
			_, _ = writer.Write(listingFixture(t, "listing-unavailable.json"))
			return
		}
		_, _ = writer.Write(fixture)
	}))
	defer server.Close()
	baseURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	base := newHTTPTransport(server.Client(), map[Host]*url.URL{HostMain: baseURL})
	mobile := NewMobileTransport(base, "fixture-install", MobileCredentials{BasicUser: "fixture-user", BasicPassword: "fixture-password"}, nil)
	detail, err := ListingFetch(context.Background(), mobile, "1234567890")
	if err != nil || detail.Listing.ID != "1234567890" || calls.Load() != 1 {
		t.Fatalf("detail=%#v err=%v calls=%d", detail.Listing, err, calls.Load())
	}
	_, err = ListingFetch(context.Background(), mobile, "999")
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeUnavailable || calls.Load() != 2 {
		t.Fatalf("404 was retried or misclassified: err=%#v calls=%d", err, calls.Load())
	}
	missingCredentials := NewMobileTransport(base, "fixture-install", MobileCredentials{}, nil)
	_, err = ListingFetch(context.Background(), missingCredentials, "1234567890")
	if !errors.As(err, &typed) || typed.Code != domain.CodeUnavailable || calls.Load() != 2 {
		t.Fatalf("missing credentials did not fail closed: err=%#v calls=%d", err, calls.Load())
	}
}
