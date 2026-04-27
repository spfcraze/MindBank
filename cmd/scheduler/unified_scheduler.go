package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	pollInterval      = 60 * time.Second
	mindbankAPI       = "http://localhost:8095"
	stateFile         = "/tmp/mindbank-miner-state.json"
	nodeCreatePath    = "/api/v1/nodes"
	edgeCreatePath    = "/api/v1/edges"
)

// NodeCreatePayload is the request body for creating a node.
type NodeCreatePayload struct {
	WorkspaceName string            `json:"workspace_name"`
	Label         string            `json:"label"`
	NodeType      string            `json:"node_type"`
	Namespace     string            `json:"namespace"`
	Content       string            `json:"content"`
	Summary       string            `json:"summary"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

// NodeCreateResponse is the expected response from the node creation endpoint.
type NodeCreateResponse struct {
	ID string `json:"id"`
}

// EdgeCreatePayload is the request body for creating an edge.
type EdgeCreatePayload struct {
	SourceID string            `json:"source_id"`
	TargetID string            `json:"target_id"`
	EdgeType string            `json:"edge_type"`
	Weight   float64           `json:"weight"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// MinerState tracks which files have already been processed.
type MinerState struct {
	Processed map[string]bool `json:"processed"`
}

func main() {
	fmt.Println("[MindBank Miner] Starting unified scheduler...")

	sessionsDir, err := getSessionsDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[MindBank Miner] Error resolving sessions directory: %v\n", err)
		os.Exit(1)
	}

	cronDirs, err := getCronOutputDirs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[MindBank Miner] Error resolving cron output dirs: %v\n", err)
		// Non-fatal — continue with sessions only
	}

	state := loadState()

	// Immediate first run — process all sources
	if err := runOnce(sessionsDir, state); err != nil {
		fmt.Fprintf(os.Stderr, "[MindBank Miner] First run error (sessions): %v\n", err)
	}
	for _, cronDir := range cronDirs {
		if err := runOnce(cronDir, state); err != nil {
			fmt.Fprintf(os.Stderr, "[MindBank Miner] First run error (cron %s): %v\n", cronDir, err)
		}
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for range ticker.C {
		if err := runOnce(sessionsDir, state); err != nil {
			fmt.Fprintf(os.Stderr, "[MindBank Miner] Poll error (sessions): %v\n", err)
		}
		for _, cronDir := range cronDirs {
			if err := runOnce(cronDir, state); err != nil {
				fmt.Fprintf(os.Stderr, "[MindBank Miner] Poll error (cron %s): %v\n", cronDir, err)
			}
		}
	}
}

func getSessionsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".hermes", "sessions"), nil
}

func getCronOutputDirs() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cronDir := filepath.Join(home, ".hermes", "cron", "output")
	entries, err := os.ReadDir(cronDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, filepath.Join(cronDir, entry.Name()))
		}
	}
	return dirs, nil
}

