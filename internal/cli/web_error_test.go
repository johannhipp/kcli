package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/johannhipp/kcli/internal/domain"
)

func TestWebHydrationErrorsHaveContractExit(t *testing.T) {
	for _, props := range []string{`{"categories":`, `{"categories":[999,"untrusted fixture text"]}`} {
		t.Run(props, func(t *testing.T) {
			setPathEnvironment(t)
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			http.DefaultTransport = webAcceptanceRoundTripper(func(*http.Request) (*http.Response, error) {
				body := `<astro-island component-url="/SearchForm.fixture.js" props='` + props + `'></astro-island>`
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			var stdout, stderr bytes.Buffer
			code := Execute(context.Background(), []string{"category", "list"}, strings.NewReader(""), &stdout, &stderr)
			var result domain.ErrorEnvelopeV1
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if code != 5 || result.Code != domain.CodeUpstreamContract || !strings.Contains(result.Message, "hydration") {
				t.Fatalf("exit=%d output=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			if strings.Contains(stdout.String()+stderr.String(), "untrusted fixture text") {
				t.Fatal("remote data escaped into diagnostics")
			}
		})
	}
}
