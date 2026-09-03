package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/secret"
	"github.com/johannhipp/kcli/internal/state"
)

const (
	AuthSecretRefreshToken = "auth.refresh_token"
	AuthSecretAccessToken  = "auth.access_token"
	AuthSecretEmail        = "auth.email"
	AuthSecretExpiry       = "auth.expiry"
)

type AuthRedirectCapture func(authorizeURL string) (string, error)

type authSession struct {
	RefreshToken string
	AccessToken  string
	Email        string
	Expiry       time.Time
}

type authSecretValue struct {
	value  string
	exists bool
}

func (a *App) AuthLogin(ctx context.Context, profile, requestID string, _ domain.AuthLoginInputV1, capture AuthRedirectCapture) (domain.AuthOutputV1, error) {
	if err := authDependencies(a); err != nil {
		return domain.AuthOutputV1{}, err
	}
	if capture == nil {
		return domain.AuthOutputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "authorization redirect capture is unavailable"}
	}
	config, err := kleinanzeigen.AuthConfigurationFor(a.Transport)
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	verifier, err := authRandomValue()
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	stateValue, err := authRandomValue()
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	nonce, err := authRandomValue()
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	challengeDigest := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeDigest[:])
	authorizeURL, err := kleinanzeigen.AuthAuthorizationURL(config, challenge, stateValue, nonce)
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	redirect, err := capture(authorizeURL)
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	code, err := kleinanzeigen.AuthParseRedirect(redirect, stateValue)
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	token, err := kleinanzeigen.AuthExchangeCode(ctx, a.Transport, config, code, verifier)
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	identity, err := kleinanzeigen.AuthValidateIDToken(token.IDToken, config, nonce, a.Clock.Now())
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	accountID, err := kleinanzeigen.AuthResolveProfileID(ctx, a.Transport, token.AccessToken, identity.Email)
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	expiresAt := a.Clock.Now().UTC().Add(token.ExpiresIn)
	session := authSession{RefreshToken: token.RefreshToken, AccessToken: token.AccessToken, Email: identity.Email, Expiry: expiresAt}
	before, err := authSnapshotSecrets(a.Secrets, profile)
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	if err := authWriteSession(a.Secrets, profile, session); err != nil {
		_ = authRestoreSecrets(a.Secrets, profile, before)
		return domain.AuthOutputV1{}, err
	}
	if err := a.State.AuthStoreAccount(ctx, identity.SubjectHash, accountID); err != nil {
		_ = authRestoreSecrets(a.Secrets, profile, before)
		return domain.AuthOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "store authenticated account state", Cause: err}
	}
	data := map[string]any{
		"status": "logged_in", "logged_in": true, "account_id": accountID,
		"expires_at": expiresAt, "refreshable": true, "remote_revoked": false,
	}
	return authOutput(a.Clock, requestID, "kleinanzeigen", data), nil
}

func (a *App) AuthStatus(ctx context.Context, profile, requestID string, input domain.AuthStatusInputV1) (domain.AuthOutputV1, error) {
	if err := authDependencies(a); err != nil {
		return domain.AuthOutputV1{}, err
	}
	session, account, exists, err := authReadLocal(a, ctx, profile)
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	if !exists {
		data := map[string]any{"status": "logged_out", "logged_in": false, "checked": false, "refreshable": false}
		return authOutput(a.Clock, requestID, "local", data), nil
	}
	status := "logged_in"
	if account.LoginRequired {
		status = "login_required"
	}
	data := map[string]any{
		"status": status, "logged_in": !account.LoginRequired, "checked": false,
		"account_id": account.AccountID, "refreshable": session.RefreshToken != "",
	}
	if !session.Expiry.IsZero() {
		data["expires_at"] = session.Expiry
		data["access_token_current"] = session.AccessToken != "" && session.Expiry.After(a.Clock.Now())
	}
	if !input.Check {
		return authOutput(a.Clock, requestID, "local", data), nil
	}
	if account.LoginRequired {
		return domain.AuthOutputV1{}, &domain.Error{Code: domain.CodeAuthRequired, Message: "login is required"}
	}
	response, err := a.AuthDo(ctx, profile, requestID, kleinanzeigen.Request{
		Host: kleinanzeigen.HostMain, Method: http.MethodGet,
		Path:             "/api/users/" + url.PathEscape(session.Email) + "/profile.json",
		MaxResponseBytes: 1 << 20, Class: kleinanzeigen.StableRead,
	})
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	remoteID, err := kleinanzeigen.AuthProfileID(response.Body)
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	if subtle.ConstantTimeCompare([]byte(remoteID), []byte(account.AccountID)) != 1 {
		return domain.AuthOutputV1{}, &domain.Error{Code: domain.CodeAuthRevoked, Message: "authenticated account identity changed"}
	}
	data["status"] = "logged_in"
	data["logged_in"] = true
	data["checked"] = true
	data["remote"] = "ok"
	return authOutput(a.Clock, requestID, "kleinanzeigen", data), nil
}