func runOnce(sessionsDir string, state *MinerState) error {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("[MindBank Miner] Sessions directory does not exist yet: %s\n", sessionsDir)
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		fullPath := filepath.Join(sessionsDir, entry.Name())
		if state.Processed[fullPath] {
			continue
		}

		fmt.Printf("[MindBank Miner] Processing new file: %s\n", fullPath)

		content, err := os.ReadFile(fullPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[MindBank Miner] Failed to read %s: %v\n", fullPath, err)
			continue
		}

		frontmatter, body := parseFrontmatter(string(content))

		ws := "hermes"
		if v, ok := frontmatter["workspace"]; ok && v != "" {
			ws = v
		}
		ns := "hermes"
		if v, ok := frontmatter["namespace"]; ok && v != "" {
			ns = v
		}

		// Extract rich metadata from transcript
		meta := extractSessionMetadata(string(content), frontmatter)

		sessionNode := NodeCreatePayload{
			WorkspaceName: ws,
			Label:         entry.Name(),
			NodeType:      "session",
			Namespace:     ns,
			Content:       truncate(body, 10000), // Store up to 10KB instead of 500 chars
			Summary:       meta.Summary,
			Metadata:      meta.ToMap(),
		}

		sessionID, err := createNode(sessionNode)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[MindBank Miner] Failed to create session node for %s: %v\n", fullPath, err)
			continue
		}
		fmt.Printf("[MindBank Miner] Created session node %s\n", sessionID)

		// Derive knowledge nodes from the body
		knowledgeNodes := extractKnowledgeNodes(body, ws, ns)
		for _, kn := range knowledgeNodes {
			knID, err := createNode(kn)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[MindBank Miner] Failed to create knowledge node for %s: %v\n", fullPath, err)
				continue
			}
			fmt.Printf("[MindBank Miner] Created knowledge node %s (%s)\n", knID, kn.Label)

			edge := EdgeCreatePayload{
				SourceID: sessionID,
				TargetID: knID,
				EdgeType: "produced",
				Weight:   1.0,
			}
			if err := createEdge(edge); err != nil {
				fmt.Fprintf(os.Stderr, "[MindBank Miner] Failed to create edge %s -> %s: %v\n", sessionID, knID, err)
				continue
			}
			fmt.Printf("[MindBank Miner] Created edge %s -> %s\n", sessionID, knID)
		}

		// Auto-detect problems from errors in transcript
		problems := extractProblems(string(content), ws, ns)
		for _, prob := range problems {
			probID, err := createNode(prob)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[MindBank Miner] Failed to create problem node for %s: %v\n", fullPath, err)
				continue
			}
			fmt.Printf("[MindBank Miner] Created problem node %s (%s)\n", probID, prob.Label)

			edge := EdgeCreatePayload{
				SourceID: sessionID,
				TargetID: probID,
				EdgeType: "discovered",
				Weight:   1.0,
			}
			if err := createEdge(edge); err != nil {
				fmt.Fprintf(os.Stderr, "[MindBank Miner] Failed to create edge %s -> %s: %v\n", sessionID, probID, err)
				continue
			}
			fmt.Printf("[MindBank Miner] Created edge %s -> %s (discovered)\n", sessionID, probID)
		}

		// Extract skills used and link them
		skills := extractSkills(string(content))
		for _, skillName := range skills {
			skillNode := NodeCreatePayload{
				WorkspaceName: ws,
				Label:         skillName,
				NodeType:      "tool", // Using "tool" type for skills (or could use "concept")
				Namespace:     ns,
				Content:       "Skill used in session: " + skillName,
				Summary:       "Skill: " + skillName,
			}
			skillID, err := createNode(skillNode)
			if err != nil {
				// Skill may already exist — try to find it or skip
				fmt.Fprintf(os.Stderr, "[MindBank Miner] Skill node creation for %s (may exist): %v\n", skillName, err)
				continue
			}
			edge := EdgeCreatePayload{
				SourceID: sessionID,
				TargetID: skillID,
				EdgeType: "used_skill",
				Weight:   1.0,
			}
			if err := createEdge(edge); err != nil {
				fmt.Fprintf(os.Stderr, "[MindBank Miner] Failed to create skill edge: %v\n", err)
				continue
			}
			fmt.Printf("[MindBank Miner] Linked skill %s to session\n", skillName)
		}

		state.Processed[fullPath] = true
		saveState(state)
	}

	return nil
}

// parseFrontmatter extracts YAML-like key:value pairs between --- markers.
// It returns the parsed metadata map and the remaining body.
func parseFrontmatter(content string) (map[string]string, string) {
	metadata := make(map[string]string)
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "---") {
		return metadata, content
	}

	parts := strings.SplitN(trimmed, "---", 3)
	if len(parts) < 3 {
		return metadata, content
	}

	front := strings.TrimSpace(parts[1])
	body := strings.TrimSpace(parts[2])

	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		val := strings.TrimSpace(line[idx+1:])
		// Strip surrounding quotes if present
		val = strings.Trim(val, `"`)
		val = strings.Trim(val, "'")
		metadata[key] = val
	}

	return metadata, body
}

