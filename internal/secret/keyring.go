package secret

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	keyring "github.com/zalando/go-keyring"
)

const (
	MaxCredentialBytes = 2560
	manifestPrefix     = "kcli-chunks-v1:"
)

var ErrNotFound = errors.New("secret not found")

type SecretStore interface {
	Get(profile, name string) (string, error)
	Set(profile, name, value string) error
	Delete(profile, name string) error
}

type Backend interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
	Delete(service, user string) error
}

type osBackend struct{}

func (osBackend) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (osBackend) Set(service, user, password string) error {
	return keyring.Set(service, user, password)
}
func (osBackend) Delete(service, user string) error { return keyring.Delete(service, user) }

type Store struct {
	service string
	backend Backend
}

func New(service string) (*Store, error) { return NewWithBackend(service, osBackend{}) }
func NewWithBackend(service string, backend Backend) (*Store, error) {
	if service == "" || strings.IndexByte(service, 0) >= 0 || backend == nil {
		return nil, fmt.Errorf("invalid secret store configuration")
	}
	return &Store{service: service, backend: backend}, nil
}
func CheckAvailability() error {
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("generate keyring probe: %w", err)
	}
	_, err := keyring.Get("github.com/johannhipp/kcli/availability", hex.EncodeToString(nonce[:]))
	if err == nil || errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return fmt.Errorf("OS keyring unavailable: %w", err)
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validatePart(value string) error {
	if !identifierPattern.MatchString(value) {
		return fmt.Errorf("secret identifier is invalid")
	}
	return nil
}
func entry(profile, name string) (string, error) {
	if err := validatePart(profile); err != nil {
		return "", err
	}
	if err := validatePart(name); err != nil {
		return "", err
	}
	return profile + ":" + name, nil
}
func chunkEntry(base string, n int) string { return base + ".chunk." + fmt.Sprintf("%04d", n) }

func (s *Store) Get(profile, name string) (string, error) {
	base, err := entry(profile, name)
	if err != nil {
		return "", err
	}
	value, err := s.backend.Get(s.service, base)
	if err != nil {
		return "", normalizeNotFound(err)
	}
	count, digest, ok := parseManifest(value)
	if !ok {
		return value, nil
	}
	if count < 2 || count > 4096 {
		return "", ErrNotFound
	}
	var joined strings.Builder
	for i := range count {
		part, getErr := s.backend.Get(s.service, chunkEntry(base, i))
		if getErr != nil {
			if isNotFound(getErr) {
				return "", ErrNotFound
			}
			return "", fmt.Errorf("read secret chunk: %w", getErr)
		}
		joined.WriteString(part)
	}
	result := joined.String()
	sum := sha256.Sum256([]byte(result))
	if hex.EncodeToString(sum[:]) != digest {
		return "", ErrNotFound
	}
	return result, nil
}

func (s *Store) Set(profile, name, value string) error {
	base, err := entry(profile, name)
	if err != nil {
		return err
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("secret value must be valid UTF-8")
	}
	oldCount := s.oldChunkCount(base)
	parts := split(value, MaxCredentialBytes)
	if len(parts) == 1 {
		if err := s.backend.Set(s.service, base, value); err != nil {
			return fmt.Errorf("store secret: %w", err)
		}
		return s.deleteChunks(base, oldCount)
	}
	for i, part := range parts {
		if err := s.backend.Set(s.service, chunkEntry(base, i), part); err != nil {
			for j := range i {
				_ = s.backend.Delete(s.service, chunkEntry(base, j))
			}
			return fmt.Errorf("store secret chunk: %w", err)
		}
	}
	sum := sha256.Sum256([]byte(value))
	manifest := manifestPrefix + strconv.Itoa(len(parts)) + ":" + hex.EncodeToString(sum[:])
	if err := s.backend.Set(s.service, base, manifest); err != nil {
		return fmt.Errorf("store secret manifest: %w", err)
	}
	if oldCount > len(parts) {
		for i := len(parts); i < oldCount; i++ {
			_ = s.backend.Delete(s.service, chunkEntry(base, i))
		}
	}
	return nil
}

func (s *Store) Delete(profile, name string) error {
	base, err := entry(profile, name)
	if err != nil {
		return err
	}
	count := s.oldChunkCount(base)
	if err := s.backend.Delete(s.service, base); err != nil && !isNotFound(err) {
		return fmt.Errorf("delete secret: %w", err)
	}
	return s.deleteChunks(base, count)
}

func (s *Store) oldChunkCount(base string) int {
	value, err := s.backend.Get(s.service, base)
	if err != nil {
		return 0
	}
	count, _, ok := parseManifest(value)
	if !ok || count < 0 || count > 4096 {
		return 0
	}
	return count
}
func (s *Store) deleteChunks(base string, count int) error {
	for i := range count {
		if err := s.backend.Delete(s.service, chunkEntry(base, i)); err != nil && !isNotFound(err) {
			return fmt.Errorf("delete secret chunk: %w", err)
		}
	}
	return nil
}
func parseManifest(value string) (int, string, bool) {
	if !strings.HasPrefix(value, manifestPrefix) {
		return 0, "", false
	}
	parts := strings.Split(strings.TrimPrefix(value, manifestPrefix), ":")
	if len(parts) != 2 || len(parts[1]) != 64 {
		return 0, "", true
	}
	count, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, "", true
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return 0, "", true
	}
	return count, parts[1], true
}
func split(value string, max int) []string {
	if len(value) <= max {
		return []string{value}
	}
	parts := make([]string, 0, (len(value)+max-1)/max)
	for len(value) > max {
		end := max
		for end > 0 && !utf8.RuneStart(value[end]) {
			end--
		}
		parts = append(parts, value[:end])
		value = value[end:]
	}
	return append(parts, value)
}
func isNotFound(err error) bool {
	return errors.Is(err, keyring.ErrNotFound) || errors.Is(err, ErrNotFound)
}
func normalizeNotFound(err error) error {
	if isNotFound(err) {
		return ErrNotFound
	}
	return fmt.Errorf("read secret: %w", err)
}
