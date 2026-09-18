package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/state"
)

type paginationRoundTripper func(*http.Request) (*http.Response, error)

func (f paginationRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestWebSearchContinuation(t *testing.T) {
	for _, tc := range []struct {
		name             string
		rows, limit      int
		linked, paginate bool
		wantNext         string
		truncated        bool
		calls            int
	}{
		{name: "full linked page", rows: 25, linked: true, wantNext: "1", calls: 1},
		{name: "short terminal page", rows: 2, calls: 1},
		{name: "full terminal page", rows: 25, calls: 1},
		{name: "truncated page", rows: 2, limit: 1, linked: true, truncated: true, calls: 1},
		{name: "paginated terminal page", rows: 2, paginate: true, calls: 1},
		{name: "follow advertised link", rows: 2, paginate: true, linked: true, calls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			original := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = original })
			calls := 0
			http.DefaultTransport = paginationRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				count := tc.rows
				if calls > 1 {
					count = 1
					if r.URL.Path != "/s-fahrrad/seite:2/k0" {
						t.Fatalf("unadvertised page: %s", r.URL)
					}
				}
				rows := make([]string, count)
				for i := range rows {
					rows[i] = fmt.Sprintf(`[0,{"organicAdPreview":[0,{"id":[0,"%d"],"title":[0,"Bike"],"seoLink":[0,"/s-anzeige/bike/%d-217-3331"]}]}]`, calls*100+i, calls*100+i)
				}
				body := `<astro-island component-url="/ImpressionTracker.fixture.js" props='{"resultAds":[1,[` + strings.Join(rows, ",") + `]]}'></astro-island>`
				if tc.linked && calls == 1 {
					body += `<a href="/s-fahrrad/seite:2/k0">Next</a>`
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			application := New(Dependencies{Transport: kleinanzeigen.NewWebTransport(db), State: db})
			result, err := application.Search(context.Background(), domain.SearchInputV1{Limit: tc.limit, Paginate: tc.paginate})
			if err != nil {
				t.Fatal(err)
			}
			if calls == 2 && len(result.Data) != 3 {
				t.Fatalf("lost paginated results: %v", result.Data)
			}
			next := ""
			if result.Next != nil {
				next = *result.Next
			}
			if next != tc.wantNext || calls != tc.calls {
				t.Fatalf("next=%q calls=%d; want next=%q calls=%d", next, calls, tc.wantNext, tc.calls)
			}
			truncated := false
			for _, w := range result.Warnings {
				if w.Code == "page_truncated" {
					truncated = true
				}
			}
			if truncated != tc.truncated || (truncated && result.Completeness != domain.CompletenessPartial) {
				t.Fatalf("truncation=%v completeness=%s", truncated, result.Completeness)
			}
		})
	}
}
