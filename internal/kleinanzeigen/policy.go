package kleinanzeigen

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

type OperationClass string

const (
	StableRead        OperationClass = "stable-read"
	VolatileRead      OperationClass = "volatile-read"
	StateTouchingRead OperationClass = "state-touching-read"
	AccountMutation   OperationClass = "account-mutation"
	ExternalCreate    OperationClass = "external-create"
	ExternalSend      OperationClass = "external-send"
)

const (
	AppVersion               = "2026.25.0"
	JSONResponseLimit  int64 = 10 << 20
	ErrorResponseLimit int64 = 4 << 10
)

type MobileCredentials struct {
	BasicUser     string
	BasicPassword string
	AccessToken   string
	Email         string
	OAuthClientID string
}

type rateStore interface {
	ReserveRateSlot(context.Context, string, time.Time, time.Duration, time.Duration) (time.Duration, error)
	MoveRateSlot(context.Context, string, time.Time) error
}

type policyClock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type realPolicyClock struct{}

func (realPolicyClock) Now() time.Time { return time.Now().UTC() }
func (realPolicyClock) Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type MobileTransport struct {
	base        Transport
	installID   string
	credentials MobileCredentials
	rate        rateStore
	clock       policyClock
	jitter      func() time.Duration
}

func NewMobileTransport(base Transport, installID string, credentials MobileCredentials, rate rateStore) *MobileTransport {
	if base == nil {
		base = NewHTTPTransport()
	}
	return &MobileTransport{base: base, installID: installID, credentials: credentials, rate: rate, clock: realPolicyClock{}, jitter: func() time.Duration { return time.Duration(rand.IntN(251)) * time.Millisecond }}
}

func (t *MobileTransport) Do(input Request) (Response, error) {
	if input.Context == nil {
		input.Context = context.Background()
	}
	requestContext, cancel := requestDeadline(input.Context, input.Host, input.Timeout)
	defer cancel()
	input.Context = requestContext
	if input.Class == "" {
		input.Class = StateTouchingRead
	}
	if err := t.validateIdentity(input.Host); err != nil {
		return Response{}, err
	}
	input.MaxResponseBytes = boundedJSONLimit(input.MaxResponseBytes)
	input.Headers = t.mobileHeaders(input.Host, input.Headers)
	attempts := 1
	if input.Class == StableRead || input.Class == VolatileRead {
		attempts = 3
	}
	var lastErr error
	for attempt := range attempts {
		if err := input.Context.Err(); err != nil {
			return Response{}, contextOperationError(err)
		}
		if err := t.reserve(input.Context, input.Host, input.OneShot); err != nil {
			return Response{}, err
		}
		response, err := t.base.Do(input)
		if err != nil {
			lastErr = err
			if attempt+1 >= attempts || !retryableTransportError(input.Context, err) {
				break
			}
			if err := t.clock.Sleep(input.Context, retryBackoff(attempt)); err != nil {
				return Response{}, contextOperationError(err)
			}
			continue
		}
		if !retryableStatus(response.StatusCode, input.Class) || attempt+1 >= attempts {
			return response, nil
		}
		delay := retryDelay(response.Headers, attempt, t.clock.Now())
		if t.rate != nil && delay > 0 {
			if err := t.rate.MoveRateSlot(input.Context, string(input.Host), t.clock.Now().Add(delay)); err != nil {
				return Response{}, fmt.Errorf("advance rate slot: %w", err)
			}
		}
		if err := t.clock.Sleep(input.Context, delay); err != nil {
			return Response{}, contextOperationError(err)
		}
	}
	if err := input.Context.Err(); err != nil {
		return Response{}, contextOperationError(err)
	}
	return Response{}, &domain.Error{Code: domain.CodeConnectivity, Message: "mobile API request failed", Retryable: true, Cause: lastErr}
}

func requestDeadline(ctx context.Context, host Host, requested time.Duration) (context.Context, context.CancelFunc) {
	ceiling := 25 * time.Second
	switch host {
	case HostLogin:
		ceiling = 30 * time.Second
	case HostMedia:
		ceiling = 60 * time.Second
	}
	if requested > 0 && requested < ceiling {
		ceiling = requested
	}
	return context.WithTimeout(ctx, ceiling)
}

func boundedJSONLimit(limit int64) int64 {
	if limit <= 0 || limit > JSONResponseLimit {
		return JSONResponseLimit
	}
	return limit
}

