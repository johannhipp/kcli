package kleinanzeigen

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Host string

const (
	HostMain    Host = "main"
	HostGateway Host = "gateway"
	HostLogin   Host = "login"
	HostMedia   Host = "media"
)

type Request struct {
	Context          context.Context
	Host             Host
	Method           string
	Path             string
	AbsoluteURL      string
	Query            map[string][]string
	Headers          map[string][]string
	Body             []byte
	MaxResponseBytes int64
	Timeout          time.Duration
	Class            OperationClass
	OneShot          bool
}
type Response struct {
	StatusCode int
	Headers    map[string][]string
	Body       []byte
}
type Transport interface {
	Do(Request) (Response, error)
}

type HTTPTransport struct {
	client         *http.Client
	bases          map[Host]*url.URL
	mu             sync.RWMutex
	mediaURLs      map[string]struct{}
	loginRedirects map[string]struct{}
}

func NewHTTPTransport() *HTTPTransport {
	client := &http.Client{Timeout: 30 * time.Second}
	bases := map[Host]*url.URL{
		HostMain:    mustURL("https://api.kleinanzeigen.de"),
		HostGateway: mustURL("https://gateway.kleinanzeigen.de"),
		HostLogin:   mustURL("https://login.kleinanzeigen.de"),
	}
	transport := newHTTPTransport(client, bases)
	_ = transport.AllowLoginRedirect("https://login.kleinanzeigen.de/android/com.ebay.kleinanzeigen/callback")
	return transport
}
func newHTTPTransport(client *http.Client, bases map[Host]*url.URL) *HTTPTransport {
	copied := make(map[Host]*url.URL, len(bases))
	for host, base := range bases {
		value := *base
		copied[host] = &value
	}
	return &HTTPTransport{client: client, bases: copied, mediaURLs: make(map[string]struct{}), loginRedirects: make(map[string]struct{})}
}
func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func (t *HTTPTransport) Do(input Request) (Response, error) {
	u, err := t.requestURL(input)
	if err != nil {
		return Response{}, err
	}
	if input.Context == nil {
		input.Context = context.Background()
	}
	if input.Method == "" {
		return Response{}, fmt.Errorf("request method is required")
	}
	req, err := http.NewRequestWithContext(input.Context, input.Method, u.String(), bytes.NewReader(input.Body))
	if err != nil {
		return Response{}, fmt.Errorf("create request: %w", err)
	}
	for key, values := range input.Headers {
		if strings.ContainsAny(key, "\r\n") {
			return Response{}, fmt.Errorf("invalid header name")
		}
		for _, value := range values {
			if strings.ContainsAny(value, "\r\n") {
				return Response{}, fmt.Errorf("invalid header value")
			}
			req.Header.Add(key, value)
		}
	}
	client := *t.client
	client.CheckRedirect = func(next *http.Request, _ []*http.Request) error {
		if t.redirectAllowed(input.Host, next.URL) {
			return nil
		}
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return Response{}, ConnectError(err)
	}
	defer resp.Body.Close()
	limit := input.MaxResponseBytes
	if limit <= 0 || limit > JSONResponseLimit {
		limit = JSONResponseLimit
	}
	isError := resp.StatusCode < 200 || resp.StatusCode >= 300
	if isError && limit > ErrorResponseLimit {
		limit = ErrorResponseLimit
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return Response{}, &afterResponseError{err: fmt.Errorf("read response: %w", err)}
	}
	if int64(len(body)) > limit {
		if !isError {
			return Response{}, &afterResponseError{err: fmt.Errorf("response exceeds %d bytes", limit)}
		}
		body = body[:limit]
	}
	if isError {
		body = []byte(RedactText(string(body)))
		if int64(len(body)) > ErrorResponseLimit {
			body = body[:int(ErrorResponseLimit)]
		}
	}
	return Response{StatusCode: resp.StatusCode, Headers: cloneHeaders(resp.Header), Body: body}, nil
}

type connectError struct{ err error }

func (e *connectError) Error() string { return e.err.Error() }
func (e *connectError) Unwrap() error { return e.err }

type afterResponseError struct{ err error }

func (e *afterResponseError) Error() string { return e.err.Error() }
func (e *afterResponseError) Unwrap() error { return e.err }

