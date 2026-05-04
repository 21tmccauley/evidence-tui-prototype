package runner

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/paramify/evidence-tui-prototype/internal/catalog"
)

var resourceUnsafe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// Instance is one run target (standard: ID == BaseID; multi-instance: env/resource overrides).
type Instance struct {
	ID       FetcherID
	BaseID   FetcherID
	Name     string
	Provider string
	Env      map[string]string
	Resource string
}

type multiConfig struct {
	GitLabProjects []instanceConfig
	AWSRegions     []instanceConfig
}

type instanceConfig struct {
	Name     string
	Provider string
	Fetchers []string
	Env      map[string]string
	Resource string
}

// ParseMultiInstanceConfig reads GITLAB_PROJECT_<N>_* and AWS_REGION_<N>_* from env (run_fetchers.py parity).
func ParseMultiInstanceConfig(env []string) multiConfig {
	envMap := environMap(env)
	gitlabGroups := parseIndexedEnv(env, regexp.MustCompile(`^GITLAB_PROJECT_(\d+)_(.+)$`))
	awsGroups := parseIndexedEnv(env, regexp.MustCompile(`^AWS_REGION_(\d+)_(.+)$`))

	cfg := multiConfig{}
	for _, n := range sortedKeys(gitlabGroups) {
		group := gitlabGroups[n]
		if group["url"] == "" || group["api_access_token"] == "" || group["id"] == "" || group["fetchers"] == "" {
			continue
		}
		inst := instanceConfig{
			Name:     "project_" + n,
			Provider: "gitlab",
			Fetchers: splitFetchers(group["fetchers"]),
			Resource: group["id"],
			Env: map[string]string{
				"GITLAB_URL":        group["url"],
				"GITLAB_API_TOKEN":  group["api_access_token"],
				"GITLAB_PROJECT_ID": group["id"],
			},
		}
		for k, v := range group {
			if k == "url" || k == "api_access_token" || k == "id" || k == "fetchers" {
				continue
			}
			inst.Env["GITLAB_"+strings.ToUpper(k)] = v
		}
		cfg.GitLabProjects = append(cfg.GitLabProjects, inst)
	}

	for _, n := range sortedKeys(awsGroups) {
		group := awsGroups[n]
		if group["fetchers"] == "" {
			continue
		}
		region := firstNonEmpty(group["region"], n)
		profile := firstNonEmpty(group["profile"], envMap["AWS_PROFILE"])
		inst := instanceConfig{
			Name:     "region_" + n,
			Provider: "aws",
			Fetchers: splitFetchers(group["fetchers"]),
			Resource: region,
			Env: map[string]string{
				"AWS_DEFAULT_REGION": region,
				"AWS_REGION":         region,
			},
		}
		if profile != "" {
			inst.Env["AWS_PROFILE"] = profile
		}
		for k, v := range group {
			if k == "region" || k == "profile" || k == "fetchers" {
				continue
			}
			inst.Env["AWS_"+strings.ToUpper(k)] = v
		}
		cfg.AWSRegions = append(cfg.AWSRegions, inst)
	}

	return cfg
}

// CreateFetcherInstances expands selected ids using multi-instance env config when matched.
func CreateFetcherInstances(selected []FetcherID, scripts map[FetcherID]catalog.Script, cfg multiConfig) []Instance {
	allConfigs := append([]instanceConfig{}, cfg.GitLabProjects...)
	allConfigs = append(allConfigs, cfg.AWSRegions...)

	instances := []Instance{}
	for _, id := range selected {
		s, ok := scripts[id]
		if !ok {
			continue
		}
		var covered bool
		for _, c := range allConfigs {
			if !fetcherMatches(c.Fetchers, id, s) {
				continue
			}
			covered = true
			instances = append(instances, Instance{
				ID:       FetcherID(string(id) + "_" + c.Name),
				BaseID:   id,
				Name:     c.Name,
				Provider: c.Provider,
				Env:      cloneMap(c.Env),
				Resource: c.Resource,
			})
		}
		if !covered {
			instances = append(instances, Instance{ID: id, BaseID: id})
		}
	}
	return instances
}

func InstancesFromEnv(selected []FetcherID, scripts map[FetcherID]catalog.Script, env []string) []Instance {
	return CreateFetcherInstances(selected, scripts, ParseMultiInstanceConfig(env))
}

func (inst Instance) Target() Target {
	return Target{
		ID:     inst.ID,
		BaseID: inst.BaseID,
		Label:  inst.Name,
	}
}

func EffectiveProfileRegion(cfg Config, inst Instance) (string, string) {
	profile := cfg.Profile
	region := cfg.Region
	if inst.Env != nil {
		if v := inst.Env["AWS_PROFILE"]; v != "" {
			profile = v
		}
		if v := inst.Env["AWS_REGION"]; v != "" {
			region = v
		} else if v := inst.Env["AWS_DEFAULT_REGION"]; v != "" {
			region = v
		}
	}
	return profile, region
}

func SanitizeResourceID(id string) string {
	id = strings.ReplaceAll(id, "/", "_")
	return resourceUnsafe.ReplaceAllString(id, "_")
}

// EvidenceFileForInstance picks the evidence JSON under runDir for this target.
func EvidenceFileForInstance(runDir, scriptKey string, inst Instance) (string, bool) {
	files, err := filepath.Glob(filepath.Join(runDir, "*.json"))
	if err != nil {
		return "", false
	}
	jsonFiles := map[string]string{}
	for _, p := range files {
		stem := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
		jsonFiles[stem] = p
	}

	scriptName := scriptKey
	if inst.Name != "" {
		scriptName = scriptKey + "_" + inst.Name
	}
	if p, ok := jsonFiles[scriptName]; ok {
		return p, true
	}

	baseName := stripInstanceSuffix(scriptName)
	if inst.Name != "" && inst.Resource != "" {
		expectedStem := baseName + "_" + SanitizeResourceID(inst.Resource)
		p, ok := jsonFiles[expectedStem]
		return p, ok
	}

	p, ok := jsonFiles[baseName]
	return p, ok
}

func parseIndexedEnv(env []string, pattern *regexp.Regexp) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		match := pattern.FindStringSubmatch(key)
		if match == nil {
			continue
		}
		n := match[1]
		configKey := strings.ToLower(match[2])
		if out[n] == nil {
			out[n] = map[string]string{}
		}
		out[n][configKey] = value
	}
	return out
}

func environMap(env []string) map[string]string {
	if env == nil {
		env = os.Environ()
	}
	out := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			out[key] = value
		}
	}
	return out
}

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		ni, ei := strconv.Atoi(keys[i])
		nj, ej := strconv.Atoi(keys[j])
		if ei == nil && ej == nil {
			return ni < nj
		}
		return keys[i] < keys[j]
	})
	return keys
}

func splitFetchers(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func fetcherMatches(names []string, id FetcherID, s catalog.Script) bool {
	for _, name := range names {
		if name == s.Key || name == s.ID || name == string(id) {
			return true
		}
	}
	return false
}

func cloneMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func stripInstanceSuffix(name string) string {
	for _, marker := range []string{"_project_", "_region_"} {
		if idx := strings.LastIndex(name, marker); idx >= 0 {
			suffix := name[idx+len(marker):]
			if _, err := strconv.Atoi(suffix); err == nil {
				return name[:idx]
			}
		}
	}
	return name
}
