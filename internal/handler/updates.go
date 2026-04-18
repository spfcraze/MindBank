package handler

import (
	"bufio"
	"encoding/json"
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

const githubRepo = "spfcraze/MindBank"

// UpdateJob tracks a running update process.
type UpdateJob struct {
	ID      string `json:"id"`
	Status  string `json:"status"` // running, success, error
	Output  string `json:"output"`
	Started string `json:"started"`
}

// UpdateHandler handles update checking and execution.
type UpdateHandler struct {
	mu       sync.Mutex
	jobs     map[string]*UpdateJob
	installDir string
}

// NewUpdateHandler creates a new update handler.
func NewUpdateHandler() *UpdateHandler {
	// Auto-detect install directory (parent of scripts/)
	dir, _ := os.Getwd()
	if filepath.Base(dir) == "scripts" {
		dir = filepath.Dir(dir)
	}
	return &UpdateHandler{
		jobs:       make(map[string]*UpdateJob),
		installDir: dir,
	}
}

// RegisterUpdateRoutes registers update routes.
func RegisterUpdateRoutes(r chi.Router, h *UpdateHandler) {
	r.Get("/updates/check", h.CheckUpdate)
	r.Post("/updates/run", h.RunUpdate)
	r.Post("/updates/restart", h.RestartAPI)
	r.Get("/updates/status/{jobID}", h.GetStatus)
}

// GitHubRelease represents a GitHub release.
type GitHubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
	TarballURL  string `json:"tarball_url"`
}

// UpdateCheckResponse is the response for the check endpoint.
type UpdateCheckResponse struct {
	NeedsUpdate bool   `json:"needs_update"`
	Local       string `json:"local"`
	Remote      string `json:"remote"`
	Date        string `json:"date"`
	Changelog   string `json:"changelog"`
	ReleaseURL  string `json:"release_url"`
	InstallType string `json:"install_type"` // git or tarball
	InstallDir  string `json:"install_dir"`
}

// getLocalVersion reads the VERSION file.
func (h *UpdateHandler) getLocalVersion() string {
	data, err := os.ReadFile(filepath.Join(h.installDir, "VERSION"))
	if err != nil {
		return "0.0.0"
	}
	return strings.TrimSpace(string(data))
}

// isGitInstall checks if this is a git-based install.
func (h *UpdateHandler) isGitInstall() bool {
	_, err := os.Stat(filepath.Join(h.installDir, ".git"))
	return err == nil
}

