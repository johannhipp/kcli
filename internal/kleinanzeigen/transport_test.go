package kleinanzeigen

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestHTTPTransportUsesOwnedTypesAndBoundsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/fixture" || request.URL.Query().Get("q") != "safe" {
			t.Errorf("unexpected request %s", request.URL.String())
		}
		writer.Header().Set("X-Test", "ok")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	base, _ := url.Parse(server.URL)
	transport := newHTTPTransport(server.Client(), map[Host]*url.URL{HostMain: base})
	response, err := transport.Do(Request{Context: context.Background(), Host: HostMain, Method: http.MethodGet, Path: "/fixture", Query: map[string][]string{"q": {"safe"}}, MaxResponseBytes: 64})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || string(response.Body) != `{"ok":true}` || response.Headers["X-Test"][0] != "ok" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if _, err := transport.Do(Request{Host: HostMain, Method: http.MethodGet, Path: "/../secret"}); err == nil {
		t.Fatal("unsafe path accepted")
	}
}
func TestRedactHeaders(t *testing.T) {
	got := RedactHeaders(map[string][]string{"Authorization": {"Bearer secret"}, "Accept": {"application/json"}})
	if got["Authorization"][0] != "[REDACTED]" || got["Accept"][0] != "application/json" {
		t.Fatalf("unexpected headers: %#v", got)
	}
}
