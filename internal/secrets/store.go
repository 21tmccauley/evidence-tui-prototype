package secrets

import (
	"errors"
	"fmt"
	"sort"
)

const (
	KeyParamifyUploadAPIToken = "PARAMIFY_UPLOAD_API_TOKEN"
	KeyParamifyAPIBaseURL     = "PARAMIFY_API_BASE_URL"
	KeyKnowBe4APIKey          = "KNOWBE4_API_KEY"
)

var knownKeys = []string{
	KeyParamifyUploadAPIToken,
	KeyParamifyAPIBaseURL,
	KeyKnowBe4APIKey,
}

// ErrNotConfigured is returned when the operator has not supplied an upload
// token via the supported configuration path.
var ErrNotConfigured = errors.New("PARAMIFY_UPLOAD_API_TOKEN is not set")

// ErrReadOnly indicates the store does not support mutation.
var ErrReadOnly = errors.New("secrets store is read-only")

// Store supplies sensitive values without ever logging them.
//
// List returns key names only and never secret values.
type Store interface {
	Get(key string) (value string, found bool, err error)
	Set(key, value string) error
	Delete(key string) error
	List() ([]string, error)
	Source() string
	// Writable reports whether Set/Delete are supported. Read-only stores
	// (e.g. process environment) return false so the UI can disable edits.
	Writable() bool
	// Locate reports which backend currently holds key. Used by the UI to
	// show provenance ("set (env)" vs "set (keychain)") in merged setups.
	Locate(key string) (source string, found bool, err error)
	ParamifyUploadAPIToken() (string, error)
}

// ServiceSecret describes one secret surfaced in the TUI.
type ServiceSecret struct {
	ServiceID   string
	ServiceName string
	Key         string
	Optional    bool
	Description string
}

var serviceSecrets = []ServiceSecret{
	{
		ServiceID:   "paramify",
		ServiceName: "Paramify",
		Key:         KeyParamifyUploadAPIToken,
		Optional:    false,
		Description: "required to upload evidence sets",
	},
	{
		ServiceID:   "paramify",
		ServiceName: "Paramify",
		Key:         KeyParamifyAPIBaseURL,
		Optional:    true,
		Description: "optional API URL override",
	},
	{
		ServiceID:   "knowbe4",
		ServiceName: "KnowBe4",
		Key:         KeyKnowBe4APIKey,
		Optional:    false,
		Description: "required for KnowBe4 scans",
	},
}

// ValidateKey rejects unknown secret keys so callers cannot accidentally store
// unrelated process environment in keychain-backed stores.
func ValidateKey(key string) error {
	for _, k := range knownKeys {
		if key == k {
			return nil
		}
	}
	return fmt.Errorf("unknown secret key %q", key)
}

func keysForSet(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func requiredRuntimeKeys() []string {
	return []string{
		KeyParamifyUploadAPIToken,
		KeyParamifyAPIBaseURL,
		KeyKnowBe4APIKey,
	}
}

// RuntimeKeys returns all keys currently injected into child subprocess env.
func RuntimeKeys() []string {
	keys := requiredRuntimeKeys()
	out := make([]string, len(keys))
	copy(out, keys)
	return out
}

// KnownServiceSecrets returns canonical service/key metadata for the TUI.
func KnownServiceSecrets() []ServiceSecret {
	out := make([]ServiceSecret, len(serviceSecrets))
	copy(out, serviceSecrets)
	return out
}

func paramifyUploadTokenFromStore(s Store) (string, error) {
	v, ok, err := s.Get(KeyParamifyUploadAPIToken)
	if err != nil {
		return "", err
	}
	if !ok || v == "" {
		return "", ErrNotConfigured
	}
	return v, nil
}
