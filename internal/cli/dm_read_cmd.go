package cli

import (
	"github.com/johannhipp/kcli/internal/domain"
	"io"
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
	input, err := dmMarkReadCommandInput(c, runtime.Stdin)
	if err != nil {
		return err
	}
	if input.DryRun {
		runtime.Diagnostic("dm mark-read dry run performs no account-state mutation")
	} else {
		runtime.Diagnostic("dm mark-read changes account read state in one bounded request")
	}
	result, err := runtime.Core.DMMarkRead(runtime.Context, runtime.Profile, runtime.RequestID, input)
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

// dmMarkReadCommandInput resolves the conversation IDs from the positional
// arguments or a versioned --input document, and merges the dry-run flag.
func dmMarkReadCommandInput(command *DMMarkReadCmd, stdin io.Reader) (domain.DMMarkReadInputV1, error) {
	if command == nil {
		return domain.DMMarkReadInputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "mark-read command is unavailable"}
	}
	base := domain.DMMarkReadInputV1{DryRun: command.DryRun}
	if command.Input != "" {
		var supplied domain.DMMarkReadInputV1
		if err := readDMJSONInput(command.Input, stdin, &supplied); err != nil {
			return domain.DMMarkReadInputV1{}, err
		}
		if len(supplied.ConversationIDs) == 0 || len(supplied.ConversationIDs) > 100 {
			return domain.DMMarkReadInputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "provide between 1 and 100 conversation IDs"}
		}
		for _, id := range supplied.ConversationIDs {
			if err := validateReference(id, false); err != nil {
				return domain.DMMarkReadInputV1{}, err
			}
		}
		base.ConversationIDs = supplied.ConversationIDs
		base.DryRun = command.DryRun || supplied.DryRun
		return base, nil
	}
	base.ConversationIDs = append([]string(nil), command.ConversationIDs...)
	return base, nil
}

func dmRuntimeReady(runtime *Runtime) error {
	if runtime == nil || runtime.Core == nil || runtime.Core.Transport == nil || runtime.Core.State == nil || runtime.Core.Secrets == nil || runtime.Core.Clock == nil || runtime.Stdout == nil || runtime.Stderr == nil || runtime.Encoder.Format == "" {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "DM runtime dependencies are unavailable"}
	}
	return nil
}
