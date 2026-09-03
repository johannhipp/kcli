//go:build testing

package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type fileBackend struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path, service string) (*Store, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("testing secret path must be absolute")
	}
	return NewWithBackend(service, &fileBackend{path: path})
}

func (f *fileBackend) Get(service, user string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	values, err := f.load()
	if err != nil {
		return "", err
	}
	value, ok := values[service+"\x00"+user]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}
func (f *fileBackend) Set(service, user, password string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	values, err := f.load()
	if err != nil {
		return err
	}
	values[service+"\x00"+user] = password
	return f.save(values)
}
func (f *fileBackend) Delete(service, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	values, err := f.load()
	if err != nil {
		return err
	}
	key := service + "\x00" + user
	if _, ok := values[key]; !ok {
		return ErrNotFound
	}
	delete(values, key)
	return f.save(values)
}
func (f *fileBackend) load() (map[string]string, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var values map[string]string
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, err
	}
	return values, nil
}
func (f *fileBackend) save(values map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(values)
	if err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, f.path)
}