// extractKnowledgeNodes performs a naive extraction of knowledge nodes from the body.
// It looks for lines starting with "## " as topic headers and creates topic nodes,
// and lines starting with "- " as fact bullets and creates fact nodes.
func extractKnowledgeNodes(body string, ws string, ns string) []NodeCreatePayload {
	var nodes []NodeCreatePayload
	lines := strings.Split(body, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			topic := strings.TrimPrefix(line, "## ")
			nodes = append(nodes, NodeCreatePayload{
				WorkspaceName: ws,
				Label:         topic,
				NodeType:      "topic",
				Namespace:     ns,
				Content:       topic,
				Summary:       topic,
			})
		} else if strings.HasPrefix(line, "- ") {
			fact := strings.TrimPrefix(line, "- ")
			if len(fact) > 0 {
				nodes = append(nodes, NodeCreatePayload{
					WorkspaceName: ws,
					Label:         truncate(fact, 100),
					NodeType:      "fact",
					Namespace:     ns,
					Content:       fact,
					Summary:       fact,
				})
			}
		}
	}

	return nodes
}

// SessionMetadata holds extracted rich metadata from a session transcript.
type SessionMetadata struct {
	Model       string
	Provider    string
	Schedule    string
	TokenCount  int
	ToolCalls   int
	DurationMin int
	ErrorCount  int
	Summary     string
}

// ToMap converts metadata to a string map for the Metadata field.
func (m SessionMetadata) ToMap() map[string]string {
	return map[string]string{
		"model":        m.Model,
		"provider":     m.Provider,
		"schedule":     m.Schedule,
		"token_count":  fmt.Sprintf("%d", m.TokenCount),
		"tool_calls":   fmt.Sprintf("%d", m.ToolCalls),
		"duration_min": fmt.Sprintf("%d", m.DurationMin),
		"error_count":  fmt.Sprintf("%d", m.ErrorCount),
	}
}

// extractSessionMetadata parses the transcript for model, provider, schedule, errors, etc.
func extractSessionMetadata(content string, frontmatter map[string]string) SessionMetadata {
	meta := SessionMetadata{}

	// 1. Model / Provider from frontmatter or transcript
	if v, ok := frontmatter["model"]; ok {
		meta.Model = v
	}
	if v, ok := frontmatter["provider"]; ok {
		meta.Provider = v
	}
	if meta.Model == "" {
		// Try to extract from system prompt metadata block
		modelRe := regexp.MustCompile(`(?i)model\s*[:=]\s*([\w\-\.]+)`)
		if m := modelRe.FindStringSubmatch(content); len(m) > 1 {
			meta.Model = m[1]
		}
	}
	if meta.Provider == "" {
		providerRe := regexp.MustCompile(`(?i)provider\s*[:=]\s*([\w\-\.]+)`)
		if m := providerRe.FindStringSubmatch(content); len(m) > 1 {
			meta.Provider = m[1]
		}
	}

	// 2. Schedule pattern (for cron jobs)
	scheduleRe := regexp.MustCompile(`\*\*Schedule:\*\*\s*([\d\*/\-,\s]+)`)
	if m := scheduleRe.FindStringSubmatch(content); len(m) > 1 {
		meta.Schedule = strings.TrimSpace(m[1])
	}

	// 3. Token count estimate: ~4 chars per token
	meta.TokenCount = len(content) / 4

	// 4. Tool call count: count tool call blocks
	toolRe := regexp.MustCompile(`(?i)(?:functions?\.|tool\s+call|api\(|terminal\(|read_file\(|write_file\(|patch\(|web_search\(|browser_)([a-z_]+)`)
	meta.ToolCalls = len(toolRe.FindAllString(content, -1))

	// 5. Error count
	errorPatterns := []string{
		`exit code [1-9]`,
		`exit code -[1-9]`,
		`error:`,
		`ERROR:`,
		`FAIL`,
		`panic:`,
		`404`,
		`500`,
		`build failed`,
		`cannot find`,
		`no such file`,
	}
	for _, pat := range errorPatterns {
		re := regexp.MustCompile(`(?i)` + pat)
		meta.ErrorCount += len(re.FindAllString(content, -1))
	}

	// 6. Summary: first non-empty line after frontmatter, truncated
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "---") && !strings.HasPrefix(line, "#") {
			meta.Summary = truncate(line, 200)
			break
		}
	}
	if meta.Summary == "" {
		meta.Summary = truncate(content, 200)
	}

	return meta
}

