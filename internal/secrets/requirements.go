package secrets

import "sort"

var requiredBySource = map[string][]string{
	"knowbe4": {KeyKnowBe4APIKey},
}

// RequiredKeysForSource returns required secret keys for a catalog source.
func RequiredKeysForSource(source string) []string {
	keys := requiredBySource[source]
	out := make([]string, len(keys))
	copy(out, keys)
	return out
}

// MissingRequiredKeys returns the subset of keys that are unset in store.
func MissingRequiredKeys(store Store, keys []string) ([]string, error) {
	if store == nil || len(keys) == 0 {
		return nil, nil
	}
	missing := []string{}
	for _, key := range dedupeStrings(keys) {
		_, found, err := store.Get(key)
		if err != nil {
			return nil, err
		}
		if !found {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing, nil
}

func dedupeStrings(in []string) []string {
	set := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if set[s] {
			continue
		}
		set[s] = true
		out = append(out, s)
	}
	return out
}