// ConnectError marks a failure that occurred before an HTTP response was
// received. Stable and volatile reads may retry only marked failures.
func ConnectError(err error) error {
	if err == nil {
		return nil
	}
	return &connectError{err: err}
}
func (t *HTTPTransport) requestURL(input Request) (*url.URL, error) {

	if input.Host == HostMedia {
		if input.AbsoluteURL == "" || input.Path != "" || len(input.Query) != 0 {
			return nil, fmt.Errorf("media requests require one exact absolute URL")
		}
		u, err := url.Parse(input.AbsoluteURL)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || !t.exactMediaAllowed(u.String()) {
			return nil, fmt.Errorf("media URL is not allowlisted")
		}
		return u, nil
	}
	base, ok := t.bases[input.Host]
	if !ok {
		return nil, fmt.Errorf("unknown transport host")
	}
	if input.AbsoluteURL != "" {
		return nil, fmt.Errorf("absolute URLs are only valid for media requests")
	}
	if err := validateRequestPath(input.Path); err != nil {
		return nil, err
	}
	u := *base
	u.Path = input.Path
	query := make(url.Values, len(input.Query))
	for key, values := range input.Query {
		if err := validateQueryKey(key); err != nil {
			return nil, err
		}
		for _, value := range values {
			if len(value) > 8192 || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
				return nil, fmt.Errorf("invalid query value")
			}
		}
		query[key] = append([]string(nil), values...)
	}
	u.RawQuery = query.Encode()
	return &u, nil
}

func (t *HTTPTransport) AllowMediaURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("media URL must be an exact HTTPS URL")
	}
	t.mu.Lock()
	t.mediaURLs[u.String()] = struct{}{}
	t.mu.Unlock()
	return nil
}

func (t *HTTPTransport) AllowLoginRedirect(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("login redirect must be an exact HTTPS URL")
	}
	t.mu.Lock()
	t.loginRedirects[u.String()] = struct{}{}
	t.mu.Unlock()
	return nil
}

func (t *HTTPTransport) exactMediaAllowed(raw string) bool {
	t.mu.RLock()
	_, ok := t.mediaURLs[raw]
	t.mu.RUnlock()
	return ok
}

func (t *HTTPTransport) redirectAllowed(host Host, target *url.URL) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	switch host {
	case HostLogin:
		_, ok := t.loginRedirects[target.String()]
		return ok
	case HostMedia:
		_, ok := t.mediaURLs[target.String()]
		return ok
	default:
		return false
	}
}
func validateRequestPath(path string) error {
	lower := strings.ToLower(path)
	if path == "" || path[0] != '/' || strings.ContainsAny(path, "\\?#\r\n\x00") || strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") || strings.Contains(lower, "%2e") {
		return fmt.Errorf("invalid request path")
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("invalid request path")
		}
	}
	return nil
}
func validateQueryKey(key string) error {
	if key == "" || strings.ContainsAny(key, "&=\r\n\x00") {
		return fmt.Errorf("invalid query key")
	}
	return nil
}
func cloneHeaders(headers http.Header) map[string][]string {
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func RedactHeaders(headers map[string][]string) map[string][]string {
	out := make(map[string][]string, len(headers))
	for key, values := range headers {
		switch strings.ToLower(key) {
		case "authorization", "cookie", "set-cookie", "x-ebayk-userid-token", "x-ecg-authorization-user":
			out[key] = []string{"[REDACTED]"}
		default:
			out[key] = append([]string(nil), values...)
		}
	}
	return out
}

var authorizationPattern = regexp.MustCompile(`(?i)\b(?:basic|bearer)\s+[A-Za-z0-9._~+/=-]+`)
var emailPathPattern = regexp.MustCompile(`(?i)[A-Z0-9._%+-]+(?:@|%40)[A-Z0-9.-]+(?:\.|%2e)[A-Z]{2,}`)

func RedactText(value string) string {
	value = authorizationPattern.ReplaceAllString(value, "[REDACTED_AUTHORIZATION]")
	return emailPathPattern.ReplaceAllString(value, "[REDACTED_EMAIL]")
}

func RedactURL(value string) string {
	return RedactText(value)
}
