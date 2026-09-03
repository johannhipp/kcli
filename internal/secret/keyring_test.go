package secret

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

type memoryBackend struct {
	mu     sync.Mutex
	values map[string]string
}

func (m *memoryBackend) key(service, user string) string { return service + "\x00" + user }
func (m *memoryBackend) Get(service, user string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	value, ok := m.values[m.key(service, user)]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}
func (m *memoryBackend) Set(service, user, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values[m.key(service, user)] = password
	return nil
}
func (m *memoryBackend) Delete(service, user string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := m.key(service, user)
	if _, ok := m.values[key]; !ok {
		return ErrNotFound
	}
	delete(m.values, key)
	return nil
}

func TestChunkedRoundTripAndPartialSet(t *testing.T) {
	backend := &memoryBackend{values: map[string]string{}}
	store, err := NewWithBackend("kcli-test", backend)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Repeat("token", 820)
	if err := store.Set("profile", "refresh", want); err != nil {
		t.Fatal(err)
	}
	for key, value := range backend.values {
		if strings.Contains(key, ".chunk.") && len(value) > MaxCredentialBytes {
			t.Fatalf("chunk is %d bytes", len(value))
		}
	}
	got, err := store.Get("profile", "refresh")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("round trip got %d bytes, want %d", len(got), len(want))
	}
	for key := range backend.values {
		if strings.Contains(key, ".chunk.") {
			delete(backend.values, key)
			break
		}
	}
	if _, err := store.Get("profile", "refresh"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("partial chunks returned %v", err)
	}
}
func TestChunkedValueCanBeReplacedByShortValue(t *testing.T) {
	backend := &memoryBackend{values: map[string]string{}}
	store, _ := NewWithBackend("kcli-test", backend)
	if err := store.Set("profile", "access", strings.Repeat("a", 4096)); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("profile", "access", "short"); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("profile", "access")
	if err != nil || got != "short" {
		t.Fatalf("got %q, %v", got, err)
	}
	for key := range backend.values {
		if strings.Contains(key, ".chunk.") {
			t.Fatalf("stale chunk %q", key)
		}
	}
}
