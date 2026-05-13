package platforms

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
)

// EnrichFromRootEnvExample reads <repoRoot>/.env.example and assigns its
// declared keys to platforms by case-insensitive prefix match against the
// platform ID. The shipped evidence-fetchers repo bundles every platform's
// keys into one root-level .env.example template (with most keys commented
// out as `# KEY=value`); this lets the Secrets screen surface them per
// platform without requiring per-folder declaration files.
//
// Keys that don't prefix-match any platform are dropped (e.g. PARAMIFY_*
// is handled by the pseudo-source pinned in Secrets, and global keys like
// FETCHER_TIMEOUT are not per-platform).
//
// Per-platform declarations (.env.example or platform.json inside the
// folder) still win — this function only adds keys, never replaces.
func EnrichFromRootEnvExample(plats []Platform, repoRoot string) []Platform {
	if len(plats) == 0 || repoRoot == "" {
		return plats
	}
	path := filepath.Join(repoRoot, ".env.example")
	data, err := os.ReadFile(path)
	if err != nil {
		return plats
	}
	keys := extractDeclaredKeys(string(data))
	if len(keys) == 0 {
		return plats
	}

	// Sort platform IDs by length descending so longer IDs win over shorter
	// prefixes (e.g. "aws_govcloud" would shadow "aws" if both existed).
	idx := make([]int, len(plats))
	for i := range plats {
		idx[i] = i
	}
	sort.Slice(idx, func(i, j int) bool {
		return len(plats[idx[i]].ID) > len(plats[idx[j]].ID)
	})

	for _, key := range keys {
		upper := strings.ToUpper(key)
		for _, i := range idx {
			platformPrefix := strings.ToUpper(plats[i].ID) + "_"
			if strings.HasPrefix(upper, platformPrefix) || strings.EqualFold(key, plats[i].ID) {
				if !envKeysContain(plats[i].EnvKeys, key) {
					plats[i].EnvKeys = append(plats[i].EnvKeys, EnvKey{Name: key})
				}
				break
			}
		}
	}
	return plats
}

// EnrichFromCatalog upgrades the DisplayName on platforms whose folder ID
// matches a catalog category. The catalog supplies human-readable names
// like "Amazon Web Services" that beat the humanized folder ID ("Aws").
func EnrichFromCatalog(plats []Platform, cat *catalog.Catalog) []Platform {
	if cat == nil || len(plats) == 0 {
		return plats
	}
	for i := range plats {
		c, ok := cat.Wrapper.Categories[plats[i].ID]
		if !ok || strings.TrimSpace(c.Name) == "" {
			continue
		}
		plats[i].DisplayName = c.Name
	}
	return plats
}

// extractDeclaredKeys parses .env.example-style content and returns every
// declared key name in order of first appearance. Both commented (`# KEY=`)
// and uncommented (`KEY=`) forms count as declarations — the shipped repo
// uses the commented style for keys the operator is expected to fill in.
func extractDeclaredKeys(body string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// Strip a leading "# " (template-style declaration) if present, but
		// only if the remainder still looks like `KEY=...`. Avoids picking
		// up prose comments that happen to contain `=`.
		if strings.HasPrefix(line, "#") {
			rest := strings.TrimLeft(strings.TrimPrefix(line, "#"), " \t")
			if !looksLikeAssignment(rest) {
				continue
			}
			line = rest
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:eq])
		if name == "" || seen[name] || !isValidEnvKey(name) {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func looksLikeAssignment(s string) bool {
	eq := strings.IndexByte(s, '=')
	if eq <= 0 {
		return false
	}
	return isValidEnvKey(strings.TrimSpace(s[:eq]))
}

// isValidEnvKey rejects strings that contain characters env var names
// never have (spaces, punctuation). Allows uppercase letters, digits,
// underscores; first char must be a letter or underscore.
func isValidEnvKey(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r == '_':
			// always ok
		case r >= 'a' && r <= 'z':
			// allow lowercase too — some repos use mixed case
		case r >= '0' && r <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

func envKeysContain(keys []EnvKey, name string) bool {
	for _, k := range keys {
		if k.Name == name {
			return true
		}
	}
	return false
}