// extractProblems scans the transcript for errors and creates problem nodes.
func extractProblems(content string, ws string, ns string) []NodeCreatePayload {
	var problems []NodeCreatePayload
	lines := strings.Split(content, "\n")

	// Patterns that indicate problems
	problemPatterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)exit code\s+(-?\d+)`),
		regexp.MustCompile(`(?i)(error|ERROR):\s*(.+)`),
		regexp.MustCompile(`(?i)(panic):\s*(.+)`),
		regexp.MustCompile(`(?i)(build failed|cannot build|compilation error)`),
		regexp.MustCompile(`(?i)(404|500|502|503)\s+(not found|error|internal)`),
	}

	for _, line := range lines {
		for _, re := range problemPatterns {
			if m := re.FindStringSubmatch(line); len(m) > 1 {
				label := truncate(strings.TrimSpace(line), 120)
				if label == "" {
					continue
				}
				problems = append(problems, NodeCreatePayload{
					WorkspaceName: ws,
					Label:         label,
					NodeType:      "problem",
					Namespace:     ns,
					Content:       line,
					Summary:       label,
				})
				break // Only one problem per line
			}
		}
	}

	// Deduplicate by label
	seen := make(map[string]bool)
	var uniq []NodeCreatePayload
	for _, p := range problems {
		if !seen[p.Label] {
			seen[p.Label] = true
			uniq = append(uniq, p)
		}
	}
	return uniq
}

// extractSkills finds skill names invoked in the session.
func extractSkills(content string) []string {
	// Pattern: [SYSTEM: The user has invoked the "X" skill]
	// or: invoked the "([^"]+)" skill
	re := regexp.MustCompile(`invoked the "([^"]+)" skill`)
	matches := re.FindAllStringSubmatch(content, -1)

	seen := make(map[string]bool)
	var skills []string
	for _, m := range matches {
		if len(m) > 1 {
			skill := m[1]
			if !seen[skill] {
				seen[skill] = true
				skills = append(skills, skill)
			}
		}
	}
	return skills
}

func createNode(node NodeCreatePayload) (string, error) {
	payload, err := json.Marshal(node)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(mindbankAPI+nodeCreatePath, "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("node create returned %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result NodeCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.ID, nil
}

func createEdge(edge EdgeCreatePayload) error {
	payload, err := json.Marshal(edge)
	if err != nil {
		return err
	}

	resp, err := http.Post(mindbankAPI+edgeCreatePath, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("edge create returned %d: %s", resp.StatusCode, string(bodyBytes))
	}
	return nil
}

func loadState() *MinerState {
	state := &MinerState{Processed: make(map[string]bool)}
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if os.IsNotExist(err) {
			return state
		}
		fmt.Fprintf(os.Stderr, "[MindBank Miner] Warning: could not read state file: %v\n", err)
		return state
	}
	if err := json.Unmarshal(data, state); err != nil {
		fmt.Fprintf(os.Stderr, "[MindBank Miner] Warning: could not parse state file: %v\n", err)
		return state
	}
	return state
}

func saveState(state *MinerState) {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[MindBank Miner] Warning: could not marshal state: %v\n", err)
		return
	}
	if err := os.WriteFile(stateFile, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "[MindBank Miner] Warning: could not write state file: %v\n", err)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