func (t *MobileTransport) mobileHeaders(host Host, supplied map[string][]string) map[string][]string {
	headers := make(map[string][]string, len(supplied)+8)
	for key, values := range supplied {
		headers[key] = append([]string(nil), values...)
	}
	headers["Accept"] = []string{"application/json"}
	headers["Accept-Language"] = []string{"de-DE"}
	headers["User-Agent"] = []string{"Kleinanzeigen/" + AppVersion + " (Android 13; Pixel 7)"}
	headers["X-ECG-USER-AGENT"] = []string{"ebayk-android-app-" + AppVersion}
	headers["X-ECG-USER-VERSION"] = []string{AppVersion}
	if t.installID != "" {
		headers["X-EBAYK-APP"] = []string{t.installID}
	}
	switch host {
	case HostMain:
		if t.credentials.BasicUser != "" || t.credentials.BasicPassword != "" {
			value := base64.StdEncoding.EncodeToString([]byte(t.credentials.BasicUser + ":" + t.credentials.BasicPassword))
			headers["Authorization"] = []string{"Basic " + value}
		}
		if t.credentials.AccessToken != "" {
			headers["X-EBAYK-USERID-TOKEN"] = []string{t.credentials.AccessToken}
			if t.credentials.Email != "" {
				headers["X-ECG-Authorization-User"] = []string{"email=" + t.credentials.Email + ",access=" + t.credentials.AccessToken}
			}
		}
	case HostGateway:
		if t.credentials.AccessToken != "" {
			headers["Authorization"] = []string{"Bearer " + t.credentials.AccessToken}
		}
	case HostLogin, HostMedia:
		deleteHeaderFold(headers, "Authorization")
		deleteHeaderFold(headers, "X-EBAYK-USERID-TOKEN")
		deleteHeaderFold(headers, "X-ECG-Authorization-User")
	}
	return headers
}

func deleteHeaderFold(headers map[string][]string, name string) {
	for key := range headers {
		if strings.EqualFold(key, name) {
			delete(headers, key)
		}
	}
}

func (t *MobileTransport) reserve(ctx context.Context, host Host, oneShot bool) error {
	if t.rate == nil {
		return nil
	}
	maxQueue := time.Duration(0)
	if oneShot {
		maxQueue = 30 * time.Second
	}
	wait, err := t.rate.ReserveRateSlot(ctx, string(host), t.clock.Now(), t.jitter(), maxQueue)
	if err != nil {
		return err
	}
	if wait <= 0 {
		return nil
	}
	if err := t.clock.Sleep(ctx, wait); err != nil {
		return contextOperationError(err)
	}
	return nil
}

func (t *MobileTransport) validateIdentity(host Host) error {
	switch host {
	case HostMain:
		if t.installID == "" || t.credentials.BasicUser == "" || t.credentials.BasicPassword == "" {
			return &domain.Error{Code: domain.CodeUnavailable, Message: "mobile distribution credentials or install identity are unavailable"}
		}
	case HostGateway:
		if t.installID == "" || t.credentials.AccessToken == "" {
			return &domain.Error{Code: domain.CodeAuthRequired, Message: "gateway access requires an authenticated mobile session"}
		}
	}
	return nil
}

func retryableTransportError(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var marked *connectError
	return errors.As(err, &marked)
}

func retryableStatus(status int, class OperationClass) bool {
	if class != StableRead && class != VolatileRead {
		return false
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusNotFound {
		return false
	}
	return status == http.StatusTooManyRequests || status == http.StatusInternalServerError || status == http.StatusServiceUnavailable
}

func retryBackoff(attempt int) time.Duration {
	return time.Duration(attempt+1) * 200 * time.Millisecond
}

func retryDelay(headers map[string][]string, attempt int, now time.Time) time.Duration {
	for key, values := range headers {
		if !strings.EqualFold(key, "Retry-After") || len(values) == 0 {
			continue
		}
		value := strings.TrimSpace(values[0])
		if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
			return time.Duration(seconds) * time.Second
		}
		if when, err := http.ParseTime(value); err == nil && when.After(now) {
			return when.Sub(now)
		}
	}
	return retryBackoff(attempt)
}

func contextOperationError(err error) error {
	if errors.Is(err, context.Canceled) {
		return &domain.Error{Code: domain.CodeInterrupted, Message: "operation canceled", Cause: err}
	}
	return &domain.Error{Code: domain.CodeConnectivity, Message: "mobile API request timed out", Retryable: true, Cause: err}
}

func ResponseError(response Response) error {
	body := strings.TrimSpace(string(response.Body))
	body = RedactText(body)
	if int64(len(body)) > ErrorResponseLimit {
		body = body[:int(ErrorResponseLimit)]
	}
	details := map[string]any{"status": response.StatusCode}
	if body != "" {
		details["body"] = body
	}
	switch response.StatusCode {
	case http.StatusNotFound:
		return &domain.Error{Code: domain.CodeNotFound, Message: "mobile API resource was not found", Details: details}
	case http.StatusUnauthorized, http.StatusForbidden:
		return &domain.Error{Code: domain.CodeUpstreamContract, Message: "mobile API authorization contract was rejected", Details: details}
	case http.StatusTooManyRequests:
		retry := retryDelay(response.Headers, 0, time.Now().UTC())
		return &domain.Error{Code: domain.CodeRateLimited, Message: "mobile API rate limit reached", Retryable: true, RetryAfter: &retry, Details: details}
	default:
		return &domain.Error{Code: domain.CodeUpstream, Message: "mobile API request failed", Retryable: response.StatusCode >= 500, Details: details}
	}
}
