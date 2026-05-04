package runner

import (
	"testing"
	"time"
)

func TestResolveTimeout_Default(t *testing.T) {
	t.Setenv("FETCHER_TIMEOUT", "")
	if got, want := ResolveTimeout("iam_policies"), 300*time.Second; got != want {
		t.Errorf("default timeout: got %v, want %v", got, want)
	}
}

func TestResolveTimeout_GlobalOverride(t *testing.T) {
	t.Setenv("FETCHER_TIMEOUT", "120")
	if got, want := ResolveTimeout("iam_policies"), 120*time.Second; got != want {
		t.Errorf("FETCHER_TIMEOUT not honored: got %v, want %v", got, want)
	}
}

func TestResolveTimeout_PerScriptOverride(t *testing.T) {
	t.Setenv("FETCHER_TIMEOUT", "300")
	t.Setenv("IAM_POLICIES_TIMEOUT", "90")
	if got, want := ResolveTimeout("iam_policies"), 90*time.Second; got != want {
		t.Errorf("IAM_POLICIES_TIMEOUT not honored: got %v, want %v", got, want)
	}
}

func TestResolveTimeout_SSLLabsFloor(t *testing.T) {
	t.Setenv("FETCHER_TIMEOUT", "300")
	if got, want := ResolveTimeout("ssllabs_tls_scan"), 3600*time.Second; got != want {
		t.Errorf("ssllabs floor not applied: got %v, want %v", got, want)
	}
}

// A per-script override below the ssllabs floor must still be raised to
// the floor; the floor is the safety net.
func TestResolveTimeout_SSLLabsFloorBeatsLowOverride(t *testing.T) {
	t.Setenv("SSLLABS_TLS_SCAN_TIMEOUT", "30")
	if got, want := ResolveTimeout("ssllabs_tls_scan"), 3600*time.Second; got != want {
		t.Errorf("low override should be floored: got %v, want %v", got, want)
	}
}

// SSLLABS_FETCHER_TIMEOUT can raise the floor further.
func TestResolveTimeout_SSLLabsFloorRaisable(t *testing.T) {
	t.Setenv("FETCHER_TIMEOUT", "300")
	t.Setenv("SSLLABS_FETCHER_TIMEOUT", "4500")
	if got, want := ResolveTimeout("ssllabs_tls_scan"), 4500*time.Second; got != want {
		t.Errorf("SSLLABS_FETCHER_TIMEOUT raise: got %v, want %v", got, want)
	}
}

// A per-script override that already exceeds the floor wins.
func TestResolveTimeout_SSLLabsHighOverrideWins(t *testing.T) {
	t.Setenv("SSLLABS_TLS_SCAN_TIMEOUT", "7200")
	if got, want := ResolveTimeout("ssllabs_tls_scan"), 7200*time.Second; got != want {
		t.Errorf("high override should win: got %v, want %v", got, want)
	}
}

func TestEnvInt_HandlesGarbage(t *testing.T) {
	t.Setenv("X", "")
	if got := envInt("X", 42); got != 42 {
		t.Errorf("empty: got %d, want 42", got)
	}
	t.Setenv("X", "not-a-number")
	if got := envInt("X", 42); got != 42 {
		t.Errorf("garbage: got %d, want 42", got)
	}
	t.Setenv("X", "-5")
	if got := envInt("X", 42); got != 42 {
		t.Errorf("negative should fall back: got %d, want 42", got)
	}
}
