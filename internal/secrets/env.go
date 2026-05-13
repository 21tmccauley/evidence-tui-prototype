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
	// Reads are unrestricted: with .env as the canonical store and the
	// keychain backend gone, the TUI no longer needs to gate which keys
	// it can look up. Set/Delete still validate (they return ErrReadOnly
	// here, but the Memory backend uses ValidateKey for keychain-style
	// safety).
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
	// Return every key currently set in the env slice. Callers filter to
	// what they care about; the store doesn't editorialize.
	out := map[string]bool{}
	for _, entry := range e.environ() {
		k, _, ok := strings.Cut(entry, "=")
		if !ok || k == "" {
			continue
		}
		out[k] = true
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
