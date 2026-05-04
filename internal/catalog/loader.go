package catalog

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
)

// EVD-* id shape per .cursor/rules/50-catalog.mdc.
var idShape = regexp.MustCompile(`^EVD-[A-Z0-9]+(-[A-Z0-9]+)+$`)

// Load parses a catalog, validates EVD ids, dedupes, sets Source/Key, stable-sorts.
func Load(r io.Reader) (*Catalog, []Script, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, fmt.Errorf("read catalog: %w", err)
	}
	var c Catalog
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, nil, fmt.Errorf("decode catalog: %w", err)
	}
	scripts, err := flatten(&c)
	if err != nil {
		return nil, nil, err
	}
	return &c, scripts, nil
}

func LoadFile(path string) (*Catalog, []Script, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open catalog %q: %w", path, err)
	}
	defer f.Close()
	return Load(f)
}

// flatten validates ids, rejects duplicates, sets Source and Key, stable-sorts.
func flatten(c *Catalog) ([]Script, error) {
	if c == nil {
		return nil, fmt.Errorf("nil catalog")
	}
	seen := map[string]string{} // id -> "<source>/<key>" for duplicate diagnostics

	categoryKeys := make([]string, 0, len(c.Wrapper.Categories))
	for k := range c.Wrapper.Categories {
		categoryKeys = append(categoryKeys, k)
	}
	sort.Strings(categoryKeys)

	out := []Script{}
	for _, ck := range categoryKeys {
		cat := c.Wrapper.Categories[ck]

		scriptKeys := make([]string, 0, len(cat.Scripts))
		for k := range cat.Scripts {
			scriptKeys = append(scriptKeys, k)
		}
		sort.Strings(scriptKeys)

		staged := make([]Script, 0, len(scriptKeys))
		for _, sk := range scriptKeys {
			s := cat.Scripts[sk]
			s.Source = ck
			s.Key = sk
			if !idShape.MatchString(s.ID) {
				return nil, fmt.Errorf("invalid id %q at %s/%s: must match EVD-<UPPER>-<UPPER>", s.ID, ck, sk)
			}
			if where, dup := seen[s.ID]; dup {
				return nil, fmt.Errorf("duplicate id %q: first at %s, again at %s/%s", s.ID, where, ck, sk)
			}
			seen[s.ID] = ck + "/" + sk
			staged = append(staged, s)
		}
		sort.SliceStable(staged, func(i, j int) bool { return staged[i].ID < staged[j].ID })
		out = append(out, staged...)
	}
	return out, nil
}
