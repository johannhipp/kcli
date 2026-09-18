package kleinanzeigen

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/state"
)

const publicWebOrigin = "https://www.kleinanzeigen.de"

// WebTransport translates the internal discovery requests into verified public
// website reads. It never loads account credentials, cookies, or mobile headers.
type WebTransport struct {
	client      *http.Client
	database    *state.DB
	rate        rateStore
	clock       policyClock
	mu          sync.Mutex
	next        time.Time
	media       map[string]bool
	pages       map[string]map[int]string
	listingURLs map[string]string
}

func NewWebTransport(database *state.DB) *WebTransport {
	t := &WebTransport{client: &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, database: database, clock: realPolicyClock{}, media: map[string]bool{}, pages: map[string]map[int]string{}, listingURLs: map[string]string{}}
	if database != nil {
		t.rate = database
	}
	return t
}
func (*WebTransport) Source() string { return "public-web" }
func (t *WebTransport) reserve(ctx context.Context) error {
	jitter := time.Duration(rand.IntN(251)) * time.Millisecond
	var wait time.Duration
	if t.rate != nil {
		var err error
		wait, err = t.rate.ReserveRateSlot(ctx, "www.kleinanzeigen.de", t.clock.Now(), jitter, 30*time.Second)
		if err != nil {
			return err
		}
	} else {
		t.mu.Lock()
		now := t.clock.Now()
		if t.next.After(now) {
			wait = t.next.Sub(now)
		}
		t.next = now.Add(wait + 2500*time.Millisecond + jitter)
		t.mu.Unlock()
	}
	if wait > 0 {
		if err := t.clock.Sleep(ctx, wait); err != nil {
			return contextOperationError(err)
		}
	}
	return nil
}
func webURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "www.kleinanzeigen.de" || u.User != nil || u.Fragment != "" {
		return nil, &domain.Error{Code: domain.CodeInvalidInput, Message: "public website URL must use https://www.kleinanzeigen.de"}
	}
	return u, nil
}
func (t *WebTransport) fetch(ctx context.Context, raw string) (Response, string, error) {
	for redirects := 0; redirects < 5; redirects++ {
		u, err := webURL(raw)
		if err != nil {
			return Response{}, "", err
		}
		if err = t.reserve(ctx); err != nil {
			return Response{}, "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return Response{}, "", err
		}
		req.Header.Set("Accept-Language", "de-DE")
		req.Header.Set("Accept", "text/html,application/json")
		resp, err := t.client.Do(req)
		if err != nil {
			return Response{}, "", &domain.Error{Code: domain.CodeConnectivity, Message: "public website request failed; no retry was attempted", Cause: err}
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, JSONResponseLimit+1))
		resp.Body.Close()
		if readErr != nil {
			return Response{}, "", &domain.Error{Code: domain.CodeConnectivity, Message: "read public website response", Cause: readErr}
		}
		if int64(len(body)) > JSONResponseLimit {
			return Response{}, "", webContract("public website response exceeds size limit")
		}
		result := Response{StatusCode: resp.StatusCode, Headers: cloneHeaders(resp.Header), Body: body}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location, err := u.Parse(resp.Header.Get("Location"))
			if err != nil || resp.Header.Get("Location") == "" {
				return Response{}, "", webContract("public website redirect omitted a valid location")
			}
			raw = location.String()
			continue
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 || resp.StatusCode == 429 {
			if err := t.persistCooldown(ctx, result); err != nil {
				return Response{}, "", err
			}
			return Response{}, "", webResponseError(result)
		}
		lower := strings.ToLower(string(body))
		for _, marker := range []string{"ip-eingeschraenkt", "access denied", "verify you are human", "captcha-delivery.com", "cf-chl-"} {
			if strings.Contains(lower, marker) {
				return Response{}, "", &domain.Error{Code: domain.CodeUnavailable, Message: "public website access challenge; stopped without retry"}
			}
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			result.Body = nil
		}
		return result, u.String(), nil
	}
	return Response{}, "", webContract("public website redirect limit reached")
}
func webContract(message string) error {
	return &domain.Error{Code: domain.CodeUpstreamContract, Message: message}
}
func (t *WebTransport) Do(input Request) (Response, error) {
	if input.Method != http.MethodGet || input.Host != HostMain || len(input.Body) != 0 {
		return Response{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "anonymous website transport supports public GET requests only"}
	}
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var target string
	var parse func([]byte, string) ([]byte, error)
	switch {
	case input.Path == "/api/categories.json":
		target = publicWebOrigin + "/"
		parse = func(body []byte, _ string) ([]byte, error) { return parseWebCategories(body) }
	case input.Path == "/api/locations.json":
		target = publicWebOrigin + "/s-ort-empfehlungen.json?" + url.Values{"query": input.Query["q"]}.Encode()
		parse = func(body []byte, _ string) ([]byte, error) { return parseWebLocations(body) }
	case strings.HasPrefix(input.Path, "/api/ads/search-metadata/"):
		id := strings.TrimSuffix(strings.TrimPrefix(input.Path, "/api/ads/search-metadata/"), ".json")
		if !numericID(id) {
			return Response{}, webContract("invalid category identifier")
		}
		target = publicWebOrigin + "/s-suchanfrage.html?" + url.Values{"categoryId": {id}}.Encode()
		parse = func(body []byte, _ string) ([]byte, error) { return parseWebFilters(body) }
	case input.Path == "/api/ads.json":
		return t.search(ctx, input.Query)
	case strings.HasPrefix(input.Path, "/api/ads/") && strings.HasSuffix(input.Path, ".json"):
		id := strings.TrimSuffix(strings.TrimPrefix(input.Path, "/api/ads/"), ".json")
		if !numericID(id) {
			return Response{}, webContract("invalid listing identifier")
		}
		target = input.AbsoluteURL
		if target == "" {
			t.mu.Lock()
			target = t.listingURLs[id]
			t.mu.Unlock()
			if target == "" && t.database != nil {
				target, _ = t.database.PublicListingURL(ctx, id)
			}
		}
		returnedID, err := ListingReferenceID(target)
		if target == "" || err != nil || returnedID != id {
			return Response{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "listing ID has no encountered public URL; pass the complete website listing URL"}
		}
		parsedTarget, parseErr := url.Parse(target)
		if parseErr != nil {
			return Response{}, webInvalid("invalid public listing URL")
		}
		parsedTarget.Host = "www.kleinanzeigen.de"
		target = parsedTarget.String()
		parse = parseWebListing
	default:
		return Response{}, &domain.Error{Code: domain.CodeUnavailable, Message: "operation is outside anonymous website scope"}
	}
	response, finalURL, err := t.fetch(ctx, target)
	if err != nil {
		return Response{}, err
	}
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusNotFound && parse != nil && strings.HasPrefix(input.Path, "/api/ads/") && !strings.Contains(input.Path, "search-metadata") {
			return response, nil
		}
		return Response{}, webResponseError(response)
	}
	response.Body, err = parse(response.Body, finalURL)
	return response, err
}
func (t *WebTransport) AllowMediaURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "img.kleinanzeigen.de" || u.User != nil || u.Fragment != "" {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "media must use the exact public Kleinanzeigen image host"}
	}
	t.mu.Lock()
	t.media[raw] = true
	t.mu.Unlock()
	return nil
}
func (t *WebTransport) OpenMedia(input Request) (MediaResponse, error) {
	t.mu.Lock()
	allowed := t.media[input.AbsoluteURL]
	t.mu.Unlock()
	if !allowed || input.Method != http.MethodGet || input.Host != HostMedia {
		return MediaResponse{}, fmt.Errorf("media URL is not allowlisted")
	}
	ctx := input.Context
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	if err := t.reserve(ctx); err != nil {
		cancel()
		return MediaResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, input.AbsoluteURL, nil)
	if err != nil {
		cancel()
		return MediaResponse{}, err
	}
	resp, err := t.client.Do(req)
	if err != nil {
		cancel()
		return MediaResponse{}, &domain.Error{Code: domain.CodeConnectivity, Message: "public image request failed; no retry was attempted", Cause: err}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		response := Response{StatusCode: resp.StatusCode, Headers: cloneHeaders(resp.Header)}
		err := t.persistCooldown(ctx, response)
		cancel()
		if err != nil {
			return MediaResponse{}, err
		}
		return MediaResponse{}, webResponseError(response)
	}
	return MediaResponse{StatusCode: resp.StatusCode, Headers: cloneHeaders(resp.Header), Body: &listingCancelReadCloser{ReadCloser: resp.Body, cancel: cancel}}, nil
}
func webJSON(value any) ([]byte, error) { return json.Marshal(value) }

func transportSource(transport Transport) string {
	if source, ok := transport.(interface{ Source() string }); ok {
		return source.Source()
	}
	return "mobile-api"
}

func (t *WebTransport) persistCooldown(ctx context.Context, response Response) error {
	if response.StatusCode != http.StatusTooManyRequests || t.rate == nil {
		return nil
	}
	delay := retryDelay(response.Headers, 0, t.clock.Now())
	if delay < time.Minute {
		delay = time.Minute
	}
	return t.rate.MoveRateSlot(ctx, "www.kleinanzeigen.de", t.clock.Now().Add(delay))
}

// Website errors intentionally omit HTML response bodies: these may contain
// page tokens, listing prose, and other unrelated visitor data.
func webResponseError(response Response) error {
	response.Body = nil
	err := ResponseError(response).(*domain.Error)
	err.Message = strings.ReplaceAll(err.Message, "mobile API", "public website")
	return err
}
