package preflight

import "os/exec"

// ToolStatus captures whether a command-line dependency is available.
type ToolStatus struct {
	Name  string
	Path  string
	Found bool
}

// CheckTools checks PATH for the tools the Python flow warns about.
func CheckTools(names []string) []ToolStatus {
	out := make([]ToolStatus, 0, len(names))
	for _, name := range names {
		path, err := exec.LookPath(name)
		out = append(out, ToolStatus{
			Name:  name,
			Path:  path,
			Found: err == nil,
		})
	}
	return out
}
