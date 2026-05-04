package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// AuthChecker is the AWS pre-flight seam. Production code uses
// CLIAuthChecker which shells out to `aws sts get-caller-identity`; tests
// inject a fake.
type AuthChecker interface {
	CheckAWSAuth(ctx context.Context, profile, region string) error
}

// CLIAuthChecker invokes the local `aws` CLI to verify that the configured
// profile/region resolve to a valid identity. Mirrors
// run_fetchers.py:209-247.
type CLIAuthChecker struct{}

// CheckAWSAuth runs `aws sts get-caller-identity --output json` with the
// supplied profile/region (each omitted from argv when empty). Returns nil
// on success, or an error wrapping the CLI's stderr otherwise.
func (CLIAuthChecker) CheckAWSAuth(ctx context.Context, profile, region string) error {
	args := []string{"sts", "get-caller-identity", "--output", "json"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	if region != "" {
		args = append(args, "--region", region)
	}
	out, err := exec.CommandContext(ctx, "aws", args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("aws sts: %s", msg)
	}
	return nil
}

// awsAuthTimeout caps the pre-flight call so a hung CLI can't stall the
// entire run.
const awsAuthTimeout = 15 * time.Second

// ValidateAWSEvidence fails if evidence metadata.account_id or metadata.arn is "unknown" (run_fetchers.py:250-277). Nil if file missing/unreadable/malformed.
func ValidateAWSEvidence(runDir, scriptKey string) error {
	return ValidateAWSEvidenceForInstance(runDir, scriptKey, Instance{})
}

func ValidateAWSEvidenceForInstance(runDir, scriptKey string, inst Instance) error {
	p, ok := EvidenceFileForInstance(runDir, scriptKey, inst)
	if !ok {
		return nil
	}
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()

	var data struct {
		Metadata struct {
			AccountID string `json:"account_id"`
			ARN       string `json:"arn"`
		} `json:"metadata"`
	}
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		return nil
	}
	if data.Metadata.AccountID == "unknown" || data.Metadata.ARN == "unknown" {
		return errors.New("evidence metadata shows unknown AWS identity; AWS CLI was likely not authenticated when this fetcher ran")
	}
	return nil
}
