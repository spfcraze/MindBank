package handler

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

// ImportHandler handles Obsidian vault imports.
type ImportHandler struct {
	mu        sync.Mutex
	installDir string
}

// NewImportHandler creates a new import handler with auto-detected install dir.
func NewImportHandler() *ImportHandler {
	dir, _ := os.Getwd()
	if filepath.Base(dir) == "scripts" {
		dir = filepath.Dir(dir)
	}
	return &ImportHandler{installDir: dir}
}

// RegisterImportRoutes registers import routes.
func RegisterImportRoutes(r chi.Router, h *ImportHandler) {
	r.Route("/import", func(r chi.Router) {
		r.Post("/obsidian", h.RunObsidianImport)
		r.Get("/obsidian/status", h.GetImportStatus)
	})
}

// ImportRequest is the request body for Obsidian import.
type ImportRequest struct {
	VaultPath string `json:"vault_path"`
	Namespace string `json:"namespace,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

// ImportResponse is the response for import trigger.
type ImportResponse struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ImportStatus tracks a running import.
type ImportStatus struct {
	JobID   string   `json:"job_id"`
	Status  string   `json:"status"` // running, success, error
	Output  []string `json:"output"`
	Started string   `json:"started"`
	Ended   string   `json:"ended,omitempty"`
	Summary *ImportSummary `json:"summary,omitempty"`
}

// ImportSummary holds final import stats.
type ImportSummary struct {
	NotesFound    int `json:"notes_found"`
	NotesParsed   int `json:"notes_parsed"`
	NotesCreated  int `json:"notes_created"`
	TopicsCreated int `json:"topics_created"`
	EdgesCreated  int `json:"edges_created"`
	Skipped       int `json:"skipped"`
	Errors        int `json:"errors"`
}

var (
	importMu     sync.Mutex
	currentImport *ImportStatus
)

// RunObsidianImport handles POST /api/v1/import/obsidian.
func (h *ImportHandler) RunObsidianImport(w http.ResponseWriter, r *http.Request) {
	var req ImportRequest
	if err := bindJSON(r, &req); err != nil {
		respondError(w, 400, "invalid request: "+err.Error())
		return
	}

	// Validate vault path
	if req.VaultPath == "" {
		respondError(w, 400, "vault_path is required")
		return
	}

	// Check if import already running
	importMu.Lock()
	if currentImport != nil && currentImport.Status == "running" {
		importMu.Unlock()
		respondError(w, 409, "import already running (job: "+currentImport.JobID+")")
		return
	}

	// Create new job
	jobID := fmt.Sprintf("import-%d", time.Now().UnixNano())
	status := &ImportStatus{
		JobID:   jobID,
		Status:  "running",
		Output:  []string{},
		Started: time.Now().Format(time.RFC3339),
	}
	currentImport = status
	importMu.Unlock()

	// Build command
	scriptPath := h.installDir + "/scripts/import-obsidian.py"
	args := []string{scriptPath, req.VaultPath, "--verbose"}

	if req.Namespace != "" {
		args = append(args, "--namespace", req.Namespace)
	}
	if req.DryRun {
		args = append(args, "--dry-run")
	}

	cmd := exec.Command("python3", args...)

	// Stream stdout
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		respondError(w, 500, "failed to create stdout pipe: "+err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		respondError(w, 500, "failed to create stderr pipe: "+err.Error())
		return
	}

	if err := cmd.Start(); err != nil {
		respondError(w, 500, "failed to start import: "+err.Error())
		return
	}

	// Collect output in background
	go func() {
		merged := io.MultiReader(stdout, stderr)
		scanner := bufio.NewScanner(merged)
		scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

		for scanner.Scan() {
			line := scanner.Text()
			importMu.Lock()
			status.Output = append(status.Output, line)
			// Keep last 500 lines
			if len(status.Output) > 500 {
				status.Output = status.Output[len(status.Output)-500:]
			}
			importMu.Unlock()
		}

		err := cmd.Wait()
		importMu.Lock()
		if err != nil {
			status.Status = "error"
			status.Output = append(status.Output, "ERROR: "+err.Error())
		} else {
			status.Status = "success"
		}
		status.Ended = time.Now().Format(time.RFC3339)

		// Parse summary from output
		status.Summary = parseImportSummary(status.Output)
		importMu.Unlock()

		slog.Info("Obsidian import completed",
			"job_id", jobID,
			"status", status.Status,
			"summary", status.Summary,
		)
	}()

	respondJSON(w, 202, ImportResponse{
		JobID:  jobID,
		Status: "running",
	})
}

// GetImportStatus handles GET /api/v1/import/obsidian/status.
func (h *ImportHandler) GetImportStatus(w http.ResponseWriter, r *http.Request) {
	importMu.Lock()
	defer importMu.Unlock()

	if currentImport == nil {
		respondJSON(w, 200, map[string]string{"status": "idle"})
		return
	}

	respondJSON(w, 200, currentImport)
}

// parseImportSummary extracts stats from import output lines.
func parseImportSummary(lines []string) *ImportSummary {
	summary := &ImportSummary{}
	for _, line := range lines {
		// Extract number after last colon in the line
		extractNum := func(s string) int {
			idx := strings.LastIndex(s, ":")
			if idx == -1 {
				return 0
			}
			numStr := strings.TrimSpace(s[idx+1:])
			var n int
			if _, err := fmt.Sscanf(numStr, "%d", &n); err == nil {
				return n
			}
			return 0
		}
		if strings.Contains(line, "Notes found:") {
			summary.NotesFound = extractNum(line)
		} else if strings.Contains(line, "Notes parsed:") {
			summary.NotesParsed = extractNum(line)
		} else if strings.Contains(line, "Notes created:") {
			summary.NotesCreated = extractNum(line)
		} else if strings.Contains(line, "Topics created:") {
			summary.TopicsCreated = extractNum(line)
		} else if strings.Contains(line, "Edges created:") {
			summary.EdgesCreated = extractNum(line)
		} else if strings.Contains(line, "Skipped") && strings.Contains(line, "existing") {
			summary.Skipped = extractNum(line)
		} else if strings.Contains(line, "Errors:") {
			summary.Errors = extractNum(line)
		}
	}
	return summary
}

// respondJSON writes a JSON response — uses shared respondJSON from router.go.
