package preflight

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/ini.v1"
)

// Profile is one AWS profile discovered from ~/.aws/config.
type Profile struct {
	Name   string
	Region string
	Note   string
}

// LoadAWSProfiles parses an AWS config file. Empty path means ~/.aws/config.
// It accepts both [default] and [profile name] section styles.
func LoadAWSProfiles(path string) ([]Profile, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		path = filepath.Join(home, ".aws", "config")
	}
	cfg, err := ini.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	profiles := []Profile{}
	seen := map[string]bool{}
	for _, sec := range cfg.Sections() {
		raw := sec.Name()
		if raw == ini.DefaultSection {
			continue
		}
		name := profileName(raw)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		profiles = append(profiles, Profile{
			Name:   name,
			Region: sec.Key("region").String(),
			Note:   profileNote(sec),
		})
	}

	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].Name == "default" {
			return true
		}
		if profiles[j].Name == "default" {
			return false
		}
		return profiles[i].Name < profiles[j].Name
	})
	return profiles, nil
}

func profileName(section string) string {
	section = strings.TrimSpace(section)
	if section == "default" {
		return section
	}
	return strings.TrimPrefix(section, "profile ")
}

func profileNote(sec *ini.Section) string {
	switch {
	case sec.HasKey("sso_start_url"), sec.HasKey("sso_session"):
		return "AWS SSO profile"
	case sec.HasKey("role_arn"):
		return "assume-role profile"
	default:
		return "AWS config profile"
	}
}