func (a *App) AuthLogout(ctx context.Context, profile, requestID string, input domain.AuthLogoutInputV1) (domain.AuthOutputV1, error) {
	if err := authDependencies(a); err != nil {
		return domain.AuthOutputV1{}, err
	}
	entries, err := authPresentSecrets(a.Secrets, profile)
	if err != nil {
		return domain.AuthOutputV1{}, err
	}
	preview, err := a.State.AuthLogoutPreview(ctx)
	if err != nil {
		return domain.AuthOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "preview local logout", Cause: err}
	}
	data := map[string]any{
		"status": "logged_out", "logged_in": false, "dry_run": input.DryRun,
		"secret_entries": entries, "account_state_present": preview.HadAccount,
		"next_cursor_generation": preview.NextGeneration, "remote_revoked": false,
	}
	if input.DryRun {
		data["would_clear_local_session"] = true
		return authOutput(a.Clock, requestID, "local", data), nil
	}
	if err := authDeleteSecrets(a.Secrets, profile); err != nil {
		return domain.AuthOutputV1{}, err
	}
	effect, err := a.State.AuthLogout(ctx)
	if err != nil {
		return domain.AuthOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "clear local authenticated state", Cause: err}
	}
	data["next_cursor_generation"] = effect.NextGeneration
	data["cleared_local_session"] = true
	return authOutput(a.Clock, requestID, "local", data), nil
}

// AuthDo applies the current user session, refreshes one minute before expiry,
// and performs at most one forced refresh-and-retry after a 401 response.
func (a *App) AuthDo(ctx context.Context, profile, requestID string, request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	if err := authDependencies(a); err != nil {
		return kleinanzeigen.Response{}, err
	}
	session, err := authUsableSession(a, ctx, profile, requestID, "", false)
	if err != nil {
		return kleinanzeigen.Response{}, err
	}
	request.Context = ctx
	response, err := kleinanzeigen.AuthenticatedTransport(a.Transport, session.AccessToken, session.Email).Do(request)
	if err != nil {
		authErr := kleinanzeigen.AuthRequestError(err)
		if typed, ok := authErr.(*domain.Error); ok && kleinanzeigen.IsTransportPreWrite(err) {
			if typed.Details == nil {
				typed.Details = map[string]any{}
			}
			typed.Details["pre_write"] = true
		}
		return kleinanzeigen.Response{}, authErr
	}
	if response.StatusCode == http.StatusForbidden {
		return kleinanzeigen.Response{}, &domain.Error{Code: domain.CodeAuthRevoked, Message: "authenticated request was forbidden"}
	}
	if response.StatusCode != http.StatusUnauthorized {
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return kleinanzeigen.Response{}, kleinanzeigen.AuthResponseError(response)
		}
		return response, nil
	}
	// External create/send must never be retried: a 401 is surfaced for
	// reconciliation/login and the request is not repeated after a refresh.
	if request.Class == kleinanzeigen.ExternalCreate || request.Class == kleinanzeigen.ExternalSend {
		authInvalidateSession(a, ctx, profile)
		return kleinanzeigen.Response{}, &domain.Error{Code: domain.CodeAuthExpired, Message: "authenticated session is required to send; no retry was attempted"}
	}
	session, err = authUsableSession(a, ctx, profile, requestID, session.AccessToken, true)
	if err != nil {
		return kleinanzeigen.Response{}, err
	}
	response, err = kleinanzeigen.AuthenticatedTransport(a.Transport, session.AccessToken, session.Email).Do(request)
	if err != nil {
		return kleinanzeigen.Response{}, kleinanzeigen.AuthRequestError(err)
	}
	if response.StatusCode == http.StatusForbidden {
		return kleinanzeigen.Response{}, &domain.Error{Code: domain.CodeAuthRevoked, Message: "authenticated request was forbidden"}
	}
	if response.StatusCode == http.StatusUnauthorized {
		authInvalidateSession(a, ctx, profile)
		return kleinanzeigen.Response{}, &domain.Error{Code: domain.CodeAuthExpired, Message: "authenticated session was rejected after refresh"}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return kleinanzeigen.Response{}, kleinanzeigen.AuthResponseError(response)
	}
	return response, nil
}

