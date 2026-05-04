// Package uploader implements the Paramify upload contract (paramify_pusher.py); see .cursor/rules/30-uploader-contract.mdc.
package uploader

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/paramify/evidence-tui-prototype/internal/evidence"
)

// DefaultBaseURL matches paramify_pusher.py when PARAMIFY_API_BASE_URL is unset.
const DefaultBaseURL = "https://app.paramify.com/api/v0"

// ScriptArtifactNotePrefix identifies fetcher script artifacts for dedup.
// Do not change without updating the contract rule and any customer data.
const ScriptArtifactNotePrefix = "Automated evidence collection script:"

var instanceSuffixRE = regexp.MustCompile(`_(project|region)_\d+$`)

// Client performs Paramify HTTP operations. Token values must never be logged.
type Client struct {
	baseURL         string
	token           string
	fetcherRepoRoot string
	http            *http.Client
	now             func() time.Time
}

// Config wires a Paramify API client.
type Config struct {
	BaseURL         string
	Token           string
	FetcherRepoRoot string
	HTTP            *http.Client
}

// New returns a Client or an error if required fields are missing.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("paramify uploader: empty API token")
	}
	base := strings.TrimSpace(cfg.BaseURL)
	if base == "" {
		base = DefaultBaseURL
	}
	base = strings.TrimRight(base, "/")
	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	return &Client{
		baseURL:         base,
		token:           cfg.Token,
		fetcherRepoRoot: cfg.FetcherRepoRoot,
		http:            hc,
		now:             time.Now,
	}, nil
}

func (c *Client) nowFunc() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// Summary aggregates one ProcessEvidenceDir run.
type Summary struct {
	Results       []RowResult `json:"results"`
	Skipped       int         `json:"skipped"`
	Successful    int         `json:"successful"`
	FailedUploads int         `json:"failed_uploads"`
}

// RowResult mirrors paramify_pusher.py's per-row upload_result dict.
type RowResult struct {
	Check         string `json:"check"`
	Resource      string `json:"resource"`
	Status        string `json:"status"`
	EvidenceFile  string `json:"evidence_file"`
	EvidenceSetID string `json:"evidence_set_id"`
	UploadSuccess bool   `json:"upload_success"`
	Timestamp     string `json:"timestamp"`
}

type uploadLogEnvelope struct {
	UploadTimestamp string      `json:"upload_timestamp"`
	Results         []RowResult `json:"results"`
	Error           string      `json:"error,omitempty"`
}

type evidencesResponse struct {
	Evidences []struct {
		ID            string `json:"id"`
		ReferenceID   string `json:"referenceId"`
		ReferenceId   string `json:"reference_id"`
		Name          string `json:"name"`
		Description   string `json:"description"`
		Instructions  any    `json:"instructions"`
		Automated     bool   `json:"automated"`
		ValidationRaw any    `json:"validationRules"`
	} `json:"evidences"`
}

type apiErrorBody struct {
	Message string `json:"message"`
	Error   string `json:"error"`
}

// ProcessEvidenceDir reads summary.json and evidence_sets.json under dir,
// uploads evidence (and script artifacts with dedup), and always writes
// upload_log.json into dir (even on partial failure).
func (c *Client) ProcessEvidenceDir(ctx context.Context, dir string) (out Summary, err error) {
	logPath := filepath.Join(dir, "upload_log.json")
	defer func() {
		env := uploadLogEnvelope{
			UploadTimestamp: c.nowFunc().Format(time.RFC3339Nano),
			Results:         out.Results,
		}
		if err != nil {
			env.Error = err.Error()
		}
		_ = writeUploadLogFile(logPath, env)
	}()

	summaryPath := filepath.Join(dir, "summary.json")
	summaryBytes, e0 := os.ReadFile(summaryPath)
	if e0 != nil {
		err = fmt.Errorf("read summary: %w", e0)
		return out, err
	}
	var sum struct {
		Results []struct {
			Check        string `json:"check"`
			Script       string `json:"script"`
			Resource     string `json:"resource"`
			Status       string `json:"status"`
			EvidenceFile any    `json:"evidence_file"`
		} `json:"results"`
	}
	if e0 = json.Unmarshal(summaryBytes, &sum); e0 != nil {
		err = fmt.Errorf("parse summary: %w", e0)
		return out, err
	}

	setsPath := filepath.Join(dir, "evidence_sets.json")
	setsBytes, e0 := os.ReadFile(setsPath)
	if e0 != nil {
		err = fmt.Errorf("read evidence_sets.json: %w", e0)
		return out, err
	}
	var doc evidence.Document
	if e0 = json.Unmarshal(setsBytes, &doc); e0 != nil {
		err = fmt.Errorf("parse evidence_sets.json: %w", e0)
		return out, err
	}

	for _, row := range sum.Results {
		checkName := row.Check
		if checkName == "" {
			checkName = row.Script
		}
		resource := row.Resource
		if resource == "" {
			resource = "unknown"
		}
		evPath, ok := evidenceFilePath(row.EvidenceFile)
		if !ok {
			out.Skipped++
			continue
		}
		set, found := evidenceSetForCheck(checkName, doc)
		if !found {
			out.Skipped++
			continue
		}
		if _, statErr := os.Stat(evPath); statErr != nil {
			out.Skipped++
			continue
		}
		evidenceID, errGC := c.getOrCreateEvidenceSet(ctx, set)
		if errGC != nil || evidenceID == "" {
			out.Skipped++
			continue
		}
		title := buildArtifactTitle(checkName, resource, set.Name)
		note := fmt.Sprintf("Evidence file for %s: %s", checkName, filepath.Base(evPath))
		if resource != "unknown" {
			note = fmt.Sprintf("Evidence for %s: %s", resource, filepath.Base(evPath))
		}
		upOK, upErr := c.uploadEvidenceMultipart(ctx, evidenceID, evPath, title, note)
		if upErr != nil {
			upOK = false
		}
		tr := baseRowResult(checkName, resource, row.Status, evPath, evidenceID, upOK, c.nowFunc())
		out.Results = append(out.Results, tr)
		if upOK && c.fetcherRepoRoot != "" && strings.TrimSpace(set.ScriptFile) != "" {
			scriptFull := filepath.Join(c.fetcherRepoRoot, filepath.FromSlash(set.ScriptFile))
			if st, stErr := os.Stat(scriptFull); stErr == nil && !st.IsDir() {
				exists, exErr := c.scriptArtifactExists(ctx, evidenceID, filepath.Base(scriptFull))
				if exErr == nil && !exists {
					_, _ = c.uploadScriptMultipart(ctx, evidenceID, scriptFull)
				}
			}
		}
		if upOK {
			out.Successful++
		} else {
			out.FailedUploads++
		}
	}
	return out, nil
}

