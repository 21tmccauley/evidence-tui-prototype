package platforms

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
)

// Join produces a unified catalog.Script slice from filesystem discovery and
// existing catalog metadata. Filesystem discovery is the source of truth for
// what scripts exist; the catalog supplies optional metadata (controls,
// validation_rules, EVD IDs, names) for scripts that already have entries.
// Scripts on disk that lack a catalog entry get a synthesized minimal entry
// so the runner can still execute them.
//
// repoRoot is the root the filesystem paths are relative to (typically the
// fetcher-repo-root flag value). It is used to compute script_file values
// that match the catalog's relative-path convention.
func Join(repoRoot string, plats []Platform, catScripts []catalog.Script) []catalog.Script {
	abs, err := filepath.Abs(repoRoot)
	if err != nil {
		abs = repoRoot
	}

	byFile := make(map[string]catalog.Script, len(catScripts))
	for _, s := range catScripts {
		if s.ScriptFile == "" {
			continue
		}
		byFile[filepath.ToSlash(s.ScriptFile)] = s
	}

	seen := make(map[string]bool, len(catScripts))
	out := make([]catalog.Script, 0, len(catScripts))

	for _, p := range plats {
		for _, f := range p.Fetchers {
			rel, err := filepath.Rel(abs, f.Path)
			if err != nil {
				rel = f.Path
			}
			rel = filepath.ToSlash(rel)
			if existing, ok := byFile[rel]; ok {
				out = append(out, existing)
				seen[existing.ID] = true
				continue
			}
			stem := scriptStem(f.Path)
			s := catalog.Script{
				ID:         synthesizeID(p.ID, stem),
				Name:       f.Name,
				ScriptFile: rel,
				Source:     p.ID,
				Key:        stem,
			}
			out = append(out, s)
			seen[s.ID] = true
		}
	}

	// Preserve any catalog scripts whose files are not on disk (e.g. catalog
	// declares a script that hasn't landed yet, or the repoRoot points at a
	// partial checkout). Keeping them avoids losing upload metadata mid-rollout.
	for _, s := range catScripts {
		if !seen[s.ID] {
			out = append(out, s)
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// synthesizeID builds an EVD-shaped ID for a filesystem-discovered script
// that has no catalog entry. The shape matches the loader's regex
// (^EVD-[A-Z0-9]+(-[A-Z0-9]+)+$) so the result round-trips through the
// catalog renderer.
func synthesizeID(platform, stem string) string {
	clean := func(s string) string {
		s = strings.ToUpper(s)
		s = strings.ReplaceAll(s, "_", "-")
		// Drop any other characters the loader's regex would reject.
		var b strings.Builder
		for _, r := range s {
			switch {
			case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
				b.WriteRune(r)
			}
		}
		out := b.String()
		// Collapse double dashes left by stripping characters.
		for strings.Contains(out, "--") {
			out = strings.ReplaceAll(out, "--", "-")
		}
		return strings.Trim(out, "-")
	}
	p := clean(platform)
	s := clean(stem)
	if p == "" {
		p = "X"
	}
	if s == "" {
		s = "X"
	}
	return "EVD-" + p + "-" + s
}

func scriptStem(path string) string {
	base := filepath.Base(path)
	for _, ext := range fetcherExtensions {
		if strings.HasSuffix(base, ext) {
			return strings.TrimSuffix(base, ext)
		}
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}
