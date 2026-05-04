package secrets

import (
	"errors"
	"os"
	"strings"
)

// ErrNotConfigured is returned when the operator has not supplied an upload
// token via the supported configuration path.
var ErrNotConfigured = errors.New("PARAMIFY_UPLOAD_API_TOKEN is not set")

// Store supplies sensitive values without ever logging them.
type Store interface {
	ParamifyUploadAPIToken() (string, error)
}

// Env reads the Paramify upload token from the process environment. This is
// the bootstrap path; a keychain-backed implementation can satisfy the same
// interface later without changing callers.
type Env struct{}

// ParamifyUploadAPIToken returns PARAMIFY_UPLOAD_API_TOKEN when non-empty.
func (Env) ParamifyUploadAPIToken() (string, error) {
	v := strings.TrimSpace(os.Getenv("PARAMIFY_UPLOAD_API_TOKEN"))
	if v == "" {
		return "", ErrNotConfigured
	}
	return v, nil
}
