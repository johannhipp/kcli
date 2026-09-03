package cli

import (
	"bufio"
	"errors"
	"io"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/output"
)

func (c *DMPollCmd) Run(runtime *Runtime) error {
	if err := dmRuntimeReady(runtime); err != nil {
		return err
	}
	advance := !c.NoAdvance
	if c.Advance {
		advance = true
	}
	if c.OpenChanged {
		runtime.Diagnostic("dm poll --open-changed opens changed conversations through an account-state-touching PUT")
	} else {
		runtime.Diagnostic("dm poll is list-only; no conversation is opened or marked loaded/read")
	}
	result, err := runtime.Core.DMPoll(runtime.Context, runtime.Profile, runtime.RequestID, domain.DMPollInputV1{
		After: c.After, Since: c.Since, Limit: c.Limit, Advance: &advance, OpenChanged: c.OpenChanged,
	})
	if err != nil {
		return err
	}
	if err := runtime.Encoder.Encode(runtime.Stdout, result); err != nil {
		return err
	}
	if !advance {
		runtime.Diagnostic("DM cursor was not advanced; replay consumers must deduplicate by event_id")
		return nil
	}
	if err := flushOutput(runtime.Stdout); err != nil {
		return err
	}
	if err := runtime.Core.DMAcknowledge(runtime.Context, result.Cursor); err != nil {
		return err
	}
	runtime.Diagnostic("DM cursor advanced after complete output flush; delivery remains at least once at the stdout boundary")
	return nil
}

func (c *DMWatchCmd) Run(runtime *Runtime) error {
	if err := dmRuntimeReady(runtime); err != nil {
		return err
	}
	if c.OpenChanged {
		runtime.Diagnostic("dm watch --open-changed opens changed conversations through an account-state-touching PUT")
	} else {
		runtime.Diagnostic("dm watch is list-only; no conversation is opened or marked loaded/read")
	}
	// Watch has one wire format regardless of TTY or the global finite-command
	// default: each successful emission is one kcli.event/v1 NDJSON object.
	runtime.Encoder.Format = output.FormatNDJSON
	stream := output.Encoder{Format: output.FormatNDJSON, Fields: append([]string(nil), runtime.Encoder.Fields...)}
	return runtime.Core.DMWatch(runtime.Context, runtime.Profile, runtime.RequestID, domain.DMWatchInputV1{
		After: c.After, Since: c.Since, Interval: c.Interval.String(), Limit: c.Limit,
		IncludeHeartbeats: c.IncludeHeartbeats, OpenChanged: c.OpenChanged,
	}, func(event domain.EventV1) error {
		return stream.Encode(runtime.Stdout, event)
	}, func() error {
		return flushOutput(runtime.Stdout)
	}, func(format string, args ...any) {
		runtime.Diagnostic(format, args...)
	})
}

func flushOutput(writer io.Writer) error {
	if buffered, ok := writer.(*bufio.Writer); ok {
		return buffered.Flush()
	}
	if flusher, ok := writer.(interface{ Flush() error }); ok {
		return flusher.Flush()
	}
	if writer == nil {
		return errors.New("output writer is unavailable")
	}
	return nil
}
