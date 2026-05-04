package runner

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// defaultTimeoutSeconds is the per-fetcher subprocess timeout when no env
// override is set. Mirrors run_fetchers.py:174 (`FETCHER_TIMEOUT=300`).
const defaultTimeoutSeconds = 300

// ssllabsScript is the script-key special-cased to a 3600s floor. Qualys
// SSL Labs polls take several minutes per host; the default cap is too
// low. Mirrors run_fetchers.py:184-191.
const ssllabsScript = "ssllabs_tls_scan"

const ssllabsFloorSeconds = 3600

// ResolveTimeout returns the subprocess timeout for the supplied script
// key, applying overrides in this order (highest precedence first):
//
//  1. `<UPPER_SCRIPT_KEY>_TIMEOUT` env var (per-fetcher override)
//  2. `FETCHER_TIMEOUT` env var (global default)
//  3. 300s built-in default
//
// For `ssllabs_tls_scan` only, the resolved value is then floored at
// `SSLLABS_FETCHER_TIMEOUT` (or 3600s if that env var is unset).
func ResolveTimeout(scriptKey string) time.Duration {
	base := envInt("FETCHER_TIMEOUT", defaultTimeoutSeconds)
	if v := envInt(strings.ToUpper(scriptKey)+"_TIMEOUT", 0); v > 0 {
		base = v
	}
	if scriptKey == ssllabsScript {
		floor := envInt("SSLLABS_FETCHER_TIMEOUT", ssllabsFloorSeconds)
		if base < floor {
			base = floor
		}
	}
	return time.Duration(base) * time.Second
}

func envInt(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}
