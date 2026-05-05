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

// sourceSecretsTable is the canonical mapping from a catalog source (plus
// the synthetic "paramify" pseudo-source) to the secrets the TUI knows
// about. New keys must be added here so they flow into RuntimeKeys() and
// ValidateKey() automatically.
var sourceSecretsTable = []SourceSecrets{
	{
		Source: SourceParamify,
		Label:  "Paramify",
		Keys: []ServiceSecret{
			{ServiceID: SourceParamify, ServiceName: "Paramify", Key: KeyParamifyUploadAPIToken, Optional: false, Description: "required to upload evidence sets"},
			{ServiceID: SourceParamify, ServiceName: "Paramify", Key: KeyParamifyAPIBaseURL, Optional: true, Description: "optional API URL override"},
		},
	},
	{
		Source: "aws",
		Label:  "AWS",
		Note:   "managed via ~/.aws / aws CLI; no env key configured here",
	},
	{
		Source: "k8s",
		Label:  "Kubernetes",
		Note:   "managed via kubeconfig / kubectl; no env key configured here",
	},
	{
		Source: "okta",
		Label:  "Okta",
		Keys: []ServiceSecret{
			{ServiceID: "okta", ServiceName: "Okta", Key: KeyOktaAPIToken, Optional: false, Description: "required for Okta API calls"},
			{ServiceID: "okta", ServiceName: "Okta", Key: KeyOktaOrgURL, Optional: false, Description: "Okta org URL (https://your-org.okta.com)"},
		},
	},
	{
		Source: "knowbe4",
		Label:  "KnowBe4",
		Keys: []ServiceSecret{
			{ServiceID: "knowbe4", ServiceName: "KnowBe4", Key: KeyKnowBe4APIKey, Optional: false, Description: "required for KnowBe4 scans"},
		},
	},
	{
		Source: "rippling",
		Label:  "Rippling",
		Keys: []ServiceSecret{
			{ServiceID: "rippling", ServiceName: "Rippling", Key: KeyRipplingAPIToken, Optional: false, Description: "required for Rippling API calls"},
		},
	},
	{
		Source: "sentinelone",
		Label:  "SentinelOne",
		Keys: []ServiceSecret{
			{ServiceID: "sentinelone", ServiceName: "SentinelOne", Key: KeySentinelOneAPIURL, Optional: false, Description: "SentinelOne API URL"},
			{ServiceID: "sentinelone", ServiceName: "SentinelOne", Key: KeySentinelOneAPIToken, Optional: false, Description: "SentinelOne API token"},
		},
	},
	{
		Source: "gitlab",
		Label:  "GitLab",
		Note:   "uses multi-instance env: GITLAB_PROJECT_<N>_URL and GITLAB_PROJECT_<N>_API_ACCESS_TOKEN; set in your shell",
	},
	{
		Source: "checkov",
		Label:  "Checkov",
		Note:   "no creds for local scans; reuses GitLab tokens for repo scans",
	},
	{
		Source: "ssllabs",
		Label:  "SSL Labs",
		Note:   "public Qualys SSL Labs API; no credentials needed",
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
