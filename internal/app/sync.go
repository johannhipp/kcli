package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/johannhipp/kcli/internal/dmsync"
	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/state"
)

// DMPoll observes and durably spools one bounded synchronization cycle. The
// returned cursor is acknowledged separately, after the caller has flushed its
// complete output, through DMAcknowledge.
func (a *App) DMPoll(ctx context.Context, profile, requestID string, input domain.DMPollInputV1) (domain.SyncOutputV1, error) {
	if a == nil || a.State == nil || a.Clock == nil {
		return domain.SyncOutputV1{}, &domain.Error{Code: domain.CodeUnavailable, Message: "DM synchronization runtime is unavailable"}
	}
	account, client, err := a.dmSyncClient(ctx, profile, requestID)
	if err != nil {
		return domain.SyncOutputV1{}, err
	}
	limit := input.Limit
	if limit == 0 {
		limit = 200
	}
	poller := &dmsync.Poller{State: a.State, Client: client, Clock: a.Clock, AccountHash: account.SubjectHash}
	result, err := poller.Run(ctx, dmsync.PollOptions{
		After: input.After, Since: input.Since, Limit: limit, OpenChanged: input.OpenChanged,
		LeaseOwner: syncLeaseOwner(requestID),
	})
	if err != nil {
		return domain.SyncOutputV1{}, err
	}
	envelope := Envelope(a.Clock, "kcli.dm-events/v1", requestID, "message-gateway", result.Events)
	envelope.ObservedAt = a.Clock.Now().UTC()
	envelope.Warnings = append(envelope.Warnings, result.Warnings...)
	if input.OpenChanged {
		envelope.Warnings = append(envelope.Warnings, domain.WarningV1{
			Code:    "open_changed_state_touching",
			Message: "changed conversations were opened through an account-state-touching PUT",
			Details: map[string]any{"side_effect": "account-state", "operation": "open-changed"},
		})
	}
	envelope.Warnings = append(envelope.Warnings, domain.WarningV1{
		Code: "dm_sync_metadata", Message: "DM synchronization used a bounded conservative conversation scan",
		Details: map[string]any{"pages": result.Pages, "fetched": result.Fetched, "reconciliation": result.Reconciliation, "delivery": "at-least-once", "deduplicate_by": "event_id"},
	})
	if len(result.Warnings) > 0 {
		envelope.Completeness = domain.CompletenessPartial
	}
	envelope.Next = &result.Cursor
	return domain.SyncOutputV1{Envelope: envelope, Cursor: result.Cursor}, nil
}

// DMAcknowledge advances the named cursor head monotonically. Callers invoke it
// only after the bytes carrying the complete batch have been flushed.
func (a *App) DMAcknowledge(ctx context.Context, cursor string) error {
	if a == nil || a.State == nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "DM synchronization state is unavailable"}
	}
	payload, err := domain.DecodeCursor(cursor)
	if err != nil {
		return &domain.Error{Code: domain.CodeResyncRequired, Message: "DM cursor is invalid", Cause: err}
	}
	account, exists, err := a.State.AuthAccount(ctx)
	if err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "read authenticated account state", Cause: err}
	}
	if !exists || account.SubjectHash != payload.AccountSubjectHash {
		return &domain.Error{Code: domain.CodeResyncRequired, Message: "DM cursor belongs to another account or state store"}
	}
	if err := a.State.ValidateCursor(ctx, account.SubjectHash, payload); err != nil {
		if errors.Is(err, state.ErrCursorContinuity) {
			return &domain.Error{Code: domain.CodeResyncRequired, Message: "DM cursor is stale or unknown", Cause: err}
		}
		return &domain.Error{Code: domain.CodeUnavailable, Message: "validate DM cursor", Cause: err}
	}
	if err := a.State.AdvanceCursorHead(ctx, account.SubjectHash, payload.Generation, payload.Sequence); err != nil {
		if errors.Is(err, state.ErrCursorContinuity) {
			return &domain.Error{Code: domain.CodeResyncRequired, Message: "DM cursor continuity changed before acknowledgement", Cause: err}
		}
		return &domain.Error{Code: domain.CodeUnavailable, Message: "advance DM cursor", Cause: err}
	}
	return nil
}

