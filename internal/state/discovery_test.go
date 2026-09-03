package state

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

func TestReserveRateSlotConcurrentSpacing(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	const workers = 12
	waits := make([]time.Duration, workers)
	errs := make([]error, workers)
	var group sync.WaitGroup
	for index := range workers {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			waits[index], errs[index] = database.ReserveRateSlot(ctx, "api.kleinanzeigen.de", now, 0, 30*time.Second)
		}(index)
	}
	group.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Slice(waits, func(left, right int) bool { return waits[left] < waits[right] })
	for index, wait := range waits {
		want := time.Duration(index) * 2500 * time.Millisecond
		if wait != want {
			t.Fatalf("reservation %d wait=%s want=%s all=%v", index, wait, want, waits)
		}
	}
}

func TestReserveRateSlotRejectsLongOneShotQueue(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	for range 13 {
		if _, err := database.ReserveRateSlot(ctx, "api.kleinanzeigen.de", now, 0, 0); err != nil {
			t.Fatal(err)
		}
	}
	_, err = database.ReserveRateSlot(ctx, "api.kleinanzeigen.de", now, 0, 30*time.Second)
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeRateLimitedLocal || domain.ExitCode(err) != 6 || !typed.Retryable || typed.RetryAfter == nil || *typed.RetryAfter != 32500*time.Millisecond {
		t.Fatalf("unexpected local rate error: %#v", err)
	}
}

func TestMobileInstallIDIsStable(t *testing.T) {
	ctx := context.Background()
	database, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	first, err := database.MobileInstallID(ctx, time.UnixMilli(1000))
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.MobileInstallID(ctx, time.UnixMilli(9000))
	if err != nil {
		t.Fatal(err)
	}
	if first != second || len(first) < 37 || first[len(first)-4:] != "1000" {
		t.Fatalf("unstable install ID: %q %q", first, second)
	}
}
