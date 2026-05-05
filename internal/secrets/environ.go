package secrets

import "strings"

// BuildEnviron injects selected secret keys from store into base env.
// Existing entries for these keys are replaced.
func BuildEnviron(base []string, store Store, keys []string) ([]string, error) {
	out := make([]string, 0, len(base)+len(keys))
	keySet := map[string]bool{}
	for _, k := range keys {
		if err := ValidateKey(k); err != nil {
			return nil, err
		}
		keySet[k] = true
	}

	for _, entry := range base {
		k, _, ok := strings.Cut(entry, "=")
		if ok && keySet[k] {
			continue
		}
		out = append(out, entry)
	}

	for _, key := range keys {
		if store == nil {
			continue
		}
		v, found, err := store.Get(key)
		if err != nil {
			return nil, err
		}
		if !found || v == "" {
			continue
		}
		out = append(out, key+"="+v)
	}
	return out, nil
}
