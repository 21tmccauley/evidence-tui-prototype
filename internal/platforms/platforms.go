// Package platforms discovers fetcher platforms from a directory layout.
//
// The contract is filesystem-first: a "platform" is any subdirectory under
// <repoRoot>/fetchers/, and a "fetcher" is any *.py file inside one. Optional
// per-platform metadata lives in platform.json (display name, env-key
// declarations) and .env.example (env-key declarations alone).
//
// The TUI is intentionally agnostic about what a platform "is" — it does not
// know about AWS, Okta, or any specific service. Adding a new platform is a
// filesystem operation: drop a folder, drop a Python file.
package platforms

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Platform is one fetcher folder.
type Platform struct {
	ID          string    // folder name (e.g., "aws")
	DisplayName string    // platform.json display_name, else humanized ID
	Path        string    // absolute path to the folder
	EnvKeys     []EnvKey  // declared env keys, in declaration order
	Fetchers    []Fetcher // scripts in the folder
}

// EnvKey is one declared environment variable that the platform's fetchers
// expect. The TUI surfaces these in the Secrets screen so users can see what
// needs to be set in their .env.
type EnvKey struct {
	Name     string
	Optional bool
}

// Fetcher is one runnable script inside a platform folder.
type Fetcher struct {
	ID         string // "<platform>/<script-stem>"
	Name       string // humanized stem
	Path       string // absolute path to the script
	PlatformID string
}

// Manifest is the on-disk shape of platform.json. All fields are optional.
//
// Unknown fields are ignored so the schema can grow without breaking older
// TUI builds. env_keys accepts either a bare string ("FOO") or an object
// ({"name": "FOO", "optional": true}) — the discriminated form is reserved
// for future flags like `secret: true`.
type Manifest struct {
	DisplayName string   `json:"display_name,omitempty"`
	EnvKeys     []EnvKey `json:"env_keys,omitempty"`
}

// UnmarshalJSON for EnvKey accepts the bare-string and object forms.
func (k *EnvKey) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		k.Name = s
		return nil
	}
	var obj struct {
		Name     string `json:"name"`
		Optional bool   `json:"optional"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	k.Name = obj.Name
	k.Optional = obj.Optional
	return nil
}

// Discover walks <repoRoot>/fetchers/ and returns one Platform per subdirectory,
// sorted by ID. Each platform's fetchers are sorted by ID. A missing fetchers
// directory returns an empty slice with no error — discovery is best-effort and
// it is valid to point the TUI at a repo that has not yet grown any fetchers.
func Discover(repoRoot string) ([]Platform, error) {
	if strings.TrimSpace(repoRoot) == "" {
		return nil, fmt.Errorf("repoRoot is empty")
	}
	root, err := filepath.Abs(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve repoRoot: %w", err)
	}
	fetchersDir := filepath.Join(root, "fetchers")
	entries, err := os.ReadDir(fetchersDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []Platform{}, nil
		}
		return nil, fmt.Errorf("read %s: %w", fetchersDir, err)
	}

	platforms := make([]Platform, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		p, err := readPlatform(filepath.Join(fetchersDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("platform %s: %w", e.Name(), err)
		}
		// A folder with no fetchers AND no declared env keys is almost
		// certainly a helper directory (logos, common, etc.), not a real
		// platform. Skip it. Per-platform manifests can override this by
		// declaring env keys even with no scripts.
		if len(p.Fetchers) == 0 && len(p.EnvKeys) == 0 {
			continue
		}
		platforms = append(platforms, p)
	}
	sort.Slice(platforms, func(i, j int) bool { return platforms[i].ID < platforms[j].ID })
	return platforms, nil
}

func readPlatform(dir string) (Platform, error) {
	id := filepath.Base(dir)
	p := Platform{
		ID:          id,
		DisplayName: humanize(id),
		Path:        dir,
	}

	manifest, err := readManifest(filepath.Join(dir, "platform.json"))
	if err != nil {
		return p, err
	}
	if manifest != nil {
		if strings.TrimSpace(manifest.DisplayName) != "" {
			p.DisplayName = manifest.DisplayName
		}
		p.EnvKeys = manifest.EnvKeys
	}

	// .env.example takes precedence over platform.json env_keys when present.
	keys, err := readEnvExampleKeys(filepath.Join(dir, ".env.example"))
	if err != nil {
		return p, err
	}
	if keys != nil {
		p.EnvKeys = keys
	}

	fetchers, err := readFetchers(dir, id)
	if err != nil {
		return p, err
	}
	p.Fetchers = fetchers
	return p, nil
}

func readManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	// Drop empty-name keys defensively.
	cleaned := m.EnvKeys[:0]
	for _, k := range m.EnvKeys {
		if strings.TrimSpace(k.Name) != "" {
			cleaned = append(cleaned, k)
		}
	}
	m.EnvKeys = cleaned
	return &m, nil
}

// readEnvExampleKeys returns nil when the file does not exist, distinguishing
// "no file" from "file present but empty" (an empty file returns []EnvKey{}).
func readEnvExampleKeys(path string) ([]EnvKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	keys := []EnvKey{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:eq])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		keys = append(keys, EnvKey{Name: name})
	}
	return keys, nil
}

// fetcherExtensions are the script suffixes treated as runnable fetchers.
// Python is the forward-looking model; .sh is supported during the migration
// period so existing bash fetchers continue to surface in the TUI.
var fetcherExtensions = []string{".py", ".sh"}

// isHelperScript returns true for files that look like Python/shell helpers
// rather than runnable fetchers. Universal conventions only — no platform
// names baked in. Customers can hide other helpers by prefixing with _.
func isHelperScript(name string) bool {
	if name == "__init__.py" {
		return true
	}
	// *_loader.py / *_loader.sh — the shipped fetcher repo's helper convention
	// and a generic Python idiom for env/config loaders.
	for _, ext := range fetcherExtensions {
		if strings.HasSuffix(name, "_loader"+ext) {
			return true
		}
	}
	return false
}

func readFetchers(dir, platformID string) ([]Fetcher, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	out := []Fetcher{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := scriptExt(name)
		if ext == "" {
			continue
		}
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		if isHelperScript(name) {
			continue
		}
		stem := strings.TrimSuffix(name, ext)
		out = append(out, Fetcher{
			ID:         platformID + "/" + stem,
			Name:       humanize(stem),
			Path:       filepath.Join(dir, name),
			PlatformID: platformID,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func scriptExt(name string) string {
	for _, ext := range fetcherExtensions {
		if strings.HasSuffix(name, ext) {
			return ext
		}
	}
	return ""
}

// humanize turns a snake_case identifier into Title Case for display.
func humanize(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}
