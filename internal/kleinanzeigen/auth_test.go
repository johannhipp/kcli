package kleinanzeigen

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

func TestAuthAuthorizationAndCodeExchange(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost || r.URL.Path != "/oauth/token" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected request: %s %s %#v", r.Method, r.URL.Path, r.Header)
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"grant_type": "authorization_code", "client_id": "mobile-client", "code": "single-use-code",
			"code_verifier": "verifier", "redirect_uri": AuthRedirectURI,
		}
		if !authStringMapsEqual(body, want) {
			t.Errorf("request body = %#v, want %#v", body, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"access-value","refresh_token":"refresh-value","id_token":"id-value","expires_in":3600}`))
	}))
	defer server.Close()

	transport := authTestHTTPTransport(t, server)
	token, err := AuthExchangeCode(context.Background(), transport, AuthConfiguration{ClientID: "mobile-client", Issuer: "https://issuer.invalid/"}, "single-use-code", "verifier")
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("token requests = %d, want 1", requests.Load())
	}
	if token.AccessToken != "access-value" || token.RefreshToken != "refresh-value" || token.ExpiresIn != time.Hour {
		t.Fatalf("token = %#v", token)
	}

	authorize, err := AuthAuthorizationURL(AuthConfiguration{ClientID: "mobile-client", Issuer: "https://issuer.invalid/"}, "challenge", "state", "nonce")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(authorize)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "https" || u.Host != "login.kleinanzeigen.de" || u.Path != "/authorize" {
		t.Fatalf("authorization URL = %q", authorize)
	}
	wantQuery := map[string]string{
		"client_id": "mobile-client", "response_type": "code", "redirect_uri": AuthRedirectURI,
		"scope": AuthScope, "code_challenge": "challenge", "code_challenge_method": "S256",
		"state": "state", "nonce": "nonce", "prompt": "login",
	}
	for key, want := range wantQuery {
		if got := u.Query()[key]; len(got) != 1 || got[0] != want {
			t.Errorf("query %s = %#v, want %q", key, got, want)
		}
	}
}

func TestAuthValidateIDTokenClaims(t *testing.T) {
	now := time.Unix(2_000_000_000, 0).UTC()
	config := AuthConfiguration{ClientID: "mobile-client", Issuer: "https://issuer.invalid/"}
	valid := map[string]any{"iss": config.Issuer, "aud": config.ClientID, "exp": now.Add(time.Minute).Unix(), "nonce": "expected", "email": "person@example.invalid", "sub": "auth0|account"}

	identity, err := AuthValidateIDToken(authTestJWT(t, valid), config, "expected", now)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Email != "person@example.invalid" || len(identity.SubjectHash) != 64 || strings.Contains(identity.SubjectHash, "account") {
		t.Fatalf("identity = %#v", identity)
	}

	tests := []struct {
		name   string
		mutate func(map[string]any)
		code   domain.ErrorCode
	}{
		{"issuer", func(c map[string]any) { c["iss"] = "https://issuer.invalid" }, domain.CodeAuthRequired},
		{"audience", func(c map[string]any) { c["aud"] = "other-client" }, domain.CodeAuthRequired},
		{"multiple audiences", func(c map[string]any) { c["aud"] = []string{config.ClientID, "other"} }, domain.CodeAuthRequired},
		{"nonce", func(c map[string]any) { c["nonce"] = "wrong" }, domain.CodeAuthRequired},
		{"expiry", func(c map[string]any) { c["exp"] = now.Unix() }, domain.CodeAuthExpired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claims := make(map[string]any, len(valid))
			for key, value := range valid {
				claims[key] = value
			}
			test.mutate(claims)
			_, err := AuthValidateIDToken(authTestJWT(t, claims), config, "expected", now)
			var typed *domain.Error
			if !errors.As(err, &typed) || typed.Code != test.code {
				t.Fatalf("error = %#v, want code %s", err, test.code)
			}
		})
	}
}

func TestAuthParseRedirectStrictly(t *testing.T) {
	valid := AuthRedirectURI + "?state=expected&code=one"
	if code, err := AuthParseRedirect(valid, "expected"); err != nil || code != "one" {
		t.Fatalf("valid redirect = %q, %v", code, err)
	}
	tests := []struct {
		name string
		raw  string
		code domain.ErrorCode
	}{
		{"state", AuthRedirectURI + "?state=wrong&code=one", domain.CodeInvalidInput},
		{"host", "https://attacker.invalid/android/com.ebay.kleinanzeigen/callback?state=expected&code=one", domain.CodeInvalidInput},
		{"port", "https://login.kleinanzeigen.de:443/android/com.ebay.kleinanzeigen/callback?state=expected&code=one", domain.CodeInvalidInput},
		{"path", "https://login.kleinanzeigen.de/android/com.ebay.kleinanzeigen/other?state=expected&code=one", domain.CodeInvalidInput},
		{"duplicate code", AuthRedirectURI + "?state=expected&code=one&code=two", domain.CodeInvalidInput},
		{"oauth error", AuthRedirectURI + "?state=expected&error=access_denied", domain.CodeAuthRequired},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := AuthParseRedirect(test.raw, "expected")
			var typed *domain.Error
			if !errors.As(err, &typed) || typed.Code != test.code {
				t.Fatalf("error = %#v, want code %s", err, test.code)
			}
		})
	}
}

func TestAuthRefreshInvalidGrantAndProfileResolution(t *testing.T) {
	var tokenRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenRequests.Add(1)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"redacted"}`))
		case "/api/users/person@example.invalid/profile.json":
			if r.Header.Get("X-EBAYK-USERID-TOKEN") != "access-value" || !strings.Contains(r.Header.Get("X-ECG-Authorization-User"), "access=access-value") {
				t.Errorf("authenticated headers missing: %#v", r.Header)
			}
			_, _ = w.Write([]byte(`{"data":{"id":123456}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	transport := authTestHTTPTransport(t, server)
	_, err := AuthRefresh(context.Background(), transport, AuthConfiguration{ClientID: "mobile-client", Issuer: "https://issuer.invalid/"}, "refresh-value")
	if !errors.Is(err, ErrInvalidGrant) || tokenRequests.Load() != 1 {
		t.Fatalf("refresh error = %v, requests = %d", err, tokenRequests.Load())
	}
	id, err := AuthResolveProfileID(context.Background(), transport, "access-value", "person@example.invalid")
	if err != nil || id != "123456" {
		t.Fatalf("profile ID = %q, %v", id, err)
	}
}

func TestAuthTokenEndpointErrorDoesNotExposeResponseSecrets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"access_token":"must-not-appear","refresh_token":"also-secret","email":"person@example.invalid"}`))
	}))
	defer server.Close()
	_, err := AuthExchangeCode(context.Background(), authTestHTTPTransport(t, server), AuthConfiguration{ClientID: "mobile-client", Issuer: "https://issuer.invalid/"}, "secret-code", "secret-verifier")
	text := strings.ToLower(err.Error())
	for _, forbidden := range []string{"must-not-appear", "also-secret", "person@example.invalid", "secret-code", "secret-verifier"} {
		if strings.Contains(text, strings.ToLower(forbidden)) {
			t.Fatalf("error leaked %q: %v", forbidden, err)
		}
	}
}

