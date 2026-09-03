package platform

import (
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestResolveUsesValidatedProfileAndOverrides(t *testing.T) {
	base := t.TempDir()
	config := filepath.Join(base, "config")
	state := filepath.Join(base, "state")
	cache := filepath.Join(base, "cache")
	t.Setenv("KCLI_CONFIG_HOME", config)
	t.Setenv("KCLI_STATE_HOME", state)
	t.Setenv("KCLI_CACHE_HOME", cache)
	got, err := Resolve("agent-1")
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{ConfigFile: filepath.Join(config, "kcli", "config.json"), StateDB: filepath.Join(state, "kcli", "profiles", "agent-1", "state.db"), CacheDir: filepath.Join(cache, "kcli", "profiles", "agent-1")}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Fatalf("paths mismatch (-want +got):\n%s", diff)
	}
}
func TestRejectsUnsafeProfileAndOverride(t *testing.T) {
	for _, profile := range []string{"", "../other", "a/b", ".", "name?query"} {
		if err := ValidateProfile(profile); err == nil {
			t.Errorf("ValidateProfile(%q) succeeded", profile)
		}
	}
	t.Setenv("KCLI_CONFIG_HOME", "relative")
	if _, err := Resolve("default"); err == nil {
		t.Fatal("relative override should fail")
	}
}
