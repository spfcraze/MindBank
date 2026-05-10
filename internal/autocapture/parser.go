package autocapture

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// DeriveNamespaceFromPath extracts the leaf folder name from a path.
// Falls back to "global" for empty or root paths.
// Strips worker suffixes (e.g., -worker-123) to prevent namespace pollution.
func DeriveNamespaceFromPath(path string) string {
	path = NormalizePath(path)
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return "global"
	}
	path = strings.TrimSuffix(path, "/")
	base := filepath.Base(path)
	if base == "/" || base == "." || base == "" {
		return "global"
	}
	return base
}

// NormalizePath strips known ephemeral suffixes from paths.
// This prevents worker subdirectories (e.g., -worker-123) from creating
// fragmented namespaces. Also strips team-worker suffixes.
func NormalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "/" {
		return path
	}
	path = strings.TrimSuffix(path, "/")

	// Strip worker suffixes: -worker-NUMBER
	// Also handles: -team-worker-NUMBER
	for {
		stripped := stripWorkerSuffix(path)
		if stripped == path {
			break
		}
		path = stripped
	}

	return path
}

// stripWorkerSuffix removes one trailing -worker-NUMBER suffix.
func stripWorkerSuffix(path string) string {
	// Match pattern: -worker-123 at end of last path component
	lastSlash := strings.LastIndex(path, "/")
	base := path
	prefix := ""
	if lastSlash >= 0 {
		prefix = path[:lastSlash]
		base = path[lastSlash+1:]
	}

	// Check for -worker-NUMBER at end of base
	workerIdx := strings.LastIndex(base, "-worker-")
	if workerIdx < 0 {
		return path
	}

	// Verify the part after -worker- is numeric
	suffix := base[workerIdx+len("-worker-"):]
	if suffix == "" || !isAllDigits(suffix) {
		return path
	}

	// Strip it
	newBase := base[:workerIdx]
	if newBase == "" {
		return prefix
	}
	if prefix == "" {
		return newBase
	}
	return prefix + "/" + newBase
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// ParseSessionForNamespace extracts the namespace from a Hermes session JSON.
// Looks for "working_directory" or "cwd" fields.
func ParseSessionForNamespace(data []byte) (string, error) {
	var session struct {
		WorkingDirectory string `json:"working_directory"`
		CWD              string `json:"cwd"`
	}
	if err := json.Unmarshal(data, &session); err != nil {
		return "global", err
	}
	if session.WorkingDirectory != "" {
		return DeriveNamespaceFromPath(session.WorkingDirectory), nil
	}
	if session.CWD != "" {
		return DeriveNamespaceFromPath(session.CWD), nil
	}
	return "global", nil
}
