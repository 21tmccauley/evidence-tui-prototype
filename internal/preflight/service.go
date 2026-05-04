package preflight

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/paramify/evidence-tui-prototype/internal/runner"
)

const defaultCacheTTL = 5 * time.Minute

// Result is the outcome of one credential pre-flight check.
type Result struct {
	OK        bool
	FromCache bool
	SSOError  bool
	Err       error
}

// LoginRunner runs provider-specific login repair flows.
type LoginRunner interface {
	LoginAWS(ctx context.Context, profile string) error
}

// AWSLoginRunner shells out to `aws sso login --profile <profile>`.
type AWSLoginRunner struct{}

func (AWSLoginRunner) LoginAWS(ctx context.Context, profile string) error {
	args := []string{"sso", "login"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	out, err := exec.CommandContext(ctx, "aws", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("aws sso login: %s", msg)
	}
	return nil
}

// Service owns cached AWS credential pre-flight checks.
type Service struct {
	Checker runner.AuthChecker
	Login   LoginRunner
	Cache   string
	TTL     time.Duration
}

// CachedAuthChecker adapts Service back to runner.AuthChecker so the real
// runner can reuse the same 5-minute pre-flight cache the welcome screen wrote.
type CachedAuthChecker struct {
	Service Service
}

func (c CachedAuthChecker) CheckAWSAuth(ctx context.Context, profile, region string) error {
	result := c.Service.CheckAWS(ctx, profile, region)
	if result.OK {
		return nil
	}
	if result.Err != nil {
		return result.Err
	}
	return fmt.Errorf("aws credential check failed")
}

func (s Service) CheckAWS(ctx context.Context, profile, region string) Result {
	ttl := s.TTL
	if ttl <= 0 {
		ttl = defaultCacheTTL
	}
	if s.Cache != "" {
		if freshCacheEntry(s.Cache, profile, region, time.Now(), ttl) {
			return Result{OK: true, FromCache: true}
		}
	}

	checker := s.Checker
	if checker == nil {
		checker = runner.CLIAuthChecker{}
	}
	err := checker.CheckAWSAuth(ctx, profile, region)
	if err != nil {
		return Result{Err: err, SSOError: IsSSOError(err)}
	}
	if s.Cache != "" {
		_ = writeCacheEntry(s.Cache, profile, region, time.Now())
	}
	return Result{OK: true}
}

func (s Service) LoginAWS(ctx context.Context, profile string) error {
	login := s.Login
	if login == nil {
		login = AWSLoginRunner{}
	}
	return login.LoginAWS(ctx, profile)
}

func IsSSOError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "sso") ||
		strings.Contains(msg, "token has expired") ||
		strings.Contains(msg, "expiredtoken") ||
		strings.Contains(msg, "could not load credentials")
}

type cacheFile struct {
	Entries []cacheEntry `json:"entries"`
}

type cacheEntry struct {
	Profile   string    `json:"profile"`
	Region    string    `json:"region"`
	CheckedAt time.Time `json:"checked_at"`
}

func freshCacheEntry(path, profile, region string, now time.Time, ttl time.Duration) bool {
	c, err := readCache(path)
	if err != nil {
		return false
	}
	for _, entry := range c.Entries {
		if entry.Profile == profile && entry.Region == region && now.Sub(entry.CheckedAt) < ttl {
			return true
		}
	}
	return false
}

func writeCacheEntry(path, profile, region string, checkedAt time.Time) error {
	c, _ := readCache(path)
	var updated bool
	for i := range c.Entries {
		if c.Entries[i].Profile == profile && c.Entries[i].Region == region {
			c.Entries[i].CheckedAt = checkedAt
			updated = true
			break
		}
	}
	if !updated {
		c.Entries = append(c.Entries, cacheEntry{
			Profile:   profile,
			Region:    region,
			CheckedAt: checkedAt,
		})
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func readCache(path string) (cacheFile, error) {
	var c cacheFile
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return cacheFile{}, err
	}
	return c, nil
}
