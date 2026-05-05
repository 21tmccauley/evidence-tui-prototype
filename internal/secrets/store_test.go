package secrets

import (
	"strings"
	"testing"
)

func TestBuildEnvironInjectsSecrets(t *testing.T) {
	mem := NewMemory()
	if err := mem.Set(KeyKnowBe4APIKey, "kb4-test"); err != nil {
		t.Fatalf("set knowbe4: %v", err)
	}
	if err := mem.Set(KeyParamifyUploadAPIToken, "paramify-token"); err != nil {
		t.Fatalf("set paramify token: %v", err)
	}

	base := []string{
		"PATH=/usr/bin",
		KeyKnowBe4APIKey + "=old",
		"HOME=/tmp/test",
	}
	env, err := BuildEnviron(base, mem, RuntimeKeys())
	if err != nil {
		t.Fatalf("BuildEnviron error: %v", err)
	}

	if !containsEnv(env, KeyKnowBe4APIKey, "kb4-test") {
		t.Fatalf("expected knowbe4 key in env, got %v", env)
	}
	if !containsEnv(env, KeyParamifyUploadAPIToken, "paramify-token") {
		t.Fatalf("expected paramify token in env, got %v", env)
	}
	if containsEnv(env, KeyKnowBe4APIKey, "old") {
		t.Fatalf("old knowbe4 value should be replaced, got %v", env)
	}
}

func TestMergedReadPrimaryFallbackWriteTarget(t *testing.T) {
	primary := NewMemory()
	fallback := NewMemory()
	if err := fallback.Set(KeyKnowBe4APIKey, "from-fallback"); err != nil {
		t.Fatalf("set fallback value: %v", err)
	}

	store := Merged{
		Primary:  primary,
		Fallback: fallback,
		Writer:   primary,
	}

	if got, found, err := store.Get(KeyKnowBe4APIKey); err != nil || !found || got != "from-fallback" {
		t.Fatalf("expected fallback read, got found=%t value=%q err=%v", found, got, err)
	}

	if err := store.Set(KeyKnowBe4APIKey, "from-primary"); err != nil {
		t.Fatalf("set merged value: %v", err)
	}

	if got, found, err := store.Get(KeyKnowBe4APIKey); err != nil || !found || got != "from-primary" {
		t.Fatalf("expected primary override, got found=%t value=%q err=%v", found, got, err)
	}
}

func TestWritableReportsBackendCapability(t *testing.T) {
	cases := []struct {
		name string
		s    Store
		want bool
	}{
		{"memory", NewMemory(), true},
		{"env", Env{Environ: []string{}}, false},
		{"merged-with-writable-writer", Merged{Primary: Env{}, Fallback: Env{}, Writer: NewMemory()}, true},
		{"merged-no-writer-readonly-primary", Merged{Primary: Env{}, Fallback: NewMemory()}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.s.Writable(); got != tc.want {
				t.Fatalf("Writable() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestLocateReportsProvenance(t *testing.T) {
	primary := NewMemory()
	fallback := Env{Environ: []string{KeyKnowBe4APIKey + "=from-env"}}
	store := Merged{Primary: primary, Fallback: fallback, Writer: primary}

	src, found, err := store.Locate(KeyKnowBe4APIKey)
	if err != nil || !found || src != "env" {
		t.Fatalf("expected fallback provenance 'env', got src=%q found=%t err=%v", src, found, err)
	}

	if err := primary.Set(KeyKnowBe4APIKey, "from-mem"); err != nil {
		t.Fatalf("set primary: %v", err)
	}
	src, found, err = store.Locate(KeyKnowBe4APIKey)
	if err != nil || !found || src != "memory" {
		t.Fatalf("expected primary provenance 'memory' after override, got src=%q found=%t err=%v", src, found, err)
	}

	src, found, err = store.Locate(KeyParamifyUploadAPIToken)
	if err != nil || found || src != "" {
		t.Fatalf("expected unset key to return found=false, got src=%q found=%t err=%v", src, found, err)
	}
}

func TestMergedSourceReportsWriter(t *testing.T) {
	store := Merged{Primary: Env{}, Fallback: Env{}, Writer: NewMemory()}
	if got := store.Source(); got != "memory" {
		t.Fatalf("Merged.Source() = %q, want %q", got, "memory")
	}
}

func containsEnv(env []string, key, want string) bool {
	prefix := key + "="
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			continue
		}
		return strings.TrimPrefix(entry, prefix) == want
	}
	return false
}