type authHeaderRecorder struct{ request Request }

func (r *authHeaderRecorder) Do(request Request) (Response, error) {
	r.request = request
	return Response{StatusCode: http.StatusOK}, nil
}

func TestAuthenticatedTransportSeparatesHostHeaders(t *testing.T) {
	recorder := &authHeaderRecorder{}
	transport := AuthenticatedTransport(recorder, "access-value", "person@example.invalid")
	if _, err := transport.Do(Request{
		Host: HostMain,
		Headers: map[string][]string{
			"Authorization":            {"Basic distribution"},
			"X-EBAYK-USERID-TOKEN":     {"stale"},
			"X-ECG-Authorization-User": {"stale"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if got := recorder.request.Headers["Authorization"]; len(got) != 1 || got[0] != "Basic distribution" {
		t.Fatalf("main Authorization = %#v", got)
	}
	if got := recorder.request.Headers["X-EBAYK-USERID-TOKEN"]; len(got) != 1 || got[0] != "access-value" {
		t.Fatalf("main user token = %#v", got)
	}
	if got := recorder.request.Headers["X-ECG-Authorization-User"]; len(got) != 1 || got[0] != "email=person@example.invalid,access=access-value" {
		t.Fatalf("main user authorization = %#v", got)
	}

	if _, err := transport.Do(Request{
		Host: HostGateway,
		Headers: map[string][]string{
			"Authorization":        {"Basic stale"},
			"X-EBAYK-USERID-TOKEN": {"stale"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if got := recorder.request.Headers["Authorization"]; len(got) != 1 || got[0] != "Bearer access-value" {
		t.Fatalf("gateway Authorization = %#v", got)
	}
	if _, present := recorder.request.Headers["X-EBAYK-USERID-TOKEN"]; present {
		t.Fatalf("gateway retained main-host token header")
	}

	if _, err := transport.Do(Request{Host: HostLogin, Headers: map[string][]string{"Authorization": {"Bearer stale"}, "X-EBAYK-USERID-TOKEN": {"stale"}}}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.request.Headers) != 0 {
		t.Fatalf("login headers retained credentials: %#v", recorder.request.Headers)
	}
}

func authTestHTTPTransport(t *testing.T, server *httptest.Server) Transport {
	t.Helper()
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	return newHTTPTransport(server.Client(), map[Host]*url.URL{HostLogin: base, HostMain: base, HostGateway: base})
}

func authTestJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]any{"alg": "none", "typ": "JWT"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + ".unsigned"
}

func authStringMapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range right {
		if left[key] != value {
			return false
		}
	}
	return true
}
