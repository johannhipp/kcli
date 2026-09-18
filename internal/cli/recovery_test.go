package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johannhipp/kcli/internal/domain"
)

func TestRecoveryCommandsWithBrokenLocalFiles(t *testing.T) {
	for _, broken := range []string{"config", "state", "both"} {
		t.Run(broken, func(t *testing.T) {
			base := setPathEnvironment(t)
			configPath := filepath.Join(base, "config", "kcli", "config.json")
			statePath := filepath.Join(base, "state", "kcli", "profiles", "default", "state.db")
			for _, file := range []struct{ kind, path string }{{"config", configPath}, {"state", statePath}} {
				if broken != file.kind && broken != "both" {
					continue
				}
				if err := os.MkdirAll(filepath.Dir(file.path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file.path, []byte("invalid fixture"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			commands := [][]string{{"version"}, {"config", "path"}, {"doctor"}}
			if broken != "config" {
				commands = append(commands, []string{"doctor", "--network"})
				original := http.DefaultTransport
				t.Cleanup(func() { http.DefaultTransport = original })
				http.DefaultTransport = webAcceptanceRoundTripper(func(*http.Request) (*http.Response, error) {
					t.Fatal("network request without healthy state")
					return nil, nil
				})
			}
			for _, args := range commands {
				var stdout, stderr bytes.Buffer
				if code := Execute(context.Background(), args, strings.NewReader(""), &stdout, &stderr); code != 0 {
					t.Fatalf("%v: exit=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
				}
				if args[0] == "doctor" {
					var report domain.DoctorOutputV1
					if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
						t.Fatal(err)
					}
					for _, name := range []string{"config", "state"} {
						want := "ok"
						if broken == name || broken == "both" {
							want = "error"
						}
						found := false
						for _, check := range report.Data {
							if check.Name == name {
								found = true
								if check.Status != want {
									t.Fatalf("%s check=%v", name, check)
								}
							}
						}
						if !found {
							t.Fatalf("missing %s check", name)
						}
					}
				}
			}
		})
	}
}
