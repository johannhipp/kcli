package state

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

func TestRateCooldownExpiryAndExtension(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Unix(1000, 0)
	host := "www.kleinanzeigen.de"
	if err := db.CheckRateCooldown(ctx, host, now); err != nil {
		t.Fatal(err)
	}
	for _, delay := range []time.Duration{time.Minute, 2 * time.Minute, time.Minute} {
		if err := db.SetRateCooldown(ctx, host, now.Add(delay)); err != nil {
			t.Fatal(err)
		}
	}
	var limited *domain.Error
	if err := db.CheckRateCooldown(ctx, host, now); !errors.As(err, &limited) || limited.RetryAfter == nil || *limited.RetryAfter != 2*time.Minute {
		t.Fatalf("cooldown was shortened or lost: %v", err)
	}
	if err := db.CheckRateCooldown(ctx, "unrelated.example", now); err != nil {
		t.Fatal(err)
	}
	if err := db.CheckRateCooldown(ctx, host, now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
}