func authDependencies(a *App) error {
	if a == nil || a.Secrets == nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "OS secret store is unavailable"}
	}
	if a.State == nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "profile state is unavailable"}
	}
	if a.Transport == nil || a.Clock == nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "authentication runtime is unavailable"}
	}
	return nil
}

func authRandomValue() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", &domain.Error{Code: domain.CodeUnavailable, Message: "generate secure login parameter", Cause: err}
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func authOutput(clock Clock, requestID, source string, data map[string]any) domain.AuthOutputV1 {
	return domain.AuthOutputV1{Envelope: Envelope(clock, "kcli.auth/v1", requestID, source, data)}
}

func authReadLocal(a *App, ctx context.Context, profile string) (authSession, state.AuthAccountState, bool, error) {
	account, accountExists, err := a.State.AuthAccount(ctx)
	if err != nil {
		return authSession{}, state.AuthAccountState{}, false, &domain.Error{Code: domain.CodeUnavailable, Message: "read authenticated account state", Cause: err}
	}
	refresh, err := a.Secrets.Get(profile, AuthSecretRefreshToken)
	if errors.Is(err, secret.ErrNotFound) {
		if accountExists && account.LoginRequired {
			return authSession{}, account, true, nil
		}
		return authSession{}, account, false, nil
	}
	if err != nil {
		return authSession{}, state.AuthAccountState{}, false, &domain.Error{Code: domain.CodeUnavailable, Message: "read local authenticated session", Cause: err}
	}
	if !accountExists {
		return authSession{}, state.AuthAccountState{}, false, &domain.Error{Code: domain.CodeUnavailable, Message: "local authenticated session state is incomplete"}
	}
	session := authSession{RefreshToken: refresh}
	session.AccessToken, err = authOptionalSecret(a.Secrets, profile, AuthSecretAccessToken)
	if err != nil {
		return authSession{}, state.AuthAccountState{}, false, err
	}
	session.Email, err = authOptionalSecret(a.Secrets, profile, AuthSecretEmail)
	if err != nil {
		return authSession{}, state.AuthAccountState{}, false, err
	}
	expiry, err := authOptionalSecret(a.Secrets, profile, AuthSecretExpiry)
	if err != nil {
		return authSession{}, state.AuthAccountState{}, false, err
	}
	if expiry != "" {
		session.Expiry, err = time.Parse(time.RFC3339Nano, expiry)
		if err != nil {
			return authSession{}, state.AuthAccountState{}, false, &domain.Error{Code: domain.CodeUnavailable, Message: "local authenticated session expiry is invalid"}
		}
	}
	return session, account, true, nil
}

func authOptionalSecret(store secret.SecretStore, profile, name string) (string, error) {
	value, err := store.Get(profile, name)
	if errors.Is(err, secret.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", &domain.Error{Code: domain.CodeUnavailable, Message: "read local authenticated session", Cause: err}
	}
	return value, nil
}

func authUsableSession(a *App, ctx context.Context, profile, requestID, rejectedAccess string, force bool) (authSession, error) {
	session, account, exists, err := authReadLocal(a, ctx, profile)
	if err != nil {
		return authSession{}, err
	}
	if !exists || account.LoginRequired || session.RefreshToken == "" || session.Email == "" {
		return authSession{}, &domain.Error{Code: domain.CodeAuthRequired, Message: "login is required"}
	}
	if !force && session.AccessToken != "" && session.Expiry.After(a.Clock.Now().Add(time.Minute)) {
		return session, nil
	}
	config, err := kleinanzeigen.AuthConfigurationFor(a.Transport)
	if err != nil {
		return authSession{}, err
	}
	owner, err := authLeaseOwner(requestID)
	if err != nil {
		return authSession{}, err
	}
	for range 100 {
		acquired, err := a.State.AcquireLease(ctx, "auth-refresh", owner, 30*time.Second)
		if err != nil {
			return authSession{}, &domain.Error{Code: domain.CodeUnavailable, Message: "acquire session refresh lease", Cause: err}
		}
		if !acquired {
			if err := a.Clock.Sleep(ctx, 50*time.Millisecond); err != nil {
				return authSession{}, err
			}
			continue
		}
		defer a.State.ReleaseLease(context.Background(), "auth-refresh", owner)
		current, currentAccount, currentExists, err := authReadLocal(a, ctx, profile)
		if err != nil {
			return authSession{}, err
		}
		if !currentExists || currentAccount.LoginRequired || current.RefreshToken == "" || current.Email == "" {
			return authSession{}, &domain.Error{Code: domain.CodeAuthRequired, Message: "login is required"}
		}
		if rejectedAccess != "" && current.AccessToken != "" && subtle.ConstantTimeCompare([]byte(current.AccessToken), []byte(rejectedAccess)) != 1 {
			return current, nil
		}
		if !force && current.AccessToken != "" && current.Expiry.After(a.Clock.Now().Add(time.Minute)) {
			return current, nil
		}
		refreshed, err := kleinanzeigen.AuthRefresh(ctx, a.Transport, config, current.RefreshToken)
		if err != nil {
			if errors.Is(err, kleinanzeigen.AuthErrInvalidGrant) {
				authInvalidateSession(a, ctx, profile)
			}
			return authSession{}, err
		}
		if refreshed.RefreshToken != "" {
			current.RefreshToken = refreshed.RefreshToken
		}
		current.AccessToken = refreshed.AccessToken
		current.Expiry = a.Clock.Now().UTC().Add(refreshed.ExpiresIn)
		if err := authWriteSession(a.Secrets, profile, current); err != nil {
			return authSession{}, err
		}
		return current, nil
	}
	return authSession{}, &domain.Error{Code: domain.CodeUnavailable, Message: "session refresh is busy", Retryable: true}
}