func baseRowResult(check, resource, status, evFile, setID string, uploadOK bool, now time.Time) RowResult {
	return RowResult{
		Check:         check,
		Resource:      resource,
		Status:        status,
		EvidenceFile:  evFile,
		EvidenceSetID: setID,
		UploadSuccess: uploadOK,
		Timestamp:     now.Format(time.RFC3339Nano),
	}
}

func evidenceFilePath(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	return s, true
}

func evidenceSetForCheck(checkName string, doc evidence.Document) (evidence.Set, bool) {
	m := doc.EvidenceSets
	if s, ok := m[checkName]; ok {
		return s, true
	}
	base := instanceSuffixRE.ReplaceAllString(checkName, "")
	if base != checkName {
		if s, ok := m[base]; ok {
			return s, true
		}
	}
	for k, s := range m {
		if strings.HasPrefix(k, base) || strings.HasPrefix(base, k) {
			return s, true
		}
	}
	return evidence.Set{}, false
}

func buildArtifactTitle(checkName, resource, displayName string) string {
	name := displayName
	if strings.TrimSpace(name) == "" {
		name = checkName
	}
	if resource != "" && resource != "unknown" {
		return fmt.Sprintf("%s - %s", name, resource)
	}
	return name
}

func writeUploadLogFile(path string, env uploadLogEnvelope) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

func (c *Client) getOrCreateEvidenceSet(ctx context.Context, set evidence.Set) (string, error) {
	ref := strings.TrimSpace(set.ID)
	if ref == "" {
		return "", errors.New("evidence set has empty id")
	}
	if id, ok, err := c.findEvidenceSetID(ctx, ref); err != nil {
		return "", err
	} else if ok {
		return id, nil
	}
	return c.createEvidenceSet(ctx, set)
}

func (c *Client) findEvidenceSetID(ctx context.Context, referenceID string) (string, bool, error) {
	try := func(rawURL string) (string, bool, error) {
		resp, err := c.roundTrip(ctx, func() (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
			if err != nil {
				return nil, err
			}
			req.Header.Set("Authorization", "Bearer "+c.token)
			req.Header.Set("Accept", "application/json")
			return req, nil
		})
		if err != nil {
			return "", false, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", false, nil
		}
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return "", false, err
		}
		return parseEvidenceIDFromList(body, referenceID)
	}
	filtered := c.baseURL + "/evidence?referenceId=" + url.QueryEscape(referenceID)
	if id, ok, err := try(filtered); err != nil || ok {
		return id, ok, err
	}
	return try(c.baseURL + "/evidence")
}

func parseEvidenceIDFromList(body []byte, wantRef string) (string, bool, error) {
	var wrap evidencesResponse
	if err := json.Unmarshal(body, &wrap); err != nil {
		return "", false, err
	}
	for _, ev := range wrap.Evidences {
		ref := ev.ReferenceID
		if ref == "" {
			ref = ev.ReferenceId
		}
		if ref == wantRef && ev.ID != "" {
			return ev.ID, true, nil
		}
	}
	return "", false, nil
}

