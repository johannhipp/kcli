package domain

import (
	"errors"
	"fmt"
	"time"
)

type ErrorCode string

const (
	CodeNotImplemented         ErrorCode = "not_implemented"
	CodeInvalidCommand         ErrorCode = "invalid_command"
	CodeInvalidIdentifier      ErrorCode = "invalid_identifier"
	CodeInvalidInput           ErrorCode = "invalid_input"
	CodeInvalidSchema          ErrorCode = "invalid_schema"
	CodeResyncRequired         ErrorCode = "resync_required"
	CodeAuthRequired           ErrorCode = "auth_required"
	CodeAuthExpired            ErrorCode = "auth_expired"
	CodeAuthRevoked            ErrorCode = "auth_revoked"
	CodeNotFound               ErrorCode = "not_found"
	CodeUnavailable            ErrorCode = "unavailable"
	CodeUpstream               ErrorCode = "upstream_failure"
	CodeConnectivity           ErrorCode = "connectivity_failure"
	CodeRateLimited            ErrorCode = "rate_limited"
	CodeRateLimitedLocal       ErrorCode = "rate_limited_local"
	CodeConfirmationRequired   ErrorCode = "confirmation_required"
	CodeConfirmationExpired    ErrorCode = "confirmation_expired"
	CodeConfirmationMismatch   ErrorCode = "confirmation_mismatch"
	CodeWarningBlocked         ErrorCode = "warning_blocked"
	CodeDuplicateUnresolved    ErrorCode = "duplicate_unresolved"
	CodeAmbiguousExternalState ErrorCode = "ambiguous_external_state"
	CodeInterrupted            ErrorCode = "interrupted"
	CodeTerminated             ErrorCode = "terminated"
)

type Error struct {
	Code       ErrorCode      `json:"code"`
	Message    string         `json:"message"`
	Retryable  bool           `json:"retryable"`
	RequestID  string         `json:"request_id,omitempty"`
	RetryAfter *time.Duration `json:"-"`
	Details    map[string]any `json:"details,omitempty"`
	Cause      error          `json:"-"`
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func NewError(code ErrorCode, message string) *Error {
	return &Error{Code: code, Message: message}
}

func NotImplemented(operation string) *Error {
	return NewError(CodeNotImplemented, operation+" arrives in a later phase")
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *Error
	if !errors.As(err, &e) {
		return 1
	}
	switch e.Code {
	case CodeInvalidCommand, CodeInvalidIdentifier, CodeInvalidInput, CodeInvalidSchema, CodeResyncRequired:
		return 2
	case CodeAuthRequired, CodeAuthExpired, CodeAuthRevoked:
		return 3
	case CodeNotFound, CodeUnavailable:
		return 4
	case CodeUpstream, CodeConnectivity:
		return 5
	case CodeRateLimited, CodeRateLimitedLocal:
		return 6
	case CodeConfirmationRequired, CodeConfirmationExpired, CodeConfirmationMismatch, CodeWarningBlocked, CodeDuplicateUnresolved:
		return 7
	case CodeAmbiguousExternalState:
		return 8
	case CodeInterrupted:
		return 130
	case CodeTerminated:
		return 143
	default:
		return 1
	}
}
