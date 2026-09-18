package kleinanzeigen

import (
	"context"
	"math/rand/v2"
	"net/http"
	"time"
)

// Cooldowns are separate from queue reservations so a server response can stop
// clients that already reserved a request start time.
type webRateStore interface {
	ReserveRateSlot(context.Context, string, time.Time, time.Duration, time.Duration) (time.Duration, error)
	SetRateCooldown(context.Context, string, time.Time) error
	CheckRateCooldown(context.Context, string, time.Time) error
}

func (t *WebTransport) reserve(ctx context.Context) error {
	jitter := time.Duration(rand.IntN(251)) * time.Millisecond
	var wait time.Duration
	if t.rate != nil {
		if err := t.rate.CheckRateCooldown(ctx, "www.kleinanzeigen.de", t.clock.Now()); err != nil {
			return err
		}
		var err error
		wait, err = t.rate.ReserveRateSlot(ctx, "www.kleinanzeigen.de", t.clock.Now(), jitter, 30*time.Second)
		if err != nil {
			return err
		}
	} else {
		t.mu.Lock()
		now := t.clock.Now()
		if t.next.After(now) {
			wait = t.next.Sub(now)
		}
		t.next = now.Add(wait + 2500*time.Millisecond + jitter)
		t.mu.Unlock()
	}
	if wait > 0 {
		if err := t.clock.Sleep(ctx, wait); err != nil {
			return contextOperationError(err)
		}
	}
	if t.rate != nil {
		return t.rate.CheckRateCooldown(ctx, "www.kleinanzeigen.de", t.clock.Now())
	}
	return nil
}
func (t *WebTransport) persistCooldown(ctx context.Context, response Response) error {
	if response.StatusCode != http.StatusTooManyRequests || t.rate == nil {
		return nil
	}
	delay := retryDelay(response.Headers, 0, t.clock.Now())
	if delay < time.Minute {
		delay = time.Minute
	}
	return t.rate.SetRateCooldown(ctx, "www.kleinanzeigen.de", t.clock.Now().Add(delay))
}