// DMWatch repeats the same Poller cycle and emits only complete EventV1 values.
// The sink must serialize each event as one NDJSON line; flush is called before
// the durable cursor head advances.
func (a *App) DMWatch(ctx context.Context, profile, requestID string, input domain.DMWatchInputV1, emit func(domain.EventV1) error, flush func() error, notice func(string, ...any)) error {
	if a == nil || a.State == nil || a.Clock == nil || emit == nil || flush == nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "DM watch runtime is unavailable"}
	}
	interval := dmsync.DefaultWatchInterval
	var err error
	if input.Interval != "" {
		interval, err = time.ParseDuration(input.Interval)
		if err != nil {
			return &domain.Error{Code: domain.CodeInvalidInput, Message: "DM watch interval is invalid", Cause: err}
		}
	}
	limit := input.Limit
	if limit == 0 {
		limit = 200
	}
	account, client, err := a.dmSyncClient(ctx, profile, requestID)
	if err != nil {
		return err
	}
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	watcher := &dmsync.Watcher{
		Poller: &dmsync.Poller{State: a.State, Client: client, Clock: a.Clock, AccountHash: account.SubjectHash},
		Clock:  a.Clock, Emit: emit, Flush: flush, Notice: notice, Signals: signals,
	}
	return watcher.Run(ctx, dmsync.WatchOptions{
		After: input.After, Since: input.Since, Interval: interval, Limit: limit,
		IncludeHeartbeats: input.IncludeHeartbeats, OpenChanged: input.OpenChanged,
		LeaseOwner: syncLeaseOwner(requestID),
	})
}

func syncLeaseOwner(requestID string) string {
	if requestID == "" {
		return "sync-anonymous"
	}
	if len(requestID) > 120 {
		requestID = requestID[:120]
	}
	return fmt.Sprintf("sync-%s", requestID)
}

type syncRetryTransport struct {
	base    kleinanzeigen.Transport
	mu      sync.Mutex
	headers map[string][]string
}

func (t *syncRetryTransport) AuthConfiguration() kleinanzeigen.AuthConfiguration {
	if provider, ok := t.base.(kleinanzeigen.AuthConfigurationProvider); ok {
		return provider.AuthConfiguration()
	}
	return kleinanzeigen.AuthConfiguration{}
}

func (t *syncRetryTransport) Do(request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	response, err := t.base.Do(request)
	t.mu.Lock()
	t.headers = make(map[string][]string, len(response.Headers))
	for key, values := range response.Headers {
		t.headers[key] = append([]string(nil), values...)
	}
	t.mu.Unlock()
	return response, err
}

func (t *syncRetryTransport) retryAfter(now time.Time) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	var value string
	for key, values := range t.headers {
		if strings.EqualFold(key, "Retry-After") && len(values) > 0 {
			value = strings.TrimSpace(values[0])
			break
		}
	}
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if target, err := http.ParseTime(value); err == nil && target.After(now) {
		return target.Sub(now)
	}
	return 0
}

func (a *App) dmSyncClient(ctx context.Context, profile, requestID string) (state.AuthAccountState, *kleinanzeigen.MessageClient, error) {
	account, _, err := a.dmClient(ctx, profile, requestID)
	if err != nil {
		return state.AuthAccountState{}, nil, err
	}
	capture := &syncRetryTransport{base: a.Transport}
	authApp := &App{Dependencies: a.Dependencies}
	authApp.Transport = capture
	client, err := kleinanzeigen.NewMessageClient(account.AccountID, func(callContext context.Context, request kleinanzeigen.Request) (kleinanzeigen.Response, error) {
		response, requestErr := authApp.AuthDo(callContext, profile, requestID, request)
		if requestErr == nil {
			return response, nil
		}
		var typed *domain.Error
		if errors.As(requestErr, &typed) && typed.Code == domain.CodeRateLimited && typed.RetryAfter == nil {
			if delay := capture.retryAfter(a.Clock.Now().UTC()); delay > 0 {
				copied := *typed
				copied.RetryAfter = &delay
				requestErr = &copied
			}
		}
		return response, requestErr
	})
	return account, client, err
}
