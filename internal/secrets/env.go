package secrets

import (
	"os"
	"strings"
)

// Env reads known secret keys from process environment.
type Env struct {
	Environ []string
}

func (e Env) Source() string { return "env" }

func (e Env) Writable() bool { return false }

func (e Env) Locate(key string) (string, bool, error) {
	_, found, err := e.Get(key)
	if err != nil || !found {
		return "", false, err
	}
	return e.Source(), true, nil
}

func (e Env) ParamifyUploadAPIToken() (string, error) {
	return paramifyUploadTokenFromStore(e)
}

func (e Env) Get(key string) (string, bool, error) {
	if err := ValidateKey(key); err != nil {
		return "", false, err
	}
	env := e.environ()
	for _, entry := range env {
		k, v, ok := strings.Cut(entry, "=")
		if !ok || k != key {
			continue
		}
		v = strings.TrimSpace(v)
		if v == "" {
			return "", false, nil
		}
		return v, true, nil
	}
	return "", false, nil
}

func (e Env) Set(_, _ string) error { return ErrReadOnly }

func (e Env) Delete(_ string) error { return ErrReadOnly }

func (e Env) List() ([]string, error) {
	out := map[string]bool{}
	for _, key := range requiredRuntimeKeys() {
		_, found, err := e.Get(key)
		if err != nil {
			return nil, err
		}
		if found {
			out[key] = true
		}
	}
	return keysForSet(out), nil
}

func (e Env) environ() []string {
	if e.Environ == nil {
		return os.Environ()
	}
	out := make([]string, len(e.Environ))
	copy(out, e.Environ)
	return out
}
