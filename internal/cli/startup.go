package cli

import (
	"context"
	"errors"

	"github.com/johannhipp/kcli/internal/domain"
)

func stateInitializationError(ctx context.Context, err error) error {
	var interrupted *domain.Error
	if errors.As(context.Cause(ctx), &interrupted) {
		return interrupted
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &domain.Error{Code: domain.CodeConnectivity, Message: "command timed out while opening profile state", Retryable: true, Cause: err}
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return &domain.Error{Code: domain.CodeInterrupted, Message: "command canceled while opening profile state", Cause: err}
	}
	return &domain.Error{Code: domain.CodeUnavailable, Message: "open profile state", Cause: err}
}
