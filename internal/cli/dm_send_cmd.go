package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
)

const (
	dmMessageInputLimit = 64 << 10
	dmJSONInputLimit    = 128 << 10
)

func (c *DMReplyCmd) Run(runtime *Runtime) error {
	if err := dmRuntimeReady(runtime); err != nil {
		return err
	}
	input, err := dmReplyCommandInput(c, runtime.Stdin)
	if err != nil {
		return err
	}
	if input.DryRun {
		runtime.Diagnostic("dm reply dry run reads inbox summary context and stores digest-only local confirmation state; no external message is sent")
	} else {
		runtime.Diagnostic("dm reply confirm atomically claims local confirmation state and performs at most one external-send attempt")
	}
	result, err := runtime.Core.DMReply(runtime.Context, runtime.Profile, runtime.RequestID, input)
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func (c *DMStartCmd) Run(runtime *Runtime) error {
	if err := dmRuntimeReady(runtime); err != nil {
		return err
	}
	input, err := dmStartCommandInput(c, runtime.Stdin)
	if err != nil {
		return err
	}
	if input.DryRun {
		runtime.Diagnostic("dm start dry run reads listing/inbox context and stores digest-only local confirmation state; creating a conversation may be visible to the seller")
	} else {
		runtime.Diagnostic("dm start confirm atomically claims local confirmation state; creating a conversation may be visible to the seller; external-create and external-send each have at most one attempt")
	}
	result, err := runtime.Core.DMStart(runtime.Context, runtime.Profile, runtime.RequestID, input)
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func dmReplyCommandInput(command *DMReplyCmd, stdin io.Reader) (domain.DMReplyInputV1, error) {
	if command == nil {
		return domain.DMReplyInputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "reply command is unavailable"}
	}
	if err := validateRuntimeMutationSources(command.Message, command.MessageFile, command.Input, command.DryRun, command.Confirm); err != nil {
		return domain.DMReplyInputV1{}, err
	}
	base := domain.DMReplyInputV1{
		ConversationID: command.ConversationID, DryRun: command.DryRun, Confirm: command.Confirm,
		AcknowledgeWarning:           command.AcknowledgeWarning,
		AcknowledgePossibleDuplicate: command.AcknowledgePossibleDuplicate,
	}
	if command.Input != "" {
		var supplied domain.DMReplyInputV1
		if err := readDMJSONInput(command.Input, stdin, &supplied); err != nil {
			return domain.DMReplyInputV1{}, err
		}
		if supplied.ConversationID != "" && supplied.ConversationID != base.ConversationID {
			return domain.DMReplyInputV1{}, conflictingDMInput("conversation_id")
		}
		if supplied.DryRun && !base.DryRun || supplied.Confirm != "" && supplied.Confirm != base.Confirm {
			return domain.DMReplyInputV1{}, conflictingDMInput("confirmation phase")
		}
		if supplied.MessageFile != "" || supplied.Input != "" {
			return domain.DMReplyInputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "nested message_file or input sources are not allowed in --input"}
		}
		warning, err := mergeDMInputValue(base.AcknowledgeWarning, supplied.AcknowledgeWarning, "acknowledge_warning")
		if err != nil {
			return domain.DMReplyInputV1{}, err
		}
		duplicate, err := mergeDMInputValue(base.AcknowledgePossibleDuplicate, supplied.AcknowledgePossibleDuplicate, "acknowledge_possible_duplicate")
		if err != nil {
			return domain.DMReplyInputV1{}, err
		}
		base.Message = supplied.Message
		base.AcknowledgeWarning = warning
		base.AcknowledgePossibleDuplicate = duplicate
	} else if command.MessageFile != "" {
		message, err := readDMMessage(command.MessageFile, stdin)
		if err != nil {
			return domain.DMReplyInputV1{}, err
		}
		base.Message = message
	} else {
		base.Message = command.Message
	}
	if err := validateResolvedDMMessage(base.Message); err != nil {
		return domain.DMReplyInputV1{}, err
	}
	return base, nil
}

