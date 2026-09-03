package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/platform"
	"golang.org/x/term"
)

func (c *AuthLoginCmd) Run(runtime *Runtime) error {
	if err := authRuntime(runtime); err != nil {
		return err
	}
	input := domain.AuthLoginInputV1{NoOpen: c.NoOpen, RedirectFile: c.RedirectFile}
	result, err := runtime.Core.AuthLogin(runtime.Context, runtime.Profile, runtime.RequestID, input, func(authorizeURL string) (string, error) {
		return authCaptureRedirect(runtime, c, authorizeURL)
	})
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func (c *AuthStatusCmd) Run(runtime *Runtime) error {
	if err := authRuntime(runtime); err != nil {
		return err
	}
	result, err := runtime.Core.AuthStatus(runtime.Context, runtime.Profile, runtime.RequestID, domain.AuthStatusInputV1{Check: c.Check})
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func (c *AuthLogoutCmd) Run(runtime *Runtime) error {
	if err := authRuntime(runtime); err != nil {
		return err
	}
	result, err := runtime.Core.AuthLogout(runtime.Context, runtime.Profile, runtime.RequestID, domain.AuthLogoutInputV1{DryRun: c.DryRun})
	if err != nil {
		return err
	}
	return runtime.Encoder.Encode(runtime.Stdout, result)
}

func authRuntime(runtime *Runtime) error {
	if runtime == nil || runtime.Core == nil {
		return &domain.Error{Code: domain.CodeUnavailable, Message: "authentication runtime is unavailable"}
	}
	return nil
}

func authCaptureRedirect(runtime *Runtime, command *AuthLoginCmd, authorizeURL string) (string, error) {
	stdinTerminal := authStdinTerminal()
	if command.RedirectFile == "" && !stdinTerminal {
		return "", &domain.Error{
			Code: domain.CodeAuthRequired, Message: "interactive login requires a terminal or --redirect-file FILE|-",
			Details: map[string]any{"reason": "interactive_required"},
		}
	}
	if _, err := fmt.Fprintf(runtime.Stderr, "Open this authorization URL:\n%s\n", authorizeURL); err != nil {
		return "", &domain.Error{Code: domain.CodeUnavailable, Message: "write authorization instructions", Cause: err}
	}
	if !command.NoOpen {
		if err := platform.OpenURL(runtime.Context, authorizeURL); err != nil {
			runtime.Diagnostic("Could not open the browser; open the authorization URL above manually.")
		}
	}
	if command.RedirectFile != "" {
		return authReadRedirectFile(runtime, command.RedirectFile, stdinTerminal)
	}
	if _, err := fmt.Fprintln(runtime.Stderr, "Paste the complete final redirect URL (the browser page may look unsuccessful):"); err != nil {
		return "", &domain.Error{Code: domain.CodeUnavailable, Message: "write authorization prompt", Cause: err}
	}
	return authReadTerminalRedirect(runtime)
}

func authReadRedirectFile(runtime *Runtime, path string, stdinTerminal bool) (string, error) {
	if path == "-" {
		if stdinTerminal {
			if _, err := fmt.Fprintln(runtime.Stderr, "Paste the complete final redirect URL:"); err != nil {
				return "", &domain.Error{Code: domain.CodeUnavailable, Message: "write authorization prompt", Cause: err}
			}
			return authReadTerminalRedirect(runtime)
		}
		return authReadRedirect(runtime.Stdin)
	}
	if strings.IndexByte(path, 0) >= 0 {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "redirect file path is invalid"}
	}
	file, err := os.Open(path)
	if err != nil {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "read authorization redirect file", Cause: err}
	}
	defer file.Close()
	return authReadRedirect(file)
}

func authReadTerminalRedirect(runtime *Runtime) (string, error) {
	data, err := term.ReadPassword(int(os.Stdin.Fd()))
	_, _ = fmt.Fprintln(runtime.Stderr)
	if err != nil {
		return "", &domain.Error{Code: domain.CodeUnavailable, Message: "read authorization redirect without echo", Cause: err}
	}
	if len(data) > 64<<10 {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "authorization redirect is too large"}
	}
	return strings.TrimSpace(string(data)), nil
}

func authReadRedirect(reader io.Reader) (string, error) {
	data, err := io.ReadAll(io.LimitReader(reader, (64<<10)+1))
	if err != nil {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "read authorization redirect", Cause: err}
	}
	if len(data) > 64<<10 {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "authorization redirect is too large"}
	}
	value := strings.TrimSpace(string(data))
	if value == "" {
		return "", &domain.Error{Code: domain.CodeInvalidInput, Message: "authorization redirect is empty"}
	}
	return value, nil
}

func authStdinTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0 && term.IsTerminal(int(os.Stdin.Fd()))
}
