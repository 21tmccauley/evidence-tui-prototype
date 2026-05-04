package preflight

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

type fakeChecker struct {
	calls int
	err   error
}

func (f *fakeChecker) CheckAWSAuth(context.Context, string, string) error {
	f.calls++
	return f.err
}

func TestServiceCheckAWSCachesSuccess(t *testing.T) {
	checker := &fakeChecker{}
	service := Service{
		Checker: checker,
		Cache:   filepath.Join(t.TempDir(), "pre-flight-cache.json"),
	}

	first := service.CheckAWS(context.Background(), "prod", "us-east-1")
	if !first.OK || first.FromCache {
		t.Fatalf("first check: %#v", first)
	}
	second := service.CheckAWS(context.Background(), "prod", "us-east-1")
	if !second.OK || !second.FromCache {
		t.Fatalf("second check should use cache: %#v", second)
	}
	if checker.calls != 1 {
		t.Fatalf("checker calls: got %d want 1", checker.calls)
	}
}

func TestServiceCheckAWSDetectsSSOError(t *testing.T) {
	service := Service{
		Checker: &fakeChecker{err: errors.New("aws sts: The SSO session has expired")},
	}
	result := service.CheckAWS(context.Background(), "prod", "us-east-1")
	if result.OK {
		t.Fatal("check should fail")
	}
	if !result.SSOError {
		t.Fatalf("expected SSOError, got %#v", result)
	}
}
