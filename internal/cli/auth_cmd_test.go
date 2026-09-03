package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/johannhipp/kcli/internal/app"
	"github.com/johannhipp/kcli/internal/domain"
	"github.com/johannhipp/kcli/internal/kleinanzeigen"
	"github.com/johannhipp/kcli/internal/output"
	"github.com/johannhipp/kcli/internal/secret"
	"github.com/johannhipp/kcli/internal/state"
)

type authCLITestTransport struct{ calls atomic.Int32 }

func (*authCLITestTransport) AuthConfiguration() kleinanzeigen.AuthConfiguration {
	return kleinanzeigen.AuthConfiguration{ClientID: "mobile-client", Issuer: "https://issuer.invalid/"}
}
func (t *authCLITestTransport) Do(kleinanzeigen.Request) (kleinanzeigen.Response, error) {
	t.calls.Add(1)
	return kleinanzeigen.Response{}, errors.New("network must not be called")
}

type authCLITestSecrets struct{}

func (authCLITestSecrets) Get(string, string) (string, error) { return "", secret.ErrNotFound }
func (authCLITestSecrets) Set(string, string, string) error   { return nil }
func (authCLITestSecrets) Delete(string, string) error        { return secret.ErrNotFound }

func TestAuthLoginHeadlessWithoutRedirectFileFailsBeforeNetwork(t *testing.T) {
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	_ = writePipe.Close()
	previousStdin := os.Stdin
	os.Stdin = readPipe
	t.Cleanup(func() {
		os.Stdin = previousStdin
		_ = readPipe.Close()
	})

	database, err := state.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	transport := &authCLITestTransport{}
	var stdout, stderr bytes.Buffer
	runtime := &Runtime{
		Context: context.Background(), Stdout: &stdout, Stderr: &stderr,
		RequestID: "request-headless", Profile: "default", Encoder: output.Encoder{Format: output.FormatJSON},
		Core: app.New(app.Dependencies{Transport: transport, State: database, Secrets: authCLITestSecrets{}}),
	}
	err = (&AuthLoginCmd{NoOpen: true}).Run(runtime)
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeAuthRequired || typed.Details["reason"] != "interactive_required" {
		t.Fatalf("error = %#v", err)
	}
	if transport.calls.Load() != 0 {
		t.Fatalf("network calls = %d, want 0", transport.calls.Load())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("unexpected output: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestAuthRedirectFileCaptureReadsCompleteURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "redirect.txt")
	redirect := kleinanzeigen.AuthRedirectURI + "?state=state&code=code"
	if err := os.WriteFile(path, []byte("  "+redirect+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	runtime := &Runtime{Context: context.Background(), Stderr: &stderr}
	got, err := authCaptureRedirect(runtime, &AuthLoginCmd{NoOpen: true, RedirectFile: path}, "https://login.kleinanzeigen.de/authorize?state=public")
	if err != nil || got != redirect {
		t.Fatalf("redirect = %q, err = %v", got, err)
	}
	if !strings.Contains(stderr.String(), "https://login.kleinanzeigen.de/authorize") || strings.Contains(stderr.String(), "code=code") {
		t.Fatalf("instructions = %q", stderr.String())
	}
}

func TestAuthReadRedirectRejectsOversize(t *testing.T) {
	_, err := authReadRedirect(strings.NewReader(strings.Repeat("x", (64<<10)+1)))
	var typed *domain.Error
	if !errors.As(err, &typed) || typed.Code != domain.CodeInvalidInput {
		t.Fatalf("error = %#v", err)
	}
}