// CheckUpdate handles GET /api/v1/updates/check.
func (h *UpdateHandler) CheckUpdate(w http.ResponseWriter, r *http.Request) {
	// Fetch latest release from GitHub
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", githubRepo)
	resp, err := http.Get(url)
	if err != nil {
		// Try tags endpoint as fallback
		url = fmt.Sprintf("https://api.github.com/repos/%s/tags", githubRepo)
		resp, err = http.Get(url)
		if err != nil {
			respondError(w, 502, "failed to reach GitHub API: "+err.Error())
			return
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		respondError(w, 500, "failed to read GitHub response")
		return
	}

	if resp.StatusCode != 200 {
		respondError(w, 502, fmt.Sprintf("GitHub API returned %d", resp.StatusCode))
		return
	}

	var release GitHubRelease

	// Handle /releases/latest (single object) vs /tags (array)
	var remoteVersion, releaseDate, releaseURL, tarballURL, changelog string
	if err := json.Unmarshal(body, &release); err == nil && release.TagName != "" {
		// Single release
		remoteVersion = strings.TrimPrefix(release.TagName, "v")
		if len(release.PublishedAt) >= 10 {
			releaseDate = release.PublishedAt[:10]
		}
		releaseURL = release.HTMLURL
		tarballURL = release.TarballURL
		changelog = release.Body
		if len(changelog) > 500 {
			changelog = changelog[:497] + "..."
		}
	} else {
		// Array of tags — take first
		var tags []struct {
			Name   string `json:"name"`
			ZipURL string `json:"zipball_url"`
		}
		if err := json.Unmarshal(body, &tags); err == nil && len(tags) > 0 {
			remoteVersion = strings.TrimPrefix(tags[0].Name, "v")
			releaseDate = "unknown"
			releaseURL = fmt.Sprintf("https://github.com/%s/releases", githubRepo)
		} else {
			respondError(w, 502, "could not parse GitHub response")
			return
		}
	}

	localVersion := h.getLocalVersion()
	needsUpdate := localVersion != remoteVersion && remoteVersion != ""

	installType := "tarball"
	if h.isGitInstall() {
		installType = "git"
	}

	// Cache tarball URL in a file for update.sh to use
	if tarballURL != "" {
		os.WriteFile(filepath.Join(h.installDir, ".update_tarball_url"), []byte(tarballURL), 0644)
	}

	respondJSON(w, 200, UpdateCheckResponse{
		NeedsUpdate: needsUpdate,
		Local:       localVersion,
		Remote:      remoteVersion,
		Date:        releaseDate,
		Changelog:   changelog,
		ReleaseURL:  releaseURL,
		InstallType: installType,
		InstallDir:  h.installDir,
	})
}

// RunUpdate handles POST /api/v1/updates/run.
func (h *UpdateHandler) RunUpdate(w http.ResponseWriter, r *http.Request) {
	// Find update.sh
	scriptPath := filepath.Join(h.installDir, "scripts", "update.sh")
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		// Try downloading it
		respondError(w, 404, "update.sh not found at "+scriptPath)
		return
	}

	// Generate job ID
	jobID := fmt.Sprintf("update-%d", os.Getpid())

	job := &UpdateJob{
		ID:      jobID,
		Status:  "running",
		Output:  "Starting update...\n",
		Started: fmt.Sprintf("%d", os.Getpid()),
	}

	h.mu.Lock()
	h.jobs[jobID] = job
	h.mu.Unlock()

	// Run update in background with streaming output
	go func() {
		// Use stdbuf to force line-buffered output (bash buffers when piped)
		// Fallback: use bash -u if stdbuf not available (e.g., macOS)
		var cmd *exec.Cmd
		if _, err := exec.LookPath("stdbuf"); err == nil {
			cmd = exec.Command("stdbuf", "-oL", "bash", scriptPath, "--yes", "--no-restart")
		} else {
			cmd = exec.Command("bash", scriptPath, "--yes", "--no-restart")
		}
		cmd.Dir = h.installDir
		cmd.Env = append(os.Environ(),
			"MINDBANK_DIR="+h.installDir,
			"AUTO_YES=true",
		)

		// Get separate stdout/stderr pipes for streaming
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			h.mu.Lock()
			job.Output += "\nError: " + err.Error()
			job.Status = "error"
			h.mu.Unlock()
			return
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			h.mu.Lock()
			job.Output += "\nError: " + err.Error()
			job.Status = "error"
			h.mu.Unlock()
			return
		}

		if err := cmd.Start(); err != nil {
			h.mu.Lock()
			job.Output += "\nError starting update: " + err.Error()
			job.Status = "error"
			h.mu.Unlock()
			return
		}

		// Stream stdout and stderr in real-time
		done := make(chan struct{}, 2)
		streamScanner := func(r io.Reader) {
			scanner := bufio.NewScanner(r)
			for scanner.Scan() {
				h.mu.Lock()
				job.Output += scanner.Text() + "\n"
				h.mu.Unlock()
			}
			done <- struct{}{}
		}

		go streamScanner(stdout)
		go streamScanner(stderr)

		// Wait for both streams to finish
		<-done
		<-done

		// Wait for process to exit
		err = cmd.Wait()

		h.mu.Lock()
		if err != nil {
			job.Status = "error"
			job.Output += "\nError: " + err.Error()
			slog.Error("update failed", "error", err)
		} else {
			job.Status = "success"
			slog.Info("update completed successfully")
		}
		h.mu.Unlock()
	}()

	respondJSON(w, 202, map[string]string{
		"job_id": jobID,
		"status": "running",
	})
}

// GetStatus handles GET /api/v1/updates/status/{jobID}.
func (h *UpdateHandler) GetStatus(w http.ResponseWriter, r *http.Request) {
	jobID := chi.URLParam(r, "jobID")

	h.mu.Lock()
	job, ok := h.jobs[jobID]
	h.mu.Unlock()

	if !ok {
		respondError(w, 404, "job not found")
		return
	}

	respondJSON(w, 200, job)
}

// RestartAPI handles POST /api/v1/updates/restart.
// Returns immediately, then restarts the API process after a delay.
func (h *UpdateHandler) RestartAPI(w http.ResponseWriter, r *http.Request) {
	// Respond first, then restart in background
	respondJSON(w, 200, map[string]string{
		"status": "restarting",
		"msg":    "API will restart in 3 seconds. Refresh the page.",
	})

	// Start a background process that waits, kills old, starts new
	go func() {
		time.Sleep(3 * time.Second)

		binPath := filepath.Join(h.installDir, "mindbank-api")
		logPath := "/tmp/mindbank.log"

		// Read .env if present
		envCmd := ""
		envFile := filepath.Join(h.installDir, ".env")
		if _, err := os.Stat(envFile); err == nil {
			envCmd = "source " + envFile + " && "
		}

		// Build restart script
		script := fmt.Sprintf(`
sleep 1
kill $(lsof -ti :%s) 2>/dev/null || true
sleep 2
cd %s
%sMB_DB_DSN="${MB_DB_DSN:-postgres://mindbank:mindbank_secret@localhost:5434/mindbank?sslmode=disable}" \
MB_OLLAMA_URL="${MB_OLLAMA_URL:-http://localhost:11434}" \
MB_PORT="%s" \
nohup %s >> %s 2>&1 &
`, os.Getenv("MB_PORT"), h.installDir, envCmd, os.Getenv("MB_PORT"), binPath, logPath)

		cmd := exec.Command("bash", "-c", script)
		cmd.Dir = h.installDir
		cmd.Env = os.Environ()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Start()

		slog.Info("restart initiated", "bin", binPath)
	}()
}
