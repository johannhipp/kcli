package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/secret"
	"github.com/johannhipp/kcli/internal/state"
)

type authTestClock struct{ now time.Time }

func (c *authTestClock) Now() time.Time { return c.now }
func (c *authTestClock) Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type authTestSecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *authTestSecrets) Get(profile, name string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[profile+"\x00"+name]
	if !ok {
		return "", secret.ErrNotFound
	}
	return value, nil
}
func (s *authTestSecrets) Set(profile, name, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[profile+"\x00"+name] = value
	return nil
}
func (s *authTestSecrets) Delete(profile, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := profile + "\x00" + name
	if _, ok := s.values[key]; !ok {
		return secret.ErrNotFound
	}
	delete(s.values, key)
	return nil
}

type authTestHTTPTransport struct {
	server *httptest.Server
	config kleinanzeigen.AuthConfiguration
}

func (t *authTestHTTPTransport) AuthConfiguration() kleinanzeigen.AuthConfiguration { return t.config }
func (t *authTestHTTPTransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	u, err := url.Parse(t.server.URL)
	if err != nil {
		return kleinanzeigen.Response{}, err
	}
	u.Path = request.Path
	query := u.Query()
	for key, values := range request.Query {
		for _, value := range values {
			query.Add(key, value)
		}
	}
	u.RawQuery = query.Encode()
	httpRequest, err := http.NewRequestWithContext(request.Context, request.Method, u.String(), bytes.NewReader(request.Body))
	if err != nil {
		return kleinanzeigen.Response{}, err
	}
	for key, values := range request.Headers {
		for _, value := range values {
			httpRequest.Header.Add(key, value)
		}
	}
	response, err := t.server.Client().Do(httpRequest)
	if err != nil {
		return kleinanzeigen.Response{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return kleinanzeigen.Response{}, err
	}
	return kleinanzeigen.Response{StatusCode: response.StatusCode, Headers: response.Header, Body: body}, nil
}

func TestAuthLoginPersistsSplitSessionAndResolvesAccount(t *testing.T) {
	now := time.Unix(2_000_000_000, 0).UTC()
	var tokenRequests atomic.Int32
	var capturedChallenge, capturedVerifier string
	var capturedNonce atomic.Value
	capturedNonce.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenRequests.Add(1)
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			capturedVerifier = body["code_verifier"]
			digest := sha256.Sum256([]byte(body["code_verifier"]))
			if got := base64.RawURLEncoding.EncodeToString(digest[:]); got != capturedChallenge {
				t.Errorf("PKCE challenge = %q, want %q", capturedChallenge, got)
			}
			claims := map[string]any{
				"iss": "https://issuer.invalid/", "aud": "mobile-client", "exp": now.Add(time.Hour).Unix(),
				"nonce": capturedNonce.Load(), "email": "person@example.invalid", "sub": "auth0|subject-1",
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "access-secret", "refresh_token": "refresh-secret",
				"id_token": authAppJWT(t, claims), "expires_in": 3600,
			})
		case "/api/users/person@example.invalid/profile.json":
			if r.Header.Get("X-EBAYK-USERID-TOKEN") != "access-secret" {
				t.Errorf("profile access header missing")
			}
			if got := r.Header.Get("X-ECG-Authorization-User"); !strings.Contains(got, "email=person@example.invalid") || !strings.Contains(got, "access=access-secret") {
				t.Errorf("profile user header = %q", got)
			}
			_, _ = w.Write([]byte(`{"data":{"id":"987654"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	application, store, database := authTestApp(t, server, now)
	result, err := application.AuthLogin(context.Background(), "default", "request-1", domain.AuthLoginInputV1{NoOpen: true, RedirectFile: "-"}, func(authorizeURL string) (string, error) {
		u, err := url.Parse(authorizeURL)
		if err != nil {
			return "", err
		}
		capturedChallenge = u.Query().Get("code_challenge")
		capturedNonce.Store(u.Query().Get("nonce"))
		return kleinanzeigen.AuthRedirectURI + "?state=" + url.QueryEscape(u.Query().Get("state")) + "&code=single-use-code", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if tokenRequests.Load() != 1 || result.Data["account_id"] != "987654" || result.Data["status"] != "logged_in" {
		t.Fatalf("login result = %#v, token requests = %d", result.Data, tokenRequests.Load())
	}
	for name, want := range map[string]string{
		AuthSecretAccessToken: "access-secret", AuthSecretRefreshToken: "refresh-secret",
		AuthSecretEmail: "person@example.invalid", AuthSecretExpiry: now.Add(time.Hour).Format(time.RFC3339Nano),
	} {
		got, err := store.Get("default", name)
		if err != nil || got != want {
			t.Errorf("secret %s = %q, %v", name, got, err)
		}
	}
	if len(store.values) != 4 {
		t.Fatalf("stored secret entries = %d, want exactly 4 (no ID token)", len(store.values))
	}
	account, exists, err := database.AuthAccount(context.Background())
	if err != nil || !exists || account.AccountID != "987654" || len(account.SubjectHash) != 64 {
		t.Fatalf("account = %#v, exists = %v, err = %v", account, exists, err)
	}
	encoded, _ := json.Marshal(result)
	for _, forbidden := range []string{"access-secret", "refresh-secret", "single-use-code", capturedVerifier, "person@example.invalid"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("login output leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestAuthLoginRejectsStateBeforeTokenRequest(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	}))
	defer server.Close()
	application, store, _ := authTestApp(t, server, time.Unix(2_000_000_000, 0).UTC())
	_, err := application.AuthLogin(context.Background(), "default", "request-bad-state", domain.AuthLoginInputV1{}, func(string) (string, error) {
		return kleinanzeigen.AuthRedirectURI + "?state=attacker&code=single-use-code", nil
	})
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeInvalidInput {
		t.Fatalf("error = %#v", err)
	}
	if requests.Load() != 0 || len(store.values) != 0 {
		t.Fatalf("requests = %d, stored secrets = %d", requests.Load(), len(store.values))
	}
}

func TestAuthStatusRefreshesAfter401AndPersistsRotation(t *testing.T) {
	now := time.Unix(2_000_000_000, 0).UTC()
	var tokenRequests atomic.Int32
	var profileRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenRequests.Add(1)
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["grant_type"] != "refresh_token" || body["refresh_token"] != "refresh-old" {
				t.Errorf("refresh body = %#v", body)
			}
			_, _ = w.Write([]byte(`{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`))
		case "/api/users/person@example.invalid/profile.json":
			profileRequests.Add(1)
			if r.Header.Get("X-EBAYK-USERID-TOKEN") == "access-old" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if r.Header.Get("X-EBAYK-USERID-TOKEN") != "access-new" {
				t.Errorf("access header = %q", r.Header.Get("X-EBAYK-USERID-TOKEN"))
			}
			_, _ = w.Write([]byte(`{"id":987654}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	application, store, database := authTestApp(t, server, now)
	authSeedSession(t, store, database, "access-old", "refresh-old", now.Add(time.Hour))
	result, err := application.AuthStatus(context.Background(), "default", "request-2", domain.AuthStatusInputV1{Check: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Data["remote"] != "ok" || tokenRequests.Load() != 1 || profileRequests.Load() != 2 {
		t.Fatalf("status = %#v, token=%d profile=%d", result.Data, tokenRequests.Load(), profileRequests.Load())
	}
	if refresh, _ := store.Get("default", AuthSecretRefreshToken); refresh != "refresh-new" {
		t.Fatalf("rotated refresh token = %q", refresh)
	}
	if access, _ := store.Get("default", AuthSecretAccessToken); access != "access-new" {
		t.Fatalf("refreshed access token = %q", access)
	}
}

func TestAuthStatusNeverRefreshesAfter403(t *testing.T) {
	now := time.Unix(2_000_000_000, 0).UTC()
	var tokenRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/token" {
			tokenRequests.Add(1)
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	application, store, database := authTestApp(t, server, now)
	authSeedSession(t, store, database, "access-old", "refresh-old", now.Add(time.Hour))
	_, err := application.AuthStatus(context.Background(), "default", "request-3", domain.AuthStatusInputV1{Check: true})
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeAuthRevoked || tokenRequests.Load() != 0 {
		t.Fatalf("error = %#v, token requests = %d", err, tokenRequests.Load())
	}
}

func TestAuthInvalidGrantClearsUsableSession(t *testing.T) {
	now := time.Unix(2_000_000_000, 0).UTC()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer server.Close()
	application, store, database := authTestApp(t, server, now)
	authSeedSession(t, store, database, "access-old", "refresh-old", now.Add(30*time.Second))
	_, err := application.AuthStatus(context.Background(), "default", "request-4", domain.AuthStatusInputV1{Check: true})
	if !errors.Is(err, kleinanzeigen.ErrInvalidGrant) {
		t.Fatalf("error = %v", err)
	}
	for _, name := range authSecretNames() {
		if value, err := store.Get("default", name); !errors.Is(err, secret.ErrNotFound) {
			t.Errorf("secret %s retained as %q, err = %v", name, value, err)
		}
	}
	account, exists, err := database.AuthAccount(context.Background())
	if err != nil || !exists || !account.LoginRequired {
		t.Fatalf("account = %#v, exists = %v, err = %v", account, exists, err)
	}
	status, err := application.AuthStatus(context.Background(), "default", "request-5", domain.AuthStatusInputV1{})
	if err != nil || status.Data["status"] != "login_required" {
		t.Fatalf("local status = %#v, err = %v", status.Data, err)
	}
}

func TestAuthConcurrentExpiryUsesOneRefreshLease(t *testing.T) {
	now := time.Unix(2_000_000_000, 0).UTC()
	var tokenRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenRequests.Add(1)
			time.Sleep(75 * time.Millisecond)
			_, _ = w.Write([]byte(`{"access_token":"access-new","refresh_token":"refresh-new","expires_in":3600}`))
		case "/api/users/person@example.invalid/profile.json":
			_, _ = w.Write([]byte(`{"id":"987654"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	application, store, database := authTestApp(t, server, now)
	authSeedSession(t, store, database, "access-old", "refresh-old", now.Add(30*time.Second))

	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	for i := range 2 {
		go func(index int) {
			<-start
			_, err := application.AuthStatus(context.Background(), "default", "request-concurrent-"+string(rune('a'+index)), domain.AuthStatusInputV1{Check: true})
			errorsCh <- err
		}(i)
	}
	close(start)
	for range 2 {
		if err := <-errorsCh; err != nil {
			t.Fatal(err)
		}
	}
	if tokenRequests.Load() != 1 {
		t.Fatalf("refresh requests = %d, want 1", tokenRequests.Load())
	}
}

func TestAuthLogoutDryRunThenClearsLocalSession(t *testing.T) {
	now := time.Unix(2_000_000_000, 0).UTC()
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	application, store, database := authTestApp(t, server, now)
	authSeedSession(t, store, database, "access", "refresh", now.Add(time.Hour))

	preview, err := application.AuthLogout(context.Background(), "default", "request-6", domain.AuthLogoutInputV1{DryRun: true})
	if err != nil || preview.Data["would_clear_local_session"] != true {
		t.Fatalf("preview = %#v, err = %v", preview.Data, err)
	}
	if _, err := store.Get("default", AuthSecretAccessToken); err != nil {
		t.Fatalf("dry run cleared token: %v", err)
	}
	result, err := application.AuthLogout(context.Background(), "default", "request-7", domain.AuthLogoutInputV1{})
	if err != nil || result.Data["cleared_local_session"] != true || result.Data["remote_revoked"] != false {
		t.Fatalf("logout = %#v, err = %v", result.Data, err)
	}
	for _, name := range authSecretNames() {
		if _, err := store.Get("default", name); !errors.Is(err, secret.ErrNotFound) {
			t.Errorf("secret %s remains", name)
		}
	}
	if _, exists, err := database.AuthAccount(context.Background()); err != nil || exists {
		t.Fatalf("account after logout: exists=%v err=%v", exists, err)
	}
}

func authTestApp(t *testing.T, server *httptest.Server, now time.Time) (*App, *authTestSecrets, *state.DB) {
	t.Helper()
	database, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	store := &authTestSecrets{values: map[string]string{}}
	transport := &authTestHTTPTransport{server: server, config: kleinanzeigen.AuthConfiguration{ClientID: "mobile-client", Issuer: "https://issuer.invalid/"}}
	return New(Dependencies{Transport: transport, State: database, Secrets: store, Clock: &authTestClock{now: now}}), store, database
}

func authSeedSession(t *testing.T, store secret.SecretStore, database *state.DB, access, refresh string, expiry time.Time) {
	t.Helper()
	for name, value := range map[string]string{
		AuthSecretAccessToken: access, AuthSecretRefreshToken: refresh,
		AuthSecretEmail: "person@example.invalid", AuthSecretExpiry: expiry.Format(time.RFC3339Nano),
	} {
		if err := store.Set("default", name, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.AuthStoreAccount(context.Background(), strings.Repeat("d", 64), "987654"); err != nil {
		t.Fatal(err)
	}
}

func authAppJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "none"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload) + ".unsigned"
}
