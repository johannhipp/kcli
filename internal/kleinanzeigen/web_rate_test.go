package kleinanzeigen

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/state"
)

type cooldownClock struct {
	now        time.Time
	duringWait func()
}

func (c *cooldownClock) Now() time.Time { return c.now }
func (c *cooldownClock) Sleep(_ context.Context, delay time.Duration) error {
	if c.duringWait != nil {
		c.duringWait()
		c.duringWait = nil
	}
	c.now = c.now.Add(delay)
	return nil
}

func TestWebQueuedRequestObservesNewCooldown(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	first, err := state.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := state.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	clock := &cooldownClock{now: time.Unix(1000, 0)}
	if _, err := first.ReserveRateSlot(ctx, "www.kleinanzeigen.de", clock.Now(), 0, 0); err != nil {
		t.Fatal(err)
	}
	limited := NewWebTransport(first)
	limited.clock = clock
	waiting := NewWebTransport(second)
	waiting.clock = clock
	clock.duringWait = func() {
		if err := limited.persistCooldown(ctx, Response{StatusCode: 429, Headers: map[string][]string{"Retry-After": {"120"}}}); err != nil {
			t.Fatal(err)
		}
	}
	requests := 0
	waiting.client.Transport = webRoundTrip(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("request must not be sent during cooldown")
	})
	_, _, err = waiting.fetch(ctx, publicWebOrigin+"/")
	var typed *domain.Error
	if requests != 0 || !errors.As(err, &typed) || typed.Code != domain.CodeRateLimited || typed.RetryAfter == nil || *typed.RetryAfter <= 0 {
		t.Fatalf("requests=%d error=%#v", requests, err)
	}
}
