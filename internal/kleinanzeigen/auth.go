package kleinanzeigen

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

const (
	AuthRedirectURI = "https://login.kleinanzeigen.de/android/com.ebay.kleinanzeigen/callback"
	AuthScope       = "openid email profile offline_access"
	AuthIssuer      = "https://login.kleinanzeigen.de/"
)

var ErrInvalidGrant = errors.New("OAuth refresh grant is invalid")

type AuthConfiguration struct {
	ClientID string
	Issuer   string
}

type AuthConfigurationProvider interface {
	AuthConfiguration() AuthConfiguration
}

type AuthToken struct {
	AccessToken  string
	RefreshToken string
	IDToken      string
	ExpiresIn    time.Duration
}

type AuthIdentity struct {
	Email       string
	SubjectHash string
}

func (t *MobileTransport) AuthConfiguration() AuthConfiguration {
	return AuthConfiguration{ClientID: t.credentials.OAuthClientID, Issuer: AuthIssuer}
}

func AuthConfigurationFor(transport Transport) (AuthConfiguration, error) {
	provider, ok := transport.(AuthConfigurationProvider)
	if !ok {
		return AuthConfiguration{}, &domain.Error{Code: domain.CodeUnavailable, Message: "OAuth client configuration is unavailable"}
	}
	config := provider.AuthConfiguration()
	if err := authValidateConfiguration(config); err != nil {
		return AuthConfiguration{}, err
	}
	return config, nil
}

func authValidateConfiguration(config AuthConfiguration) error {
	if len(config.ClientID) > 2048 || strings.TrimSpace(config.ClientID) == "" || strings.IndexFunc(config.ClientID, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 || strings.TrimSpace(config.Issuer) == "" || !strings.HasSuffix(config.Issuer, "/") {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "OAuth client configuration is unavailable"}
	}
	issuer, err := url.Parse(config.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || issuer.RawQuery != "" || issuer.Fragment != "" {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "OAuth issuer configuration is invalid"}
	}
	return nil
}

func AuthAuthorizationURL(config AuthConfiguration, challenge, state, nonce string) (string, error) {
	if err := authValidateConfiguration(config); err != nil {
		return "", err
	}
	if challenge == "" || state == "" || nonce == "" {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "PKCE authorization parameters are incomplete"}
	}
	values := url.Values{
		"client_id":             {config.ClientID},
		"response_type":         {"code"},
		"redirect_uri":          {AuthRedirectURI},
		"scope":                 {AuthScope},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
		"nonce":                 {nonce},
		"prompt":                {"login"},
	}
	return "https://login.kleinanzeigen.de/authorize?" + values.Encode(), nil
}

func AuthParseRedirect(raw, expectedState string) (string, error) {
	if len(raw) == 0 || len(raw) > 64<<10 {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "authorization redirect is invalid"}
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host != "login.kleinanzeigen.de" || u.Path != "/android/com.ebay.kleinanzeigen/callback" || u.User != nil || u.Fragment != "" {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "authorization redirect does not match the fixed callback"}
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "authorization redirect query is invalid"}
	}
	states, statePresent := query["state"]
	if !statePresent || len(states) != 1 || subtle.ConstantTimeCompare([]byte(states[0]), []byte(expectedState)) != 1 {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "authorization redirect state does not match"}
	}
	if oauthErrors, present := query["error"]; present {
		if len(oauthErrors) != 1 || !authSafeOAuthError(oauthErrors[0]) {
			return "", &domain.Error{Code: domain.CodeAuthRequired, Message: "authorization was not completed"}
		}
		return "", &domain.Error{Code: domain.CodeAuthRequired, Message: "authorization was not completed", Details: map[string]any{"oauth_error": oauthErrors[0]}}
	}
	codes, present := query["code"]
	if !present || len(codes) != 1 || codes[0] == "" {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "authorization redirect must contain exactly one code"}
	}
	return codes[0], nil
}