func (c *Client) createEvidenceSet(ctx context.Context, set evidence.Set) (string, error) {
	instructions := evidence.RichTextToParamifyAPIString(set.Instructions)
	payload, err := json.Marshal(map[string]any{
		"referenceId":  set.ID,
		"name":         set.Name,
		"description":  set.Description,
		"instructions": instructions,
		"automated":    true,
	})
	if err != nil {
		return "", err
	}
	u := c.baseURL + "/evidence"
	resp, err := c.roundTrip(ctx, func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(payload)), nil
		}
		return req, nil
	})
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		var created struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &created); err != nil {
			return "", err
		}
		return created.ID, nil
	case http.StatusBadRequest:
		var eb apiErrorBody
		_ = json.Unmarshal(body, &eb)
		msg := eb.Message
		if msg == "" {
			msg = eb.Error
		}
		if strings.Contains(msg, "Reference ID already exists") {
			if id, ok, err := c.findEvidenceSetID(ctx, set.ID); err != nil {
				return "", err
			} else if ok {
				return id, nil
			}
		}
		return "", fmt.Errorf("create evidence set: HTTP %d", resp.StatusCode)
	default:
		return "", fmt.Errorf("create evidence set: HTTP %d", resp.StatusCode)
	}
}

func (c *Client) scriptArtifactExists(ctx context.Context, evidenceID, filename string) (bool, error) {
	resp, err := c.roundTrip(ctx, func() (*http.Request, error) {
		u := fmt.Sprintf("%s/evidence/%s/artifacts", c.baseURL, url.PathEscape(evidenceID))
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		q := req.URL.Query()
		q.Add("originalFileName", filename)
		req.URL.RawQuery = q.Encode()
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Accept", "application/json")
		return req, nil
	})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}
	var artifacts []struct {
		OriginalFileName string `json:"originalFileName"`
		Title            string `json:"title"`
		Note             string `json:"note"`
	}
	if err := json.Unmarshal(body, &artifacts); err != nil {
		var wrap struct {
			Artifacts []struct {
				OriginalFileName string `json:"originalFileName"`
				Title            string `json:"title"`
				Note             string `json:"note"`
			} `json:"artifacts"`
		}
		if err := json.Unmarshal(body, &wrap); err != nil {
			return false, nil
		}
		artifacts = wrap.Artifacts
	}
	base := filepath.Base(filename)
	for _, a := range artifacts {
		note := a.Note
		if !strings.Contains(note, ScriptArtifactNotePrefix) {
			continue
		}
		if a.OriginalFileName == base || a.Title == base {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) uploadEvidenceMultipart(ctx context.Context, evidenceID, filePath, title, note string) (bool, error) {
	return c.uploadMultipartArtifact(ctx, evidenceID, filePath, title, note)
}

func (c *Client) uploadScriptMultipart(ctx context.Context, evidenceID, scriptPath string) (bool, error) {
	name := filepath.Base(scriptPath)
	note := ScriptArtifactNotePrefix + " " + name
	return c.uploadMultipartArtifact(ctx, evidenceID, scriptPath, name, note)
}

func (c *Client) uploadMultipartArtifact(ctx context.Context, evidenceID, filePath, title, note string) (bool, error) {
	fileBody, err := os.ReadFile(filePath)
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(map[string]string{
		"title":         title,
		"note":          note,
		"effectiveDate": c.nowFunc().UTC().Format(time.RFC3339Nano),
	})
	if err != nil {
		return false, err
	}
	u := fmt.Sprintf("%s/evidence/%s/artifacts/upload", c.baseURL, url.PathEscape(evidenceID))
	return c.postMultipart(ctx, u, fileBody, filepath.Base(filePath), payload)
}

func (c *Client) postMultipart(ctx context.Context, rawURL string, fileBody []byte, fileName string, artifactJSON []byte) (bool, error) {
	buildBody := func() (contentType string, body []byte, err error) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, err := mw.CreateFormFile("file", fileName)
		if err != nil {
			return "", nil, err
		}
		if _, err := fw.Write(fileBody); err != nil {
			return "", nil, err
		}
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="artifact"; filename="artifact.json"`)
		h.Set("Content-Type", "application/json")
		part, err := mw.CreatePart(h)
		if err != nil {
			return "", nil, err
		}
		if _, err := part.Write(artifactJSON); err != nil {
			return "", nil, err
		}
		if err := mw.Close(); err != nil {
			return "", nil, err
		}
		return mw.FormDataContentType(), buf.Bytes(), nil
	}
	resp, err := c.roundTrip(ctx, func() (*http.Request, error) {
		ct, body, err := buildBody()
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
		req.Header.Set("Content-Type", ct)
		return req, nil
	})
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated, nil
}

func (c *Client) roundTrip(ctx context.Context, newReq func() (*http.Request, error)) (*http.Response, error) {
	backoffs := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	attempt := 0
	for {
		req, err := newReq()
		if err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			if attempt >= len(backoffs) {
				return nil, err
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoffs[attempt]):
			}
			attempt++
			continue
		}
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if attempt >= len(backoffs) {
				return resp, nil
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoffs[attempt]):
			}
			attempt++
			continue
		}
		return resp, nil
	}
}