func dmStartCommandInput(command *DMStartCmd, stdin io.Reader) (domain.DMStartInputV1, error) {
	if command == nil {
		return domain.DMStartInputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "start command is unavailable"}
	}
	if err := validateRuntimeMutationSources(command.Message, command.MessageFile, command.Input, command.DryRun, command.Confirm); err != nil {
		return domain.DMStartInputV1{}, err
	}
	base := domain.DMStartInputV1{
		ListingIDOrURL: command.ListingIDOrURL, ContactName: command.ContactName,
		DryRun: command.DryRun, Confirm: command.Confirm,
		AcknowledgeWarning:           command.AcknowledgeWarning,
		AcknowledgePossibleDuplicate: command.AcknowledgePossibleDuplicate,
	}
	if command.Input != "" {
		var supplied domain.DMStartInputV1
		if err := readDMJSONInput(command.Input, stdin, &supplied); err != nil {
			return domain.DMStartInputV1{}, err
		}
		if supplied.ListingIDOrURL != "" && supplied.ListingIDOrURL != base.ListingIDOrURL {
			return domain.DMStartInputV1{}, conflictingDMInput("listing_id_or_url")
		}
		if supplied.DryRun && !base.DryRun || supplied.Confirm != "" && supplied.Confirm != base.Confirm {
			return domain.DMStartInputV1{}, conflictingDMInput("confirmation phase")
		}
		if supplied.MessageFile != "" || supplied.Input != "" {
			return domain.DMStartInputV1{}, &domain.Error{Code: domain.CodeInvalidInput, Message: "nested message_file or input sources are not allowed in --input"}
		}
		contactName, err := mergeDMInputValue(base.ContactName, supplied.ContactName, "contact_name")
		if err != nil {
			return domain.DMStartInputV1{}, err
		}
		warning, err := mergeDMInputValue(base.AcknowledgeWarning, supplied.AcknowledgeWarning, "acknowledge_warning")
		if err != nil {
			return domain.DMStartInputV1{}, err
		}
		duplicate, err := mergeDMInputValue(base.AcknowledgePossibleDuplicate, supplied.AcknowledgePossibleDuplicate, "acknowledge_possible_duplicate")
		if err != nil {
			return domain.DMStartInputV1{}, err
		}
		base.Message = supplied.Message
		base.ContactName = contactName
		base.AcknowledgeWarning = warning
		base.AcknowledgePossibleDuplicate = duplicate
	} else if command.MessageFile != "" {
		message, err := readDMMessage(command.MessageFile, stdin)
		if err != nil {
			return domain.DMStartInputV1{}, err
		}
		base.Message = message
	} else {
		base.Message = command.Message
	}
	if err := validateResolvedDMMessage(base.Message); err != nil {
		return domain.DMStartInputV1{}, err
	}
	return base, nil
}

func validateRuntimeMutationSources(message, messageFile, input string, dryRun bool, confirmationID string) error {
	count := 0
	for _, source := range []string{message, messageFile, input} {
		if source != "" {
			count++
		}
	}
	if count != 1 {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "provide exactly one of --message, --message-file, or --input"}
	}
	if dryRun == (confirmationID != "") {
		return &domain.Error{Code: domain.CodeConfirmationRequired, Message: "provide exactly one of --dry-run or --confirm"}
	}
	return nil
}

func readDMMessage(path string, stdin io.Reader) (string, error) {
	reader, closeInput, err := dmInputReader(path, stdin, "message file")
	if err != nil {
		return "", err
	}
	if closeInput != nil {
		defer closeInput()
	}
	body, err := io.ReadAll(io.LimitReader(reader, dmMessageInputLimit+1))
	if err != nil {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "read message file", Cause: err}
	}
	if len(body) > dmMessageInputLimit {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: fmt.Sprintf("message exceeds %d bytes", dmMessageInputLimit)}
	}
	return string(body), nil
}

func readDMJSONInput(path string, stdin io.Reader, target any) error {
	reader, closeInput, err := dmInputReader(path, stdin, "DM input")
	if err != nil {
		return err
	}
	if closeInput != nil {
		defer closeInput()
	}
	body, err := io.ReadAll(io.LimitReader(reader, dmJSONInputLimit+1))
	if err != nil {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "read DM input", Cause: err}
	}
	if len(body) > dmJSONInputLimit {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: fmt.Sprintf("DM input exceeds %d bytes", dmJSONInputLimit)}
	}
	if err := kleinanzeigen.DecodeUniqueJSON(body, target); err != nil {
		return &domain.Error{Code: domain.CodeInvalidSchema, Message: "decode DM input", Cause: err}
	}
	strict := json.NewDecoder(bytes.NewReader(body))
	strict.DisallowUnknownFields()
	if err := strict.Decode(target); err != nil {
		return &domain.Error{Code: domain.CodeInvalidSchema, Message: "decode DM input", Cause: err}
	}
	if err := strict.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			err = fmt.Errorf("multiple JSON values are not allowed")
		}
		return &domain.Error{Code: domain.CodeInvalidSchema, Message: "decode DM input", Cause: err}
	}
	return nil
}

func dmInputReader(path string, stdin io.Reader, label string) (io.Reader, func() error, error) {
	if path == "-" {
		if stdin == nil {
			return nil, nil, &domain.Error{Code: domain.CodeInvalidInput, Message: "stdin is unavailable"}
		}
		return stdin, nil, nil
	}
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return nil, nil, &domain.Error{Code: domain.CodeInvalidInput, Message: label + " path is invalid"}
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, &domain.Error{Code: domain.CodeInvalidInput, Message: "open " + label, Cause: err}
	}
	return file, file.Close, nil
}

func validateResolvedDMMessage(message string) error {
	if message == "" || len(message) > dmMessageInputLimit || !utf8.ValidString(message) || strings.IndexByte(message, 0) >= 0 {
		return &domain.Error{Code: domain.CodeInvalidInput, Message: "message must be non-empty valid UTF-8 no larger than 65536 bytes"}
	}
	return nil
}

func mergeDMInputValue(flagValue, inputValue, field string) (string, error) {
	if flagValue != "" && inputValue != "" && flagValue != inputValue {
		return "", conflictingDMInput(field)
	}
	if flagValue != "" {
		return flagValue, nil
	}
	return inputValue, nil
}

func conflictingDMInput(field string) error {
	return &domain.Error{Code: domain.CodeInvalidInput, Message: "--input conflicts with command field " + field}
}