func AuthExchangeCode(ctx context.Context, transport Transport, config AuthConfiguration, code, verifier string) (AuthToken, error) {
	if err := authValidateConfiguration(config); err != nil {
		return AuthToken{}, err
	}
	if code == "" || verifier == "" {
		return AuthToken{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "authorization code exchange is incomplete"}
	}
	body := struct {
		GrantType   string `json:"grant_type"`
		ClientID    string `json:"client_id"`
		Code        string `json:"code"`
		Verifier    string `json:"code_verifier"`
		RedirectURI string `json:"redirect_uri"`
	}{
		GrantType: "authorization_code", ClientID: config.ClientID, Code: code,
		Verifier: verifier, RedirectURI: AuthRedirectURI,
	}
	return authTokenRequest(ctx, transport, body, true)
}

func AuthRefresh(ctx context.Context, transport Transport, config AuthConfiguration, refreshToken string) (AuthToken, error) {
	if err := authValidateConfiguration(config); err != nil {
		return AuthToken{}, err
	}
	if refreshToken == "" {
		return AuthToken{}, &domain.Error{Code: domain.CodeAuthRequired, Message: "refresh token is unavailable"}
	}
	body := struct {
		GrantType    string `json:"grant_type"`
		ClientID     string `json:"client_id"`
		RefreshToken string `json:"refresh_token"`
	}{GrantType: "refresh_token", ClientID: config.ClientID, RefreshToken: refreshToken}
	return authTokenRequest(ctx, transport, body, false)
}

