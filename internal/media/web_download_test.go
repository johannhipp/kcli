package media

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/kleinanzeigen"
)

type mediaRoundTripper func(*http.Request) (*http.Response, error)

func (f mediaRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDownloadWebTransportRedirects(t *testing.T) {
	for _, tc := range []struct {
		name, location string
		wantCalls      int
		success        bool
	}{
		{"same host", "/final.png", 2, true},
		{"other host", "https://cdn.kleinanzeigen.de/final.png", 1, false},
		{"loop", "/start.png", 6, false},
		{"missing location", "", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			calls := 0
			image := "\x89PNG\r\n\x1a\nfixture"
			http.DefaultTransport = mediaRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "img.kleinanzeigen.de" {
					t.Fatalf("unexpected host %s", r.URL.Host)
				}
				response := &http.Response{StatusCode: 302, Header: http.Header{"Location": {tc.location}}, Body: io.NopCloser(strings.NewReader(""))}
				if r.URL.Path == "/final.png" {
					response.StatusCode = 200
					response.Header = http.Header{"Content-Type": {"image/png"}}
					response.Body = io.NopCloser(strings.NewReader(image))
				}
				return response, nil
			})
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			path, err := Download(ctx, kleinanzeigen.NewWebTransport(nil), DownloadRequest{ListingID: "123", Relation: "large", URL: "https://img.kleinanzeigen.de/start.png", OutputDir: dir, AllowOutsideCWD: dir, MaxBytes: 100})
			if (err == nil) != tc.success || calls != tc.wantCalls {
				t.Fatalf("calls=%d path=%q err=%v", calls, path, err)
			}
			if tc.success {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != image {
					t.Fatalf("body=%q err=%v", data, err)
				}
			}
		})
	}
}
