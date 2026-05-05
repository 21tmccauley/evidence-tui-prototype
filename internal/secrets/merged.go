package secrets

import "fmt"

// Merged reads from Primary first and then Fallback.
// Writes go to Writer when set, else Primary.
type Merged struct {
	Primary  Store
	Fallback Store
	Writer   Store
}

// Source reports the underlying writer's source so the UI surfaces a concrete
// backend name ("keychain") in save toasts instead of the opaque "merged".
func (m Merged) Source() string {
	if m.Writer != nil {
		return m.Writer.Source()
	}
	if m.Primary != nil {
		return m.Primary.Source()
	}
	if m.Fallback != nil {
		return m.Fallback.Source()
	}
	return "merged"
}

func (m Merged) Writable() bool {
	if m.Writer != nil {
		return m.Writer.Writable()
	}
	if m.Primary != nil {
		return m.Primary.Writable()
	}
	return false
}

func (m Merged) Locate(key string) (string, bool, error) {
	if m.Primary != nil {
		src, found, err := m.Primary.Locate(key)
		if err != nil {
			return "", false, err
		}
		if found {
			return src, true, nil
		}
	}
	if m.Fallback != nil {
		return m.Fallback.Locate(key)
	}
	return "", false, nil
}

func (m Merged) ParamifyUploadAPIToken() (string, error) {
	return paramifyUploadTokenFromStore(m)
}

func (m Merged) Get(key string) (string, bool, error) {
	if m.Primary != nil {
		v, ok, err := m.Primary.Get(key)
		if err != nil {
			return "", false, err
		}
		if ok {
			return v, true, nil
		}
	}
	if m.Fallback != nil {
		return m.Fallback.Get(key)
	}
	return "", false, nil
}

func (m Merged) Set(key, value string) error {
	dst := m.Writer
	if dst == nil {
		dst = m.Primary
	}
	if dst == nil {
		return fmt.Errorf("no writable store configured")
	}
	return dst.Set(key, value)
}

func (m Merged) Delete(key string) error {
	dst := m.Writer
	if dst == nil {
		dst = m.Primary
	}
	if dst == nil {
		return fmt.Errorf("no writable store configured")
	}
	return dst.Delete(key)
}

func (m Merged) List() ([]string, error) {
	all := map[string]bool{}
	add := func(s Store) error {
		if s == nil {
			return nil
		}
		keys, err := s.List()
		if err != nil {
			return err
		}
		for _, k := range keys {
			all[k] = true
		}
		return nil
	}
	if err := add(m.Primary); err != nil {
		return nil, err
	}
	if err := add(m.Fallback); err != nil {
		return nil, err
	}
	return keysForSet(all), nil
}
