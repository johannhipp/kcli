package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/johannhipp/kcli/internal/cli"
	"github.com/johannhipp/kcli/internal/domain"
)

func main() {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case signal := <-signals:
			code := domain.CodeInterrupted
			if signal == syscall.SIGTERM {
				code = domain.CodeTerminated
			}
			cancel(domain.NewError(code, "operation interrupted"))
		case <-ctx.Done():
		}
	}()
	code := cli.Execute(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if code == 0 && context.Cause(ctx) != nil {
		code = domain.ExitCode(context.Cause(ctx))
	}
	os.Exit(code)
}
