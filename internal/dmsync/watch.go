package dmsync

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
)

const (
	DefaultWatchInterval = 30 * time.Second
	MaximumBackoff       = 15 * time.Minute
)

type WatchOptions struct {
	After             string
	Since             string
	Interval          time.Duration
	Limit             int
	IncludeHeartbeats bool
	OpenChanged       bool
	LeaseOwner        string
}

type Watcher struct {
	Poller  *Poller
	Clock   Clock
	Emit    func(domain.EventV1) error
	Flush   func() error
	Notice  func(string, ...any)
	Signals <-chan os.Signal
}

type pollOutcome struct {
	result PollResult
	err    error
}

func (w *Watcher) Run(ctx context.Context, options WatchOptions) error {
	if w == nil || w.Poller == nil || w.Clock == nil || w.Emit == nil || w.Flush == nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "DM watch dependencies are unavailable"}
	}
	if options.Interval == 0 {
		options.Interval = DefaultWatchInterval
	}
	if options.Interval < DefaultWatchInterval || options.Limit < 1 || options.Limit > 500 || (options.After != "" && options.Since != "") {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "DM watch requires an interval of at least 30s, a limit between 1 and 500, and only one starting position"}
	}
	failures := 0
	cycle := int64(0)
	for {
		cycleContext, cancel := context.WithCancel(ctx)
		outcomeChannel := make(chan pollOutcome, 1)
		go func() {
			result, err := w.Poller.Run(cycleContext, PollOptions{
				After: options.After, Since: options.Since, Limit: options.Limit,
				OpenChanged: options.OpenChanged, LeaseOwner: options.LeaseOwner,
			})
			outcomeChannel <- pollOutcome{result: result, err: err}
		}()

		var outcome pollOutcome
		var interrupted os.Signal
		select {
		case outcome = <-outcomeChannel:
			cancel()
		case interrupted = <-w.Signals:
			cancel()
			outcome = <-outcomeChannel
		case <-ctx.Done():
			cancel()
			outcome = <-outcomeChannel
		}
		if interrupted != nil {
			if outcome.err == nil {
				ackContext, ackCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer ackCancel()
				if err := w.emitAndAcknowledge(ackContext, outcome.result, options.IncludeHeartbeats); err != nil && w.Notice != nil {
					w.Notice("watch interrupted before output acknowledgement: %v", err)
				}
			}
			return signalError(interrupted)
		}
		if ctx.Err() != nil {
			return &domain.Error{Code: domain.CodeInterrupted, Message: "DM watch was interrupted", Cause: ctx.Err()}
		}
		if outcome.err != nil {
			if ErrorCode(outcome.err) == domain.CodeResyncRequired {
				if err := w.emitResync(options, outcome.err); err != nil {
					return err
				}
				return outcome.err
			}
			if !IsRetryable(outcome.err) {
				return outcome.err
			}
			failures++
			delay := BackoffDelay(failures, RetryAfter(outcome.err), cycle)
			if w.Notice != nil {
				w.Notice("DM watch backing off for %s after a retryable synchronization failure", delay)
			}
			if err := w.sleepOrSignal(ctx, delay); err != nil {
				return err
			}
			cycle++
			continue
		}

		failures = 0
		if err := w.emitAndAcknowledge(ctx, outcome.result, options.IncludeHeartbeats); err != nil {
			return &domain.Error{Code: domain.CodeUnavailable, Message: "write DM watch event stream", Cause: err}
		}
		options.After = ""
		options.Since = ""
		delay := options.Interval + watchJitter(options.Interval, cycle)
		if err := w.sleepOrSignal(ctx, delay); err != nil {
			return err
		}
		cycle++
	}
}

func (w *Watcher) emitAndAcknowledge(ctx context.Context, result PollResult, includeHeartbeat bool) error {
	if len(result.Events) == 0 && includeHeartbeat && result.Cursor != "" {
		heartbeat := domain.EventV1{
			Schema:  domain.EventSchemaV1,
			EventID: stableEventID(result.AccountHash, "system.heartbeat", result.Cursor, w.Clock.Now().UTC().Format(time.RFC3339Nano)),
			Cursor:  result.Cursor, Type: "system.heartbeat", ObservedAt: w.Clock.Now().UTC(),
			AccountID: accountPseudonym(result.AccountHash), Data: map[string]any{"idle": true},
		}
		if err := w.Emit(heartbeat); err != nil {
			return err
		}
	}
	for _, event := range result.Events {
		if err := w.Emit(event); err != nil {
			return err
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return w.Poller.State.AdvanceCursorHead(ctx, result.AccountHash, result.Generation, result.Sequence)
}

func (w *Watcher) emitResync(options WatchOptions, cause error) error {
	cursor := options.After
	event := domain.EventV1{
		Schema: domain.EventSchemaV1, EventID: stableEventID(w.Poller.AccountHash, "system.resync_required", cursor),
		Cursor: cursor, Type: "system.resync_required", ObservedAt: w.Clock.Now().UTC(),
		AccountID: accountPseudonym(w.Poller.AccountHash), Data: map[string]any{"reason": "local_continuity_unavailable"},
	}
	if err := w.Emit(event); err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "write resynchronization event", Cause: err}
	}
	if err := w.Flush(); err != nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "flush resynchronization event", Cause: err}
	}
	if w.Notice != nil {
		w.Notice("DM watch stopped because local cursor continuity could not be guaranteed")
	}
	_ = cause
	return nil
}

func (w *Watcher) sleepOrSignal(ctx context.Context, delay time.Duration) error {
	sleepContext, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Clock.Sleep(sleepContext, delay) }()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			return err
		}
		if ctx.Err() != nil {
			return &domain.Error{Code: domain.CodeInterrupted, Message: "DM watch was interrupted", Cause: ctx.Err()}
		}
		return nil
	case signal := <-w.Signals:
		cancel()
		<-done
		return signalError(signal)
	case <-ctx.Done():
		cancel()
		<-done
		return &domain.Error{Code: domain.CodeInterrupted, Message: "DM watch was interrupted", Cause: ctx.Err()}
	}
}

func signalError(signal os.Signal) error {
	if signal == syscall.SIGTERM {
		return &domain.Error{Code: domain.CodeTerminated, Message: "DM watch terminated"}
	}
	return &domain.Error{Code: domain.CodeInterrupted, Message: "DM watch interrupted"}
}

func BackoffDelay(failures int, retryAfter time.Duration, salt int64) time.Duration {
	if failures < 1 {
		failures = 1
	}
	exponent := failures - 1
	if exponent > 5 {
		exponent = 5
	}
	delay := DefaultWatchInterval * time.Duration(1<<exponent)
	if retryAfter > delay {
		delay = retryAfter
	}
	if delay >= MaximumBackoff {
		return MaximumBackoff
	}
	jitter := watchJitter(delay, salt)
	if delay+jitter > MaximumBackoff {
		return MaximumBackoff
	}
	return delay + jitter
}

func watchJitter(base time.Duration, salt int64) time.Duration {
	if base <= 0 {
		return 0
	}
	digest := sha256.Sum256([]byte(fmt.Sprintf("kcli-watch-jitter/v1:%d", salt)))
	value := binary.BigEndian.Uint64(digest[:8])
	span := uint64(base / 10)
	if span == 0 {
		return 0
	}
	return time.Duration(value % (span + 1))
}
