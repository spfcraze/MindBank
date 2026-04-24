package handler

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
)

const githubRepo = "spfcraze/MindBank"

// TrackedFiles are files we can selectively sync from GitHub
var TrackedFiles = []string{
	"web/dashboard/index.html",
	"web/dashboard/graph.html",
	"internal/handler/static/index.html",
	"internal/handler/static/graph.html",
	"plugins/memory/mindbank/__init__.py",
	"scripts/update.sh",
	"scripts/run-migrations.sh",
}

// sha256Hex returns SHA256 hex of bytes
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

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
	r.Get("/updates/file-status", h.FileStatus)
	r.Post("/updates/sync-file", h.SyncFile)
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
	Remote      string `json:"remote_version"`
	Date        string `json:"release_date"`
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
		respondError(w, 404, "update script not found")
		return
	}

	// Generate job ID with timestamp for uniqueness
	jobID := fmt.Sprintf("update-%d-%d", os.Getpid(), time.Now().Unix())

	job := &UpdateJob{
		ID:      jobID,
		Status:  "running",
		Output:  "Starting update...\n",
		Started: time.Now().Format(time.RFC3339),
	}

	h.mu.Lock()
	h.jobs[jobID] = job
	h.mu.Unlock()

	// Run update in background with streaming output
	go func() {
		// 10-minute hard timeout — prevents infinite hangs
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		// Use stdbuf to force line-buffered output (bash buffers when piped)
		var cmd *exec.Cmd
		if _, err := exec.LookPath("stdbuf"); err == nil {
			cmd = exec.CommandContext(ctx, "stdbuf", "-oL", "bash", scriptPath, "--yes", "--no-restart")
		} else {
			cmd = exec.CommandContext(ctx, "bash", scriptPath, "--yes", "--no-restart")
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

		// Wait for process to exit (or context timeout)
		err = cmd.Wait()

		h.mu.Lock()
		if ctx.Err() == context.DeadlineExceeded {
			job.Status = "error"
			job.Output += "\nError: Update timed out after 10 minutes."
			slog.Error("update timed out")
		} else if err != nil {
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

// FileStatus represents the status of a tracked file.
type FileStatus struct {
	Path       string `json:"path"`
	LocalHash  string `json:"local_hash"`
	RemoteHash string `json:"remote_hash"`
	Status     string `json:"status"` // same | changed | unknown | missing
}

// FileStatus handles GET /api/v1/updates/file-status.
func (h *UpdateHandler) FileStatus(w http.ResponseWriter, r *http.Request) {
	results := make([]FileStatus, 0, len(TrackedFiles))
	for _, path := range TrackedFiles {
		localPath := filepath.Join(h.installDir, path)
		localHash := ""
		status := "unknown"

		// Read local file
		localData, err := os.ReadFile(localPath)
		if err != nil {
			if os.IsNotExist(err) {
				status = "missing"
			}
		} else {
			localHash = sha256Hex(localData)
		}

		// Fetch remote from GitHub raw
		remoteHash := ""
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/master/%s", githubRepo, path)
		resp, err := http.Get(rawURL)
		if err == nil && resp.StatusCode == 200 {
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			remoteHash = sha256Hex(data)
		} else if resp != nil {
			resp.Body.Close()
		}

		if status == "unknown" {
			if localHash == "" {
				status = "missing"
			} else if remoteHash == "" {
				status = "unknown"
			} else if localHash == remoteHash {
				status = "same"
			} else {
				status = "changed"
			}
		}

		results = append(results, FileStatus{
			Path:       path,
			LocalHash:  localHash,
			RemoteHash: remoteHash,
			Status:     status,
		})
	}

	respondJSON(w, 200, map[string]interface{}{
		"files":      results,
		"checked_at": time.Now().Format(time.RFC3339),
	})
}

// SyncFileRequest is the request body for sync-file.
type SyncFileRequest struct {
	Path string `json:"path"`
}

// SyncFileResponse is the response for sync-file.
type SyncFileResponse struct {
	Success    bool   `json:"success"`
	Path       string `json:"path"`
	BackedUpTo string `json:"backed_up_to"`
	Message    string `json:"message"`
}

// SyncFile handles POST /api/v1/updates/sync-file.
func (h *UpdateHandler) SyncFile(w http.ResponseWriter, r *http.Request) {
	var req SyncFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON: "+err.Error())
		return
	}

	// Validate path is in tracked files
	allowed := false
	for _, tf := range TrackedFiles {
		if tf == req.Path {
			allowed = true
			break
		}
	}
	if !allowed {
		respondError(w, 400, "path not in tracked files list")
		return
	}

	// Safety: never overwrite .env
	if strings.Contains(req.Path, ".env") {
		respondError(w, 400, "refusing to sync .env")
		return
	}

	localPath := filepath.Join(h.installDir, req.Path)

	// Fetch remote
	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/master/%s", githubRepo, req.Path)
	resp, err := http.Get(rawURL)
	if err != nil {
		respondError(w, 502, "failed to fetch from GitHub: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		respondError(w, 502, fmt.Sprintf("GitHub returned %d", resp.StatusCode))
		return
	}

	remoteData, err := io.ReadAll(resp.Body)
	if err != nil {
		respondError(w, 500, "failed to read remote content")
		return
	}

	// Validate UTF-8
	if !utf8.Valid(remoteData) {
		respondError(w, 400, "remote content is not valid UTF-8")
		return
	}

	// Backup old file if exists
	backupDir := filepath.Join(os.Getenv("HOME"), ".mindbank", "backup", "files", time.Now().Format("20060102-150405"))
	backedUpTo := ""
	if _, err := os.Stat(localPath); err == nil {
		backupPath := filepath.Join(backupDir, req.Path)
		os.MkdirAll(filepath.Dir(backupPath), 0755)
		oldData, _ := os.ReadFile(localPath)
		os.WriteFile(backupPath, oldData, 0644)
		backedUpTo = backupPath
	}

	// Write new file
	os.MkdirAll(filepath.Dir(localPath), 0755)
	if err := os.WriteFile(localPath, remoteData, 0644); err != nil {
		respondError(w, 500, "failed to write file: "+err.Error())
		return
	}

	// Auto-run migrations if a migration file was synced
	if strings.Contains(req.Path, "migrations") || strings.Contains(req.Path, "run-migrations.sh") {
		go func() {
			time.Sleep(1 * time.Second)
			migScript := filepath.Join(h.installDir, "scripts", "run-migrations.sh")
			if _, err := os.Stat(migScript); err == nil {
				cmd := exec.Command("bash", migScript)
				cmd.Dir = h.installDir
				output, err := cmd.CombinedOutput()
				if err != nil {
					slog.Error("auto-migration failed", "error", err, "output", string(output))
				} else {
					slog.Info("auto-migration completed", "output", string(output))
				}
			}
		}()
	}

	respondJSON(w, 200, SyncFileResponse{
		Success:    true,
		Path:       req.Path,
		BackedUpTo: backedUpTo,
		Message:    "Synced from GitHub",
	})
}

// RestartAPI handles POST /api/v1/updates/restart.
// Returns immediately, then restarts the API process after a delay.
// Uses a detached relauncher process that survives the parent death.
func (h *UpdateHandler) RestartAPI(w http.ResponseWriter, r *http.Request) {
	// Respond first, then restart in background
	respondJSON(w, 200, map[string]string{
		"status": "restarting",
		"msg":    "API will restart in 3 seconds. Refresh the page.",
	})

	// Spawn a detached relauncher process that survives our death
	go func() {
		time.Sleep(2 * time.Second) // Give response time to flush

		binPath := filepath.Join(h.installDir, "mindbank-api")
		logPath := "/tmp/mindbank.log"
		port := os.Getenv("MB_PORT")
		if port == "" {
			port = "8095"
		}
		envFile := filepath.Join(h.installDir, ".env")

		// Create a bash relauncher script that does the restart
		// This runs as a DETACHED process — survives parent death
		relaunchScript := fmt.Sprintf(`#!/bin/bash
sleep 2
# Kill old process
kill $(lsof -ti :%s) 2>/dev/null || true
sleep 2
# Wait for port release (up to 10s)
for i in $(seq 1 20); do
  if ! lsof -ti :%s >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
# Start new process
cd %s
if [ -f "%s" ]; then
  source "%s" 2>/dev/null || true
fi
nohup %s >> %s 2>&1 &
`, port, port, h.installDir, envFile, envFile, binPath, logPath)

		// Write temp script
		scriptPath := filepath.Join(os.TempDir(), "mindbank-restart.sh")
		if err := os.WriteFile(scriptPath, []byte(relaunchScript), 0755); err != nil {
			slog.Error("restart: failed to write script", "error", err)
			return
		}

		// Execute script in a NEW process group so it survives parent death
		cmd := exec.Command("bash", scriptPath)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		if err := cmd.Start(); err != nil {
			slog.Error("restart: failed to spawn relauncher", "error", err)
			return
		}
		// Detach completely — don't wait
		cmd.Process.Release()
		slog.Info("restart: relauncher spawned", "script", scriptPath, "pid", cmd.Process.Pid)
	}()
}
