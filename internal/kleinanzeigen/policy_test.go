package kleinanzeigen

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

type scriptedTransport struct {
	requests  []Request
	responses []Response
	errors    []error
}

func (s *scriptedTransport) Do(request Request) (Response, error) {
	s.requests = append(s.requests, request)
	index := len(s.requests) - 1
	var response Response
	if index < len(s.responses) {
		response = s.responses[index]
	}
	if index < len(s.errors) {
		return response, s.errors[index]
	}
	return response, nil
}

var testMobileCredentials = MobileCredentials{BasicUser: "distribution", BasicPassword: "password"}

type recordingClock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *recordingClock) Now() time.Time { return c.now }
func (c *recordingClock) Sleep(ctx context.Context, duration time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	c.sleeps = append(c.sleeps, duration)
	c.now = c.now.Add(duration)
	return nil
}

func TestMobileTransportHeadersAreSeparatedByHost(t *testing.T) {
	base := &scriptedTransport{responses: []Response{{StatusCode: 200}, {StatusCode: 200}, {StatusCode: 200}}}
	mobile := NewMobileTransport(base, "uuid-1234567890", MobileCredentials{BasicUser: "distribution", BasicPassword: "password", AccessToken: "bearer-secret", Email: "person@example.test"}, nil)
	for _, host := range []Host{HostMain, HostGateway, HostLogin} {
		if _, err := mobile.Do(Request{Context: context.Background(), Host: host, Method: http.MethodGet, Path: "/ok", Class: StateTouchingRead}); err != nil {
			t.Fatal(err)
		}
	}
	main := base.requests[0].Headers
	if !strings.HasPrefix(main["Authorization"][0], "Basic ") || main["X-EBAYK-USERID-TOKEN"][0] != "bearer-secret" || main["X-EBAYK-APP"][0] != "uuid-1234567890" {
		t.Fatalf("main headers: %#v", RedactHeaders(main))
	}
	gateway := base.requests[1].Headers
	if gateway["Authorization"][0] != "Bearer bearer-secret" {
		t.Fatalf("gateway headers: %#v", RedactHeaders(gateway))
	}
	login := base.requests[2].Headers
	if _, ok := login["Authorization"]; ok {
		t.Fatalf("login leaked authorization: %#v", RedactHeaders(login))
	}
}

func TestRetryPolicyStatusAndRetryAfter(t *testing.T) {
	tests := []struct {
		name         string
		class        OperationClass
		statuses     []int
		wantRequests int
		wantSleep    time.Duration
	}{
		{name: "stable 429", class: StableRead, statuses: []int{429, 200}, wantRequests: 2, wantSleep: 2 * time.Second},
		{name: "stable 500 twice", class: StableRead, statuses: []int{500, 503, 200}, wantRequests: 3, wantSleep: 600 * time.Millisecond},
		{name: "volatile 404", class: VolatileRead, statuses: []int{404, 200}, wantRequests: 1},
		{name: "never retry forbidden", class: StableRead, statuses: []int{403, 200}, wantRequests: 1},
		{name: "never retry auth", class: StableRead, statuses: []int{401, 200}, wantRequests: 1},
		{name: "external send", class: ExternalSend, statuses: []int{503, 200}, wantRequests: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base := &scriptedTransport{}
			for _, status := range test.statuses {
				headers := map[string][]string{}
				if status == 429 {
					headers["Retry-After"] = []string{"2"}
				}
				base.responses = append(base.responses, Response{StatusCode: status, Headers: headers})
			}
			clock := &recordingClock{now: time.Unix(100, 0)}
			mobile := NewMobileTransport(base, "install", testMobileCredentials, nil)
			mobile.clock = clock
			response, err := mobile.Do(Request{Context: context.Background(), Host: HostMain, Method: http.MethodGet, Path: "/fixture", Class: test.class})
			if err != nil {
				t.Fatal(err)
			}
			if len(base.requests) != test.wantRequests {
				t.Fatalf("requests=%d want %d status=%d", len(base.requests), test.wantRequests, response.StatusCode)
			}
			var slept time.Duration
			for _, duration := range clock.sleeps {
				slept += duration
			}
			if slept != test.wantSleep {
				t.Fatalf("slept=%s want %s", slept, test.wantSleep)
			}
		})
	}
}

