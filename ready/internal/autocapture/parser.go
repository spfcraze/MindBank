package autocapture

import (
	"encoding/json"
	"path/filepath"
	"sort"
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
	base := strings.ToLower(filepath.Base(path))
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
// Checks multiple sources in priority order:
//   1. working_directory / cwd fields (old format)
//   2. System prompt — scans for /home/rat/<project> paths (AGENTS.md injection)
//   3. User messages — scans first messages for /home/rat/<project> paths
// Falls back to "global" if no project directory is found.
func ParseSessionForNamespace(data []byte) (string, error) {
	var session struct {
		WorkingDirectory string `json:"working_directory"`
		CWD              string `json:"cwd"`
		SystemPrompt     string `json:"system_prompt"`
		Messages         []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}

	if err := json.Unmarshal(data, &session); err != nil {
		return "global", err
	}

	// Source 1: Explicit working_directory / cwd (old sessions)
	if session.WorkingDirectory != "" {
		return DeriveNamespaceFromPath(session.WorkingDirectory), nil
	}
	if session.CWD != "" {
		return DeriveNamespaceFromPath(session.CWD), nil
	}

	// Source 2: System prompt — scans for project directory paths
	if session.SystemPrompt != "" {
		if ns := extractProjectFromText(session.SystemPrompt); ns != "" {
			return ns, nil
		}
	}

	// Source 3: First N user messages — scans for project directory paths
	// Collect up to 10 user messages for broader signal
	var userTexts []string
	for _, msg := range session.Messages {
		if msg.Role == "user" && len(strings.TrimSpace(msg.Content)) > 5 {
			userTexts = append(userTexts, msg.Content)
			if len(userTexts) >= 10 {
				break
			}
		}
	}

	combinedUserText := strings.Join(userTexts, " ")
	if combinedUserText != "" {
		if ns := extractProjectFromText(combinedUserText); ns != "" {
			return ns, nil
		}
	}

	return "global", nil
}

// extractProjectFromText scans text for /home/rat/<project> patterns and returns
// the most likely project name. Uses frequency scoring to pick the best match.
func extractProjectFromText(text string) string {
	// Find all /home/rat/<name> patterns
	// Match Linux paths: /home/rat/<project>
	// Also match WSL paths: \\wsl.localhost\Ubuntu\home\rat\<project>
	// Also match Windows paths: C:\Users\ratz\... equivalent

	projectCounts := make(map[string]int)

	// Linux-style: /home/rat/<project>
	searchText := text
	for {
		idx := strings.Index(searchText, "/home/rat/")
		if idx < 0 {
			break
		}
		rest := searchText[idx+len("/home/rat/"):]
		// Extract the project directory name (until next / or space or punctuation)
		end := strings.IndexAny(rest, " \t\n\r/\\")
		if end < 0 {
			end = len(rest)
		}
		name := rest[:end]
		// Strip trailing punctuation
		name = strings.TrimRight(name, ".,;:!?)]}`\"'")
		// Strip file extension if present (not a real directory)
		if strings.Contains(name, ".") {
			ext := filepath.Ext(name)
			if ext != "" && ext != name {
				name = strings.TrimSuffix(name, ext)
			}
		}
		name = strings.ToLower(name)
		if name != "" && !isCommonDir(name) {
			projectCounts[name]++
		}
		searchText = rest[end:]
	}

	// If no /home/rat/ paths, try \home\rat\ (WSL escaped)
	if len(projectCounts) == 0 {
		searchText = text
		// Try with backslash separators (WSL/Windows escaped)
		searchText = strings.ReplaceAll(searchText, "\\\\", "\\") // unescape JSON backslashes
		searchText = strings.ReplaceAll(searchText, "\\home\\rat\\", "/home/rat/")
		for {
			idx := strings.Index(searchText, "/home/rat/")
			if idx < 0 {
				break
			}
			rest := searchText[idx+len("/home/rat/"):]
			end := strings.IndexAny(rest, " \t\n\r\\/")
			if end < 0 {
				end = len(rest)
			}
			name := rest[:end]
			name = strings.TrimRight(name, ".,;:!?)]}`\"'")
			// Strip file extension if present
			if strings.Contains(name, ".") {
				ext := filepath.Ext(name)
				if ext != "" && ext != name {
					name = strings.TrimSuffix(name, ext)
				}
			}
			name = strings.ToLower(name)
			if name != "" && !isCommonDir(name) {
				projectCounts[name]++
			}
			searchText = rest[end:]
		}
	}

	// If we found project references, return the most frequent
	if len(projectCounts) > 0 {
		return bestProject(projectCounts)
	}

	return ""
}

// isCommonDir returns true if the name is a common directory, not a project.
func isCommonDir(name string) bool {
	common := map[string]bool{
		"go": true, "bin": true, "src": true, "lib": true, "tmp": true,
		"logs": true, "cache": true, ".config": true, ".local": true,
		".hermes": true, ".cargo": true, ".npm": true, "downloads": true,
		"documents": true, "desktop": true, "pictures": true,
		"rat": true, ".omp": true, ".nvm": true, ".cache": true,
	}

	// Filter hidden directories (start with '.')
	if strings.HasPrefix(name, ".") {
		return true
	}

	return common[name]
}

// bestProject returns the project with the highest frequency count.
// In case of ties, returns the first alphabetically.
func bestProject(counts map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var pairs []kv
	for k, v := range counts {
		pairs = append(pairs, kv{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].v != pairs[j].v {
			return pairs[i].v > pairs[j].v // higher count first
		}
		return pairs[i].k < pairs[j].k // alphabetical tiebreaker
	})
	if len(pairs) > 0 && pairs[0].v > 0 {
		return pairs[0].k
	}
	return ""
}
