package kleinanzeigen

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Host string

const (
	HostMain    Host = "main"
	HostGateway Host = "gateway"
	HostLogin   Host = "login"
)

type Request struct {
	Context          context.Context
	Host             Host
	Method           string
	Path             string
	Query            map[string][]string
	Headers          map[string][]string
	Body             []byte
	MaxResponseBytes int64
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
	client *http.Client
	bases  map[Host]*url.URL
}

func NewHTTPTransport() *HTTPTransport {
	client := &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	bases := map[Host]*url.URL{
		HostMain:    mustURL("https://api.kleinanzeigen.de"),
		HostGateway: mustURL("https://gateway.kleinanzeigen.de"),
		HostLogin:   mustURL("https://login.kleinanzeigen.de"),
	}
	return &HTTPTransport{client: client, bases: bases}
}
func newHTTPTransport(client *http.Client, bases map[Host]*url.URL) *HTTPTransport {
	return &HTTPTransport{client: client, bases: bases}
}
func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func (t *HTTPTransport) Do(input Request) (Response, error) {
	base, ok := t.bases[input.Host]
	if !ok {
		return Response{}, fmt.Errorf("unknown transport host")
	}
	if err := validateRequestPath(input.Path); err != nil {
		return Response{}, err
	}
	if input.Context == nil {
		input.Context = context.Background()
	}
	u := *base
	u.Path = input.Path
	query := make(url.Values, len(input.Query))
	for key, values := range input.Query {
		if err := validateQueryKey(key); err != nil {
			return Response{}, err
		}
		for _, value := range values {
			if len(value) > 8192 || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
				return Response{}, fmt.Errorf("invalid query value")
			}
		}
		query[key] = append([]string(nil), values...)
	}
	u.RawQuery = query.Encode()
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
	resp, err := t.client.Do(req)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	limit := input.MaxResponseBytes
	if limit <= 0 {
		limit = 10 << 20
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return Response{}, fmt.Errorf("read response: %w", err)
	}
	if int64(len(body)) > limit {
		return Response{}, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return Response{StatusCode: resp.StatusCode, Headers: cloneHeaders(resp.Header), Body: body}, nil
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