func TestTransportErrorsRetryButCancellationDoesNot(t *testing.T) {
	base := &scriptedTransport{responses: []Response{{}, {}, {StatusCode: 200}}, errors: []error{ConnectError(errors.New("connect")), ConnectError(errors.New("connect"))}}
	clock := &recordingClock{now: time.Unix(100, 0)}
	mobile := NewMobileTransport(base, "install", testMobileCredentials, nil)
	mobile.clock = clock
	if _, err := mobile.Do(Request{Context: context.Background(), Host: HostMain, Method: http.MethodGet, Path: "/fixture", Class: StableRead}); err != nil {
		t.Fatal(err)
	}
	if len(base.requests) != 3 {
		t.Fatalf("expected retries, got %d", len(base.requests))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	base = &scriptedTransport{}
	mobile = NewMobileTransport(base, "install", testMobileCredentials, nil)
	_, err := mobile.Do(Request{Context: ctx, Host: HostMain, Method: http.MethodGet, Path: "/fixture", Class: StableRead})
	var typed interface{ Error() string }
	if err == nil || !errors.As(err, &typed) || len(base.requests) != 0 {
		t.Fatalf("cancellation not preserved: %v requests=%d", err, len(base.requests))
	}
}

func TestOnlyBeforeResponseErrorsAreRetried(t *testing.T) {
	base := &scriptedTransport{errors: []error{errors.New("decode after response")}}
	mobile := NewMobileTransport(base, "install", testMobileCredentials, nil)
	if _, err := mobile.Do(Request{Context: context.Background(), Host: HostMain, Method: http.MethodGet, Path: "/fixture", Class: StableRead}); err == nil {
		t.Fatal("expected transport error")
	}
	if len(base.requests) != 1 {
		t.Fatalf("unmarked error retried %d times", len(base.requests))
	}
}

func TestMobileIdentityIsRequiredBeforeNetwork(t *testing.T) {
	base := &scriptedTransport{}
	mobile := NewMobileTransport(base, "", MobileCredentials{}, nil)
	if _, err := mobile.Do(Request{Context: context.Background(), Host: HostMain, Method: http.MethodGet, Path: "/fixture", Class: StableRead}); err == nil {
		t.Fatal("missing identity accepted")
	}
	if len(base.requests) != 0 {
		t.Fatal("request sent without distribution identity")
	}
}

func TestHTTPTransportRedirectPolicyAndMediaAllowlist(t *testing.T) {
	destination := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { _, _ = writer.Write([]byte("ok")) }))
	defer destination.Close()
	redirect := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Location", destination.URL+"/callback")
		writer.WriteHeader(http.StatusFound)
	}))
	defer redirect.Close()
	base, _ := url.Parse(redirect.URL)
	transport := newHTTPTransport(redirect.Client(), map[Host]*url.URL{HostMain: base, HostLogin: base})
	main, err := transport.Do(Request{Context: context.Background(), Host: HostMain, Method: http.MethodGet, Path: "/start"})
	if err != nil || main.StatusCode != http.StatusFound {
		t.Fatalf("main redirect followed or failed: %#v %v", main, err)
	}
	login, err := transport.Do(Request{Context: context.Background(), Host: HostLogin, Method: http.MethodGet, Path: "/start"})
	if err != nil || login.StatusCode != http.StatusFound {
		t.Fatalf("unallowlisted login redirect followed: %#v %v", login, err)
	}
	if err := transport.AllowLoginRedirect(destination.URL + "/callback"); err != nil {
		t.Fatal(err)
	}
	login, err = transport.Do(Request{Context: context.Background(), Host: HostLogin, Method: http.MethodGet, Path: "/start"})
	if err != nil || login.StatusCode != http.StatusOK || string(login.Body) != "ok" {
		t.Fatalf("allowlisted login redirect failed: %#v %v", login, err)
	}
	if _, err := transport.Do(Request{Context: context.Background(), Host: HostMedia, Method: http.MethodGet, AbsoluteURL: destination.URL + "/callback"}); err == nil {
		t.Fatal("unallowlisted media URL accepted")
	}
	if err := transport.AllowMediaURL(destination.URL + "/callback"); err != nil {
		t.Fatal(err)
	}
	media, err := transport.Do(Request{Context: context.Background(), Host: HostMedia, Method: http.MethodGet, AbsoluteURL: destination.URL + "/callback"})
	if err != nil || media.StatusCode != 200 {
		t.Fatalf("allowlisted media failed: %#v %v", media, err)
	}
}

func TestHTTPTransportBoundsAndRedactsErrorBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(500)
		_, _ = writer.Write([]byte("Bearer token-value person@example.test " + strings.Repeat("x", 5000)))
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	transport := newHTTPTransport(server.Client(), map[Host]*url.URL{HostMain: base})
	response, err := transport.Do(Request{Context: context.Background(), Host: HostMain, Method: http.MethodGet, Path: "/failure"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(response.Body)
	if len(response.Body) > int(ErrorResponseLimit) || strings.Contains(text, "token-value") || strings.Contains(text, "person@example.test") || strings.Contains(strings.ToLower(text), "bearer ") {
		t.Fatalf("unsafe error body: %q", text)
	}
}

func TestHTTPTransportTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) { <-request.Context().Done() }))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client := server.Client()
	client.Timeout = 20 * time.Millisecond
	transport := newHTTPTransport(client, map[Host]*url.URL{HostMain: base})
	_, err := transport.Do(Request{Context: context.Background(), Host: HostMain, Method: http.MethodGet, Path: "/slow"})
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestResponseErrorMapsStatuses(t *testing.T) {
	tests := map[int]domain.ErrorCode{404: domain.CodeNotFound, 401: domain.CodeUpstreamContract, 403: domain.CodeUpstreamContract, 429: domain.CodeRateLimited, 500: domain.CodeUpstream}
	for status, want := range tests {
		err := ResponseError(Response{StatusCode: status, Headers: map[string][]string{"Retry-After": {"1"}}})
		var typed *domain.Error
		if !errors.As(err, &typed) || typed.Code != want {
			t.Fatalf("status %d mapped to %#v, want %s", status, err, want)
		}
	}
}

func TestRedactionRemovesAuthorizationAndEmails(t *testing.T) {
	input := "Authorization: Basic abc123; Bearer secret-token; /users/person%40example.test/profile"
	redacted := RedactURL(input)
	for _, secret := range []string{"abc123", "secret-token", "person%40example.test"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("secret %q remained in %q", secret, redacted)
		}
	}
}
