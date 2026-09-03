package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

var profilePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func ValidateProfile(name string) error {
	if !profilePattern.MatchString(name) || name == "." || name == ".." {
		return fmt.Errorf("profile must match %s", profilePattern.String())
	}
	return nil
}

type Paths struct {
	ConfigFile string
	StateDB    string
	CacheDir   string
}

func Resolve(profile string) (Paths, error) {
	if err := ValidateProfile(profile); err != nil {
		return Paths{}, err
	}
	configBase, err := basePath("KCLI_CONFIG_HOME", userConfigDir)
	if err != nil {
		return Paths{}, err
	}
	stateBase, err := basePath("KCLI_STATE_HOME", userStateDir)
	if err != nil {
		return Paths{}, err
	}
	cacheBase, err := basePath("KCLI_CACHE_HOME", os.UserCacheDir)
	if err != nil {
		return Paths{}, err
	}
	return Paths{
		ConfigFile: filepath.Join(configBase, "kcli", "config.json"),
		StateDB:    filepath.Join(stateBase, "kcli", "profiles", profile, "state.db"),
		CacheDir:   filepath.Join(cacheBase, "kcli", "profiles", profile),
	}, nil
}

func basePath(envName string, fallback func() (string, error)) (string, error) {
	if value := os.Getenv(envName); value != "" {
		if strings.IndexByte(value, 0) >= 0 || !filepath.IsAbs(value) || filepath.Clean(value) != value {
			return "", fmt.Errorf("%s must be a clean absolute path", envName)
		}
		return value, nil
	}
	return fallback()
}

func userConfigDir() (string, error) { return os.UserConfigDir() }

func userStateDir() (string, error) {
	if runtime.GOOS == "linux" {
		if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
			if !filepath.IsAbs(xdg) || filepath.Clean(xdg) != xdg {
				return "", fmt.Errorf("XDG_STATE_HOME must be a clean absolute path")
			}
			return xdg, nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "state"), nil
	}
	return os.UserConfigDir()
}

func EnsurePrivateDir(path string) error {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return fmt.Errorf("invalid empty or NUL-containing path")
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}
