package app

import (
	"context"
	"time"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/secret"
	"github.com/johannhipp/kcli/internal/state"
)

type Clock interface {
	Now() time.Time
	Sleep(context.Context, time.Duration) error
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }
func (SystemClock) Sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

type Dependencies struct {
	Transport kleinanzeigen.Transport
	State     *state.DB
	Secrets   secret.SecretStore
	Clock     Clock
}
type App struct{ Dependencies }

func New(deps Dependencies) *App {
	if deps.Clock == nil {
		deps.Clock = SystemClock{}
	}
	if deps.Transport == nil {
		deps.Transport = kleinanzeigen.NewHTTPTransport()
	}
	return &App{Dependencies: deps}
}

func Envelope[T any](clock Clock, schema, requestID, source string, data T) domain.Envelope[T] {
	return domain.Envelope[T]{Schema: schema, RequestID: requestID, Source: source, ObservedAt: clock.Now().UTC(), Completeness: domain.CompletenessComplete, Data: data, Next: nil, Warnings: []domain.WarningV1{}}
}
