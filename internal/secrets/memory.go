package secrets

import (
	"sort"
	"strings"
	"sync"
)

// Memory is a test-only in-memory secret backend.
type Memory struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewMemory() *Memory {
	return &Memory{data: map[string]string{}}
}

func (m *Memory) Source() string { return "memory" }

func (m *Memory) Writable() bool { return true }

func (m *Memory) Locate(key string) (string, bool, error) {
	_, found, err := m.Get(key)
	if err != nil || !found {
		return "", false, err
	}
	return m.Source(), true, nil
}

func (m *Memory) ParamifyUploadAPIToken() (string, error) {
	return paramifyUploadTokenFromStore(m)
}

func (m *Memory) Get(key string) (string, bool, error) {
	if err := ValidateKey(key); err != nil {
		return "", false, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[key]
	if !ok {
		return "", false, nil
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false, nil
	}
	return v, true, nil
}

func (m *Memory) Set(key, value string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	m.mu.Lock()
	defer m.mu.Unlock()
	if value == "" {
		delete(m.data, key)
		return nil
	}
	m.data[key] = value
	return nil
}

func (m *Memory) Delete(key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *Memory) List() ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	keys := make([]string, 0, len(m.data))
	for k := range m.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}
