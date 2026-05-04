package runner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// SummaryWriter writes summary.json matching the Python schema.
type SummaryWriter struct {
	Now func() time.Time
}

type SummaryResult struct {
	// CheckName is the Python-shaped "script_name" / "instance_name" (e.g.
	// "s3_encryption_status" or "checkov_terraform_project_2").
	CheckName string
	ScriptKey string
	Instance  Instance
	Success   bool

	// ErrorReason mirrors FinishedMsg.ErrorReason.
	ErrorReason string
}

type summaryJSON struct {
	Timestamp         string        `json:"timestamp"`
	EvidenceDirectory string        `json:"evidence_directory"`
	TotalScripts      int           `json:"total_scripts"`
	SuccessfulScripts int           `json:"successful_scripts"`
	FailedScripts     int           `json:"failed_scripts"`
	Results           []resultEntry `json:"results"`
}

type resultEntry struct {
	Check        string `json:"check"`
	Resource     string `json:"resource"`
	Status       string `json:"status"`
	EvidenceFile any    `json:"evidence_file"`
	ErrorReason  string `json:"error_reason,omitempty"`
}

// WriteSummary writes <evidenceDir>/summary.json with 2-space indentation,
// matching run_fetchers.py:create_summary_file.
func (w SummaryWriter) WriteSummary(evidenceDir string, results []SummaryResult) error {
	now := time.Now
	if w.Now != nil {
		now = w.Now
	}
	ts := now().Format("2006-01-02T15:04:05.999999") + "Z"

	okCount := 0
	out := make([]resultEntry, 0, len(results))
	for _, r := range results {
		status := "FAIL"
		if r.Success {
			status = "PASS"
			okCount++
		}

		runDir := OutputDirForInstance(evidenceDir, r.ScriptKey, r.Instance)
		ev, ok := EvidenceFileForInstance(runDir, r.ScriptKey, r.Instance)
		var evidenceVal any
		if ok {
			evidenceVal = ev
		} else {
			evidenceVal = nil
			ev = ""
		}

		resource := r.Instance.Resource
		if resource == "" {
			resource = "unknown"
		}
		if resource == "unknown" && ev != "" {
			if meta := resourceFromMetadata(ev); meta != "" {
				resource = meta
			}
		}

		entry := resultEntry{
			Check:        r.CheckName,
			Resource:     resource,
			Status:       status,
			EvidenceFile: evidenceVal,
		}
		if r.ErrorReason != "" {
			entry.ErrorReason = r.ErrorReason
		}
		out = append(out, entry)
	}

	payload := summaryJSON{
		Timestamp:         ts,
		EvidenceDirectory: evidenceDir,
		TotalScripts:      len(results),
		SuccessfulScripts: okCount,
		FailedScripts:     len(results) - okCount,
		Results:           out,
	}

	if err := os.MkdirAll(evidenceDir, 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(evidenceDir, "summary.json"))
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func resourceFromMetadata(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var data struct {
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(b, &data); err != nil {
		return ""
	}
	meta := data.Metadata
	if meta == nil {
		return ""
	}
	for _, key := range []string{"region", "account_id", "profile"} {
		if v, ok := meta[key]; ok {
			if s, ok := v.(string); ok && s != "" && s != "unknown" {
				return s
			}
		}
	}
	return ""
}
