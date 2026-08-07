package autocapture

import (
	"encoding/json"
	"os"
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

// ParseSessionForNamespace extracts the namespace from a Hermes session file.
// Handles BOTH single-object JSON and JSONL (one object per line) — Hermes
// session files are JSONL, so the old single-Unmarshal always failed on them
// and everything fell back to "global".
//
// Signal priority:
//  1. Explicit working_directory / cwd (any record)
//  2. Frequency-scored /home/<user>/<project> scan across system prompt +
//     message bodies — the reliable signal (a real session mentions its
//     project directory many times)
//  3. claude source_file encoding (.claude/projects/-home-rat-<proj>/...)
//
// Falls back to "global" only when no project signal exists. Never returns a
// hard error — a malformed file just yields "global".
func ParseSessionForNamespace(data []byte) (string, error) {
	return NamespaceFromSession(data), nil
}

// NamespaceFromSession is the robust, error-free namespace extractor.
func NamespaceFromSession(data []byte) string {
	records := decodeSessionRecords(data)
	if len(records) == 0 {
		return "global"
	}

	var textParts []string
	var sourceFiles []string
	collect := func(m map[string]interface{}) {
		for _, key := range []string{"system_prompt", "content", "text", "note"} {
			if v, ok := m[key].(string); ok && v != "" {
				textParts = append(textParts, v)
			}
		}
		if v, ok := m["source_file"].(string); ok && v != "" {
			sourceFiles = append(sourceFiles, v)
		}
	}
	for _, r := range records {
		// Source 1: explicit working directory wins immediately
		for _, key := range []string{"working_directory", "cwd"} {
			if v, ok := r[key].(string); ok && v != "" {
				return DeriveNamespaceFromPath(v)
			}
		}
		collect(r)
		// Single-object format nests turns under a "messages" array
		if msgs, ok := r["messages"].([]interface{}); ok {
			for _, m := range msgs {
				if mm, ok := m.(map[string]interface{}); ok {
					collect(mm)
				}
			}
		}
	}

	// Source 2: frequency-scored /home/<user>/<project> across all text
	if ns := extractProjectFromText(strings.Join(textParts, "\n")); ns != "" {
		return ns
	}

	// Source 3: decode the claude project dir from source_file paths
	for _, sf := range sourceFiles {
		if ns := namespaceFromClaudeSourceFile(sf); ns != "" {
			return ns
		}
	}

	return "global"
}

// decodeSessionRecords parses session bytes as either a single JSON object or
// JSONL. Returns each top-level object as a map; unparseable lines are skipped.
func decodeSessionRecords(data []byte) []map[string]interface{} {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil
	}

	// Single JSON object?
	var single map[string]interface{}
	if json.Unmarshal([]byte(trimmed), &single) == nil {
		return []map[string]interface{}{single}
	}

	// Otherwise treat as JSONL (one object per line)
	var out []map[string]interface{}
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec map[string]interface{}
		if json.Unmarshal([]byte(line), &rec) == nil {
			out = append(out, rec)
		}
	}
	return out
}

// namespaceFromClaudeSourceFile decodes a claude projects path
// (.claude/projects/-home-rat-myproj/<uuid>.jsonl) into a namespace. The
// encoding replaces '/' with '-', so "-home-<user>-myproj" → "/home/<user>/myproj"
// → "myproj". Returns "" when the decoded dir is just the home directory
// (no project) or nothing usable.
func namespaceFromClaudeSourceFile(sourceFile string) string {
	marker := "/projects/"
	idx := strings.Index(sourceFile, marker)
	if idx < 0 {
		return ""
	}
	rest := sourceFile[idx+len(marker):]
	slash := strings.Index(rest, "/")
	if slash >= 0 {
		rest = rest[:slash]
	}
	// rest is like "-home-rat-myproj"; decode dashes to path separators
	decoded := strings.ReplaceAll(rest, "-", "/")
	ns := DeriveNamespaceFromPath(decoded)
	// "/home/<user>" decodes to base "<user>" (home, not a project) — reject it
	if ns == "rat" || ns == "home" {
		return ""
	}
	return ns
}


// homeDirPrefix is the current user's home directory with a trailing slash,
// e.g. "/home/<user>/". Resolved at runtime so namespace detection works for any
// user (and no personal username is baked into the code).
var homeDirPrefix = func() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return strings.TrimRight(h, "/") + "/"
	}
	return "/"
}()

// homePathLiteral returns the OS-native home path pattern ("/home/<user>/") for
// transcript scanning. Legacy "/home/<user>/" transcripts still match via the
// user's own home prefix when the username is the same; other usernames are
// handled because the prefix is computed, not hardcoded.
func homePathLiteral() string { return homeDirPrefix }

// homeUser returns the current username from the home path ("" if unknown).
func homeUser() string {
	p := strings.TrimRight(homeDirPrefix, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}


// NamespaceFromText derives a namespace by scanning arbitrary text (e.g. a
// markdown transcript) for /home/<user>/<project> paths. Returns "" if none
// found, so callers can keep their own fallback.
func NamespaceFromText(text string) string {
	return extractProjectFromText(text)
}

// extractProjectFromText scans text for /home/<user>/<project> patterns and returns
// the most likely project name. Uses frequency scoring to pick the best match.
func extractProjectFromText(text string) string {
	// Find all home/<user>/<name> patterns
	// Match Linux paths: /home/<user>/<project>
	// Also match WSL paths: \\wsl.localhost\Ubuntu\home\rat\<project>
	// Also match Windows paths: C:\Users\ratz\... equivalent

	projectCounts := make(map[string]int)

	// Linux-style: /home/<user>/<project>
	searchText := text
	for {
		idx := strings.Index(searchText, homeDirPrefix)
		if idx < 0 {
			break
		}
		rest := searchText[idx+len(homeDirPrefix):]
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

	// If no home-relative paths, try the WSL-escaped form
	if len(projectCounts) == 0 {
		searchText = text
		// Try with backslash separators (WSL/Windows escaped)
		searchText = strings.ReplaceAll(searchText, "\\\\", "\\") // unescape JSON backslashes
		searchText = strings.ReplaceAll(searchText, "\\home\\"+homeUser()+"\\", homeDirPrefix)
		for {
			idx := strings.Index(searchText, homeDirPrefix)
			if idx < 0 {
				break
			}
			rest := searchText[idx+len(homeDirPrefix):]
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

// isCommonDir returns true if the name is a common directory or a placeholder,
// not a real project.
func isCommonDir(name string) bool {
	common := map[string]bool{
		"go": true, "bin": true, "src": true, "lib": true, "tmp": true,
		"logs": true, "cache": true, ".config": true, ".local": true,
		".hermes": true, ".cargo": true, ".npm": true, "downloads": true,
		"documents": true, "desktop": true, "pictures": true,
		"rat": true, ".omp": true, ".nvm": true, ".cache": true,
		// Documentation placeholders that appear in prompts/examples
		"<project>": true, "project": true, "your-project": true,
		"projectname": true, "name": true,
	}

	// Filter hidden directories (start with '.')
	if strings.HasPrefix(name, ".") {
		return true
	}
	// A real folder name only contains these chars; anything else (angle
	// brackets, quotes, braces) is a template placeholder or parse noise.
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return true
		}
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