func authTokenRequest(ctx context.Context, transport Transport, payload any, requireRefresh bool) (AuthToken, error) {
	if transport == nil {
		return AuthToken{}, &domain.Error{Code: domain.CodeUnavailable, Message: "authentication transport is unavailable"}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return AuthToken{}, fmt.Errorf("encode OAuth request: %w", err)
	}
	response, err := transport.Do(Request{
		Context: ctx, Host: HostLogin, Method: http.MethodPost, Path: "/oauth/token",
		Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: body,
		MaxResponseBytes: 1 << 20, Class: AccountMutation, OneShot: true,
	})
	if err != nil {
		return AuthToken{}, AuthRequestError(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var oauthError struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(response.Body, &oauthError)
		if oauthError.Error == "invalid_grant" {
			return AuthToken{}, &domain.Error{Code: domain.CodeAuthExpired, Message: "login is required", Cause: ErrInvalidGrant}
		}
		if oauthError.Error != "" {
			details := map[string]any{"status": response.StatusCode}
			if authSafeOAuthError(oauthError.Error) {
				details["oauth_error"] = oauthError.Error
			}
			return AuthToken{}, &domain.Error{Code: domain.CodeAuthRequired, Message: "OAuth token exchange was rejected", Details: details}
		}
		return AuthToken{}, &domain.Error{
			Code: domain.CodeUpstream, Message: "OAuth token endpoint failed",
			Retryable: response.StatusCode >= http.StatusInternalServerError,
			Details:   map[string]any{"status": response.StatusCode},
		}
	}
	var wire struct {
		AccessToken  string          `json:"access_token"`
		RefreshToken string          `json:"refresh_token"`
		IDToken      string          `json:"id_token"`
		ExpiresIn    json.RawMessage `json:"expires_in"`
	}
	if err := json.Unmarshal(response.Body, &wire); err != nil {
		return AuthToken{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "OAuth token response is invalid"}
	}
	expires, err := authExpiresIn(wire.ExpiresIn)
	if err != nil || wire.AccessToken == "" || (requireRefresh && wire.RefreshToken == "") {
		return AuthToken{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "OAuth token response is incomplete"}
	}
	return AuthToken{AccessToken: wire.AccessToken, RefreshToken: wire.RefreshToken, IDToken: wire.IDToken, ExpiresIn: expires}, nil
}

func authExpiresIn(raw json.RawMessage) (time.Duration, error) {
	if len(raw) == 0 {
		return 0, errors.New("missing expiry")
	}
	var seconds int64
	if err := json.Unmarshal(raw, &seconds); err != nil || seconds <= 0 || seconds > int64((365*24*time.Hour)/time.Second) {
		return 0, errors.New("invalid expiry")
	}
	return time.Duration(seconds) * time.Second, nil
}

func AuthValidateIDToken(raw string, config AuthConfiguration, expectedNonce string, now time.Time) (AuthIdentity, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || len(parts[1]) > 64<<10 {
		return AuthIdentity{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "OIDC ID token is invalid"}
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(claimsJSON) > 64<<10 {
		return AuthIdentity{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "OIDC ID token claims are invalid"}
	}
	var claims struct {
		Issuer   string          `json:"iss"`
		Audience json.RawMessage `json:"aud"`
		Expiry   json.Number     `json:"exp"`
		Nonce    string          `json:"nonce"`
		Email    string          `json:"email"`
		Subject  string          `json:"sub"`
	}
	decoder := json.NewDecoder(bytes.NewReader(claimsJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&claims); err != nil {
		return AuthIdentity{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "OIDC ID token claims are invalid"}
	}
	if claims.Issuer != config.Issuer {
		return AuthIdentity{}, &domain.Error{Code: domain.CodeAuthRequired, Message: "OIDC issuer does not match"}
	}
	if !authAudienceEqual(claims.Audience, config.ClientID) {
		return AuthIdentity{}, &domain.Error{Code: domain.CodeAuthRequired, Message: "OIDC audience does not match"}
	}
	expiry, err := claims.Expiry.Int64()
	if err != nil || expiry <= now.UTC().Unix() {
		return AuthIdentity{}, &domain.Error{Code: domain.CodeAuthExpired, Message: "OIDC ID token has expired"}
	}
	if subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(expectedNonce)) != 1 {
		return AuthIdentity{}, &domain.Error{Code: domain.CodeAuthRequired, Message: "OIDC nonce does not match"}
	}
	if claims.Email == "" || claims.Subject == "" || strings.ContainsAny(claims.Email, "\r\n\x00") {
		return AuthIdentity{}, &domain.Error{Code: domain.CodeUpstreamContract, Message: "OIDC identity claims are incomplete"}
	}
	subjectDigest := authSHA256(claims.Subject)
	return AuthIdentity{Email: claims.Email, SubjectHash: subjectDigest}, nil
}

func authAudienceEqual(raw json.RawMessage, clientID string) bool {
	var audience string
	if json.Unmarshal(raw, &audience) == nil {
		return audience == clientID
	}
	var audiences []string
	if json.Unmarshal(raw, &audiences) != nil || len(audiences) != 1 {
		return false
	}
	return audiences[0] == clientID
}

func authSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%x", sum)
}

var authNumericID = regexp.MustCompile(`^[1-9][0-9]{0,39}$`)

func authSafeOAuthError(value string) bool {
	switch value {
	case "access_denied", "consent_required", "interaction_required", "invalid_client",
		"invalid_grant", "invalid_request", "invalid_scope", "login_required",
		"server_error", "temporarily_unavailable", "unauthorized_client",
		"unsupported_grant_type", "unsupported_response_type":
		return true
	default:
		return false
	}
}

func AuthResolveProfileID(ctx context.Context, transport Transport, accessToken, email string) (string, error) {
	if accessToken == "" || email == "" {
		return "", &domain.Error{Code: domain.CodeAuthRequired, Message: "authenticated profile identity is unavailable"}
	}
	response, err := AuthenticatedTransport(transport, accessToken, email).Do(Request{
		Context: ctx, Host: HostMain, Method: http.MethodGet,
		Path:             "/api/users/" + url.PathEscape(email) + "/profile.json",
		MaxResponseBytes: 1 << 20, Class: StableRead,
	})
	if err != nil {
		return "", AuthRequestError(err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", AuthResponseError(response)
	}
	return AuthProfileID(response.Body)
}

func AuthResponseError(response Response) error {
	details := map[string]any{"status": response.StatusCode}
	switch response.StatusCode {
	case http.StatusUnauthorized:
		return &domain.Error{Code: domain.CodeAuthExpired, Message: "authenticated session was rejected", Details: details}
	case http.StatusForbidden:
		return &domain.Error{Code: domain.CodeAuthRevoked, Message: "authenticated request was forbidden", Details: details}
	case http.StatusNotFound:
		return &domain.Error{Code: domain.CodeNotFound, Message: "authenticated resource was not found", Details: details}
	case http.StatusTooManyRequests:
		return &domain.Error{Code: domain.CodeRateLimited, Message: "authenticated request was rate limited", Retryable: true, Details: details}
	default:
		if response.StatusCode >= http.StatusInternalServerError {
			return &domain.Error{Code: domain.CodeUpstream, Message: "authenticated upstream request failed", Retryable: true, Details: details}
		}
		return &domain.Error{Code: domain.CodeUpstreamContract, Message: "authenticated upstream request was rejected", Details: details}
	}
}

func AuthRequestError(err error) error {
	if err == nil {
		return nil
	}
	var typed *domain.Error
	if errors.As(err, &typed) {
		message := "authenticated request failed"
		switch typed.Code {
		case domain.CodeInterrupted:
			message = "authenticated request was interrupted"
		case domain.CodeUnavailable:
			message = "authentication runtime is unavailable"
		case domain.CodeConnectivity:
			message = "authenticated service is unavailable"
		case domain.CodeRateLimited, domain.CodeRateLimitedLocal:
			message = "authenticated request was rate limited"
		}
		return &domain.Error{Code: typed.Code, Message: message, Retryable: typed.Retryable, RetryAfter: typed.RetryAfter}
	}
	return &domain.Error{Code: domain.CodeConnectivity, Message: "authenticated service is unavailable", Retryable: true}
}

func AuthProfileID(body []byte) (string, error) {
	var wire struct {
		ID   json.RawMessage `json:"id"`
		Data struct {
			ID json.RawMessage `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return "", &domain.Error{Code: domain.CodeUpstreamContract, Message: "authenticated profile response is invalid"}
	}
	id := authNumericJSON(wire.Data.ID)
	if id == "" {
		id = authNumericJSON(wire.ID)
	}
	if !authNumericID.MatchString(id) {
		return "", &domain.Error{Code: domain.CodeUpstreamContract, Message: "authenticated profile response has no numeric account ID"}
	}
	return id, nil
}

func authNumericJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	var number json.Number
	if json.Unmarshal(raw, &number) == nil {
		return number.String()
	}
	return ""
}

func AuthenticatedTransport(transport Transport, accessToken, email string) Transport {
	if mobile, ok := transport.(*MobileTransport); ok {
		copy := *mobile
		copy.credentials.AccessToken = accessToken
		copy.credentials.Email = email
		return &copy
	}
	return &authHeaderTransport{base: transport, accessToken: accessToken, email: email}
}

type authHeaderTransport struct {
	base        Transport
	accessToken string
	email       string
}

func (t *authHeaderTransport) Do(request Request) (Response, error) {
	if t.base == nil {
		return Response{}, &domain.Error{Code: domain.CodeUnavailable, Message: "authentication transport is unavailable"}
	}
	request.Headers = authCloneHeaders(request.Headers)
	switch request.Host {
	case HostMain:
		deleteHeaderFold(request.Headers, "X-EBAYK-USERID-TOKEN")
		deleteHeaderFold(request.Headers, "X-ECG-Authorization-User")
		request.Headers["X-EBAYK-USERID-TOKEN"] = []string{t.accessToken}
		request.Headers["X-ECG-Authorization-User"] = []string{"email=" + t.email + ",access=" + t.accessToken}
	case HostGateway:
		deleteHeaderFold(request.Headers, "X-EBAYK-USERID-TOKEN")
		deleteHeaderFold(request.Headers, "X-ECG-Authorization-User")
		deleteHeaderFold(request.Headers, "Authorization")
		request.Headers["Authorization"] = []string{"Bearer " + t.accessToken}
	case HostLogin, HostMedia:
		deleteHeaderFold(request.Headers, "Authorization")
		deleteHeaderFold(request.Headers, "X-EBAYK-USERID-TOKEN")
		deleteHeaderFold(request.Headers, "X-ECG-Authorization-User")
	}
	return t.base.Do(request)
}

func authCloneHeaders(source map[string][]string) map[string][]string {
	result := make(map[string][]string, len(source)+2)
	for key, values := range source {
		result[key] = append([]string(nil), values...)
	}
	return result
}
