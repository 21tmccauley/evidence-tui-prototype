package secrets

import (
	"sort"
	"sync"
)

// SourceParamify is the pinned pseudo-source for upload credentials. It
// does not correspond to a catalog category; it represents the upload
// integration that all runs share.
const SourceParamify = "paramify"

// SourceSecrets describes the keys (or info) a catalog source surfaces in
// the Secrets screen. Keys may be empty for sources that manage their own
// credentials (e.g. AWS via the CLI, ssllabs has none).
type SourceSecrets struct {
	Source string          // catalog category key, or SourceParamify
	Label  string          // display name shown on the Secrets screen
	Note   string          // info text for sources without env keys
	Keys   []ServiceSecret // empty for info-only sources
}

// HasKeys reports whether the source has at least one editable key.
func (s SourceSecrets) HasKeys() bool { return len(s.Keys) > 0 }

// sourceSecretsTable now only declares the TUI's own integrations — currently
// the Paramify upload pseudo-source. Per-platform fetcher secrets are
// discovered from the filesystem (each fetcher folder's .env.example or
// platform.json) and surfaced in the Secrets screen via the platforms
// package, not via this table.
var sourceSecretsTable = []SourceSecrets{
	{
		Source: SourceParamify,
		Label:  "Paramify",
		Keys: []ServiceSecret{
			{ServiceID: SourceParamify, ServiceName: "Paramify", Key: KeyParamifyUploadAPIToken, Optional: false},
			{ServiceID: SourceParamify, ServiceName: "Paramify", Key: KeyParamifyAPIBaseURL, Optional: true},
		},
	},
}

// SecretsForSource returns the source's row, or a synthesized info-only row
// when the source isn't in the table (so the UI never lies about coverage).
func SecretsForSource(source string) SourceSecrets {
	for _, ss := range sourceSecretsTable {
		if ss.Source == source {
			return cloneSourceSecrets(ss)
		}
	}
	return SourceSecrets{
		Source: source,
		Label:  source,
		Note:   "no secrets configured for this source yet",
	}
}

// AllSourceSecrets returns a copy of the canonical source -> secrets table.
func AllSourceSecrets() []SourceSecrets {
	out := make([]SourceSecrets, len(sourceSecretsTable))
	for i, ss := range sourceSecretsTable {
		out[i] = cloneSourceSecrets(ss)
	}
	return out
}

// AllSecretKeys returns every key declared in the table, sorted and deduped.
// Used to derive RuntimeKeys() and the ValidateKey allowlist so the table
// stays the single source of truth for what the TUI knows about.
func AllSecretKeys() []string {
	keys := allKeysCached()
	out := make([]string, len(keys))
	copy(out, keys)
	return out
}

var (
	allKeysOnce  sync.Once
	allKeysCache []string
)

// allKeysCached returns the cached, deduped, sorted slice of keys backing
// AllSecretKeys() / ValidateKey() without copying.
func allKeysCached() []string {
	allKeysOnce.Do(func() {
		set := map[string]bool{}
		for _, ss := range sourceSecretsTable {
			for _, k := range ss.Keys {
				set[k.Key] = true
			}
		}
		out := make([]string, 0, len(set))
		for k := range set {
			out = append(out, k)
		}
		sort.Strings(out)
		allKeysCache = out
	})
	return allKeysCache
}

func cloneSourceSecrets(ss SourceSecrets) SourceSecrets {
	out := ss
	if len(ss.Keys) > 0 {
		out.Keys = append([]ServiceSecret(nil), ss.Keys...)
	}
	return out
}
