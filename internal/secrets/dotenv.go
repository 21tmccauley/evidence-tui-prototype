package secrets

import (
	"fmt"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

// MergeEnvFile reads path as a dotenv file and appends values missing from base.
// Existing process environment values win, matching python-dotenv override=false.
func MergeEnvFile(base []string, path string) ([]string, error) {
	values, err := godotenv.Read(path)
	if err != nil {
		return nil, fmt.Errorf("read env file %q: %w", path, err)
	}
	return MergeEnvValues(base, values), nil
}

// MergeEnvValues appends dotenv-style values that are not already present in base.
// It accepts arbitrary keys because runtime config includes non-secret vars like
// GITLAB_PROJECT_<N>_* and CHECKOV_* in addition to TUI-managed secret keys.
func MergeEnvValues(base []string, values map[string]string) []string {
	out := make([]string, 0, len(base)+len(values))
	present := map[string]bool{}
	for _, entry := range base {
		out = append(out, entry)
		key, _, ok := strings.Cut(entry, "=")
		if ok {
			present[key] = true
		}
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if present[key] {
			continue
		}
		out = append(out, key+"="+values[key])
	}
	return out
}
