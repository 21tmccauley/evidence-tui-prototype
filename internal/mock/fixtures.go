package mock

import "fmt"

// JSONLines returns a synthetic burst of JSON-ish output. n controls volume.
// Used by the streaming behavior.
func JSONLines(n int, prefix string) []string {
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fmt.Sprintf(
			`  {"resource":"%s-%04d","region":"us-east-1","status":"ok","kms":"alias/aws/s3","compliant":true}`,
			prefix, i,
		))
	}
	return out
}

// Banner returns a 3-line headline for the start of a fetcher run.
func Banner(name string) []string {
	return []string{
		fmt.Sprintf("==> %s", name),
		"---- credentials: AWS_PROFILE=paramify-prod (assumed via SSO)",
		"---- region: us-east-1",
	}
}

// Footer returns a 2-line summary line for end of run.
func Footer(records int) []string {
	return []string{
		fmt.Sprintf("---- collected %d records", records),
		"---- writing evidence-output/2026-05-01T19-22-04Z/...",
	}
}
