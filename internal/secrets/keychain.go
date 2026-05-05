package secrets

import (
	"strings"

	"github.com/zalando/go-keyring"
)

const DefaultKeychainService = "paramify-fetcher"

// Keychain stores known secret keys in the OS credential manager.
type Keychain struct {
	Service string
}

func (k Keychain) Source() string { return "keychain" }

func (k Keychain) Writable() bool { return true }

func (k Keychain) Locate(key string) (string, bool, error) {
	_, found, err := k.Get(key)
	if err != nil || !found {
		return "", false, err
	}
	return k.Source(), true, nil
}

func (k Keychain) ParamifyUploadAPIToken() (string, error) {
	return paramifyUploadTokenFromStore(k)
}

func (k Keychain) Get(key string) (string, bool, error) {
	if err := ValidateKey(key); err != nil {
		return "", false, err
	}
	v, err := keyring.Get(k.serviceName(), key)
	if err == keyring.ErrNotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false, nil
	}
	return v, true, nil
}

func (k Keychain) Set(key, value string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		if err := keyring.Delete(k.serviceName(), key); err == keyring.ErrNotFound {
			return nil
		} else {
			return err
		}
	}
	return keyring.Set(k.serviceName(), key, value)
}

func (k Keychain) Delete(key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	if err := keyring.Delete(k.serviceName(), key); err == keyring.ErrNotFound {
		return nil
	} else {
		return err
	}
}

func (k Keychain) List() ([]string, error) {
	keys := map[string]bool{}
	for _, key := range requiredRuntimeKeys() {
		_, found, err := k.Get(key)
		if err != nil {
			return nil, err
		}
		if found {
			keys[key] = true
		}
	}
	return keysForSet(keys), nil
}

func (k Keychain) serviceName() string {
	if strings.TrimSpace(k.Service) != "" {
		return k.Service
	}
	return DefaultKeychainService
}
