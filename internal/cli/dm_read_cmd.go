package cli

import (
	"github.com/johannhipp/kcli/internal/domain"
)

func (c *DMListCmd) Run(runtime *Runtime) error {
	if err := dmRuntimeReady(runtime); err != nil {
		return err
	}
	result, err := runtime.Core.DMList(runtime.Context, runtime.Profile, runtime.RequestID, domain.DMListInputV1{
		Unread: c.Unread, Page: c.Page, PageSize: c.PageSize, Paginate: c.Paginate, Limit: c.Limit,
	})
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func (c *DMGetCmd) Run(runtime *Runtime) error {
	if err := dmRuntimeReady(runtime); err != nil {
		return err
	}
	runtime.Diagnostic("dm get opens the conversation through an account-state-touching PUT")
	result, err := runtime.Core.DMGet(runtime.Context, runtime.Profile, runtime.RequestID, domain.DMGetInputV1{ConversationID: c.ConversationID})
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func (c *DMMarkReadCmd) Run(runtime *Runtime) error {
	if err := dmRuntimeReady(runtime); err != nil {
		return err
	}
	if c.DryRun {
		runtime.Diagnostic("dm mark-read dry run performs no account-state mutation")
	} else {
		runtime.Diagnostic("dm mark-read changes account read state in one bounded request")
	}
	result, err := runtime.Core.DMMarkRead(runtime.Context, runtime.Profile, runtime.RequestID, domain.DMMarkReadInputV1{
		ConversationIDs: append([]string(nil), c.ConversationIDs...), DryRun: c.DryRun,
	})
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func dmRuntimeReady(runtime *Runtime) error {
	if runtime == nil || runtime.Core == nil || runtime.Core.Transport == nil || runtime.Core.State == nil || runtime.Core.Secrets == nil || runtime.Core.Clock == nil || runtime.Stdout == nil || runtime.Stderr == nil || runtime.Encoder.Format == "" {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "DM runtime dependencies are unavailable"}
	}
	return nil
}