func authLeaseOwner(requestID string) (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", &domain.Error{Code: domain.CodeUnavailable, Message: "generate session refresh lease owner", Cause: err}
	}
	hash := sha256.New()
	_, _ = hash.Write([]byte(requestID))
	_, _ = hash.Write(entropy[:])
	return "auth-" + base64.RawURLEncoding.EncodeToString(hash.Sum(nil)[:16]), nil
}

func authWriteSession(store secret.SecretStore, profile string, session authSession) error {
	if session.RefreshToken == "" || session.AccessToken == "" || session.Email == "" || session.Expiry.IsZero() {
		return &domain.Error{Code: domain.CodeUpstreamContract, Message: "authenticated session is incomplete"}
	}
	values := []struct{ name, value string }{
		{AuthSecretRefreshToken, session.RefreshToken},
		{AuthSecretAccessToken, session.AccessToken},
		{AuthSecretEmail, session.Email},
		{AuthSecretExpiry, session.Expiry.UTC().Format(time.RFC3339Nano)},
	}
	for _, item := range values {
		if err := store.Set(profile, item.name, item.value); err != nil {
			return &domain.Error{Code: domain.CodeUnavailable, Message: "store local authenticated session", Cause: err}
		}
	}
	return nil
}

func authSnapshotSecrets(store secret.SecretStore, profile string) (map[string]authSecretValue, error) {
	result := make(map[string]authSecretValue, 4)
	for _, name := range authSecretNames() {
		value, err := store.Get(profile, name)
		if errors.Is(err, secret.ErrNotFound) {
			result[name] = authSecretValue{}
			continue
		}
		if err != nil {
			return nil, &domain.Error{Code: domain.CodeUnavailable, Message: "read local authenticated session", Cause: err}
		}
		result[name] = authSecretValue{value: value, exists: true}
	}
	return result, nil
}

func authRestoreSecrets(store secret.SecretStore, profile string, snapshot map[string]authSecretValue) error {
	var first error
	for _, name := range authSecretNames() {
		value := snapshot[name]
		var err error
		if value.exists {
			err = store.Set(profile, name, value.value)
		} else {
			err = store.Delete(profile, name)
			if errors.Is(err, secret.ErrNotFound) {
				err = nil
			}
		}
		if first == nil && err != nil {
			first = err
		}
	}
	return first
}

func authDeleteSecrets(store secret.SecretStore, profile string) error {
	var first error
	for _, name := range authSecretNames() {
		err := store.Delete(profile, name)
		if errors.Is(err, secret.ErrNotFound) {
			err = nil
		}
		if first == nil && err != nil {
			first = err
		}
	}
	if first != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "clear local authenticated session", Cause: first}
	}
	return nil
}

func authPresentSecrets(store secret.SecretStore, profile string) ([]string, error) {
	var present []string
	for _, name := range authSecretNames() {
		_, err := store.Get(profile, name)
		if errors.Is(err, secret.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, &domain.Error{Code: domain.CodeUnavailable, Message: "read local authenticated session", Cause: err}
		}
		present = append(present, name)
	}
	return present, nil
}

func authSecretNames() []string {
	return []string{AuthSecretAccessToken, AuthSecretRefreshToken, AuthSecretEmail, AuthSecretExpiry}
}

func authInvalidateSession(a *App, ctx context.Context, profile string) {
	_ = a.State.AuthSetLoginRequired(ctx)
	_ = authDeleteSecrets(a.Secrets, profile)
}
