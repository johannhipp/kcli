package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/johannhipp/kcli/internal/platform"
)

type Config struct {
	Global   map[string]string            `json:"global,omitempty"`
	Profiles map[string]map[string]string `json:"profiles,omitempty"`
}
type EffectiveValue struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

var defaultConfigValues = map[string]string{"output": "auto", "timeout": "25s", "no_color": "false", "quiet": "false"}

type ConfigChange struct {
	Key    string  `json:"key"`
	Old    *string `json:"old"`
	New    string  `json:"new"`
	DryRun bool    `json:"dry_run"`
}
type ConfigStore struct{ path string }

func NewConfigStore(path string) (*ConfigStore, error) {
	if path == "" || !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.IndexByte(path, 0) >= 0 {
		return nil, fmt.Errorf("config path must be a clean absolute path")
	}
	return &ConfigStore{path: path}, nil
}
func (s *ConfigStore) Path() string { return s.path }
func (s *ConfigStore) Load() (Config, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return Config{Global: map[string]string{}, Profiles: map[string]map[string]string{}}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	if len(data) > 1<<20 {
		return Config{}, fmt.Errorf("config exceeds 1 MiB")
	}
	var config Config
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode config: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return Config{}, fmt.Errorf("decode config: trailing data")
	}
	if config.Global == nil {
		config.Global = map[string]string{}
	}
	if config.Profiles == nil {
		config.Profiles = map[string]map[string]string{}
	}
	for key, value := range config.Global {
		if err := validateConfigValue(key, value); err != nil {
			return Config{}, err
		}
	}
	for profile, values := range config.Profiles {
		if err := platform.ValidateProfile(profile); err != nil {
			return Config{}, fmt.Errorf("invalid profile in config: %w", err)
		}
		for key, value := range values {
			if err := validateConfigValue(key, value); err != nil {
				return Config{}, err
			}
		}
	}
	return config, nil
}
func (s *ConfigStore) List(profile string) (map[string]EffectiveValue, error) {
	if err := platform.ValidateProfile(profile); err != nil {
		return nil, err
	}
	config, err := s.Load()
	if err != nil {
		return nil, err
	}
	result := make(map[string]EffectiveValue, len(defaultConfigValues))
	for key, value := range defaultConfigValues {
		result[key] = EffectiveValue{Value: value, Source: "default"}
	}
	for key, value := range config.Global {
		result[key] = EffectiveValue{Value: value, Source: "global"}
	}
	for key, value := range config.Profiles[profile] {
		result[key] = EffectiveValue{Value: value, Source: "profile:" + profile}
	}
	return result, nil
}
func (s *ConfigStore) Get(profile, key string) (EffectiveValue, error) {
	if strings.HasPrefix(key, "global.") {
		key = strings.TrimPrefix(key, "global.")
		if err := validateConfigValue(key, defaultValue(key)); err != nil {
			return EffectiveValue{}, err
		}
		config, err := s.Load()
		if err != nil {
			return EffectiveValue{}, err
		}
		if value, ok := config.Global[key]; ok {
			return EffectiveValue{Value: value, Source: "global"}, nil
		}
		return EffectiveValue{Value: defaultValue(key), Source: "default"}, nil
	}
	values, err := s.List(profile)
	if err != nil {
		return EffectiveValue{}, err
	}
	value, ok := values[key]
	if !ok {
		return EffectiveValue{}, fmt.Errorf("unknown config key %q", key)
	}
	return value, nil
}
func (s *ConfigStore) Set(profile, key, value string, dryRun bool) (ConfigChange, error) {
	if err := platform.ValidateProfile(profile); err != nil {
		return ConfigChange{}, err
	}
	global := strings.HasPrefix(key, "global.")
	key = strings.TrimPrefix(key, "global.")
	if err := validateConfigValue(key, value); err != nil {
		return ConfigChange{}, err
	}
	config, err := s.Load()
	if err != nil {
		return ConfigChange{}, err
	}
	var target map[string]string
	if global {
		target = config.Global
	} else {
		if config.Profiles[profile] == nil {
			config.Profiles[profile] = map[string]string{}
		}
		target = config.Profiles[profile]
	}
	oldValue, exists := target[key]
	var old *string
	if exists {
		copy := oldValue
		old = &copy
	}
	change := ConfigChange{Key: key, Old: old, New: value, DryRun: dryRun}
	if dryRun {
		return change, nil
	}
	target[key] = value
	if err := s.save(config); err != nil {
		return ConfigChange{}, err
	}
	return change, nil
}
func validateConfigValue(key, value string) error {
	switch key {
	case "output":
		if value != "auto" && value != "table" && value != "json" && value != "ndjson" && value != "raw" {
			return fmt.Errorf("output must be auto, table, json, ndjson, or raw")
		}
	case "timeout":
		duration, err := time.ParseDuration(value)
		if err != nil || duration <= 0 || duration > 60*time.Second {
			return fmt.Errorf("timeout must be a positive duration no greater than 60s")
		}
	case "no_color", "quiet":
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Errorf("%s must be true or false", key)
		}
	default:
		return fmt.Errorf("unknown config key %q", key)
	}
	return nil
}
func defaultValue(key string) string { return defaultConfigValues[key] }
func (s *ConfigStore) save(config Config) error {
	if err := platform.EnsurePrivateDir(filepath.Dir(s.path)); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".config-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("protect temporary config: %w", err)
	}
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write config: %w", err)
	}
	if err := os.Rename(tmp.Name(), s.path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("replace config: %w", err)
	}
	dir, err := os.Open(filepath.Dir(s.path))
	if err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return os.Chmod(s.path, 0o600)
}
