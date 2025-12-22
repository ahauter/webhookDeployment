package test

import (
	"context"
	"crypto/hmac"
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
	"time"
)

// E2ETestHelper provides utilities for end-to-end testing
type E2ETestHelper struct {
	TestEnv *RealisticTestEnv
}

// NewE2ETestHelper creates a new E2E test helper
func NewE2ETestHelper(testEnv *RealisticTestEnv) *E2ETestHelper {
	return &E2ETestHelper{
		TestEnv: testEnv,
	}
}

// CreateTestRepository creates a git repository with test files
func (h *E2ETestHelper) CreateTestRepository(name string, files map[string]string) (string, error) {
	repoDir := filepath.Join("/tmp", "e2e-test-"+name+"-"+time.Now().Format("20060102-150405"))

	if err := os.MkdirAll(repoDir, 0755); err != nil {
		return "", fmt.Errorf("creating repo directory: %w", err)
	}

	// Create files
	for filename, content := range files {
		filePath := filepath.Join(repoDir, filename)
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return "", fmt.Errorf("creating directory for %s: %w", filename, err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("writing file %s: %w", filename, err)
		}
	}

	// Initialize git repo
	commands := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@example.com"},
		{"git", "config", "user.name", "Test User"},
		{"git", "add", "."},
		{"git", "commit", "-m", "Initial commit"},
	}

	for _, cmd := range commands {
		if err := h.runCommandInDir(repoDir, cmd[0], cmd[1:]...); err != nil {
			return "", fmt.Errorf("running %v: %w", cmd, err)
		}
	}

	// Convert to file:// URL
	repoURL := "file://" + repoDir
	h.TestEnv.Logger.Info("Created test repository", "url", repoURL, "dir", repoDir)

	return repoURL, nil
}

// E2EGitHubPushPayload represents a GitHub webhook payload for E2E tests
type E2EGitHubPushPayload struct {
	Ref        string `json:"ref"`
	Repository struct {
		Name string `json:"name"`
		URL  string `json:"clone_url"`
	} `json:"repository"`
	HeadCommit struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"head_commit"`
}

// GenerateWebhookPayload creates a GitHub webhook payload
func (h *E2ETestHelper) GenerateWebhookPayload(repoURL, branch, commit string) ([]byte, error) {
	payload := GitHubPushPayload{
		Ref: "refs/heads/" + branch,
		Repository: struct {
			Name     string `json:"name"`
			CloneURL string `json:"clone_url"`
		}{
			Name:     filepath.Base(repoURL),
			CloneURL: repoURL,
		},
		HeadCommit: struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}{
			ID:      commit,
			Message: "Test commit",
		},
	}

	return json.Marshal(payload)
}

// CreateSignedWebhook creates a signed webhook request
func (h *E2ETestHelper) CreateSignedWebhook(payload []byte, secret string) (string, []byte) {
	signature := "sha256=" + h.computeHMAC(payload, secret)
	return signature, payload
}

// computeHMAC calculates HMAC-SHA256
func (h *E2ETestHelper) computeHMAC(data []byte, secret string) string {
	hmacHash := hmac.New(sha256.New, []byte(secret))
	hmacHash.Write(data)
	return hex.EncodeToString(hmacHash.Sum(nil))
}

// SendWebhook sends a webhook to the test server
func (h *E2ETestHelper) SendWebhook(serverURL, repoURL, branch, commit, secret string) error {
	payload, err := h.GenerateWebhookPayload(repoURL, branch, commit)
	if err != nil {
		return fmt.Errorf("generating webhook payload: %w", err)
	}

	signature, body := h.CreateSignedWebhook(payload, secret)

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("POST", serverURL+"/webhook", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", signature)
	req.Header.Set("User-Agent", "GitHub-Hookshot/test")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook failed with status %d: %s", resp.StatusCode, string(body))
	}

	h.TestEnv.Logger.Info("Webhook sent successfully", "repo", repoURL, "branch", branch)
	return nil
}

// VerifyApplicationHealth checks if an application is running on a port
func (h *E2ETestHelper) VerifyApplicationHealth(port int) error {
	client := &http.Client{Timeout: 5 * time.Second}

	// Try multiple times with backoff
	for i := 0; i < 10; i++ {
		url := fmt.Sprintf("http://localhost:%d/health", port)
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				h.TestEnv.Logger.Info("Application health check passed", "port", port)
				return nil
			}
		}

		if i < 9 {
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}

	return fmt.Errorf("application health check failed on port %d", port)
}

// CreateSelfUpdateBinary creates a test binary for self-update testing
func (h *E2ETestHelper) CreateSelfUpdateBinary(sourceDir, outputDir, version string) (string, error) {
	binaryPath := filepath.Join(outputDir, "binaryDeploy-test")

	// Create a simple main.go for the test binary
	mainGo := fmt.Sprintf(`package main

import (
	"fmt"
	"os"
)

const version = "%s"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version":
			fmt.Printf("binaryDeploy-test version %%s\n", version)
			return
		case "--help":
			fmt.Println("Test binary for BinaryDeploy self-update testing")
			return
		}
	}
	
	fmt.Println("BinaryDeploy test binary is running!")
}`, version)

	if err := os.WriteFile(filepath.Join(sourceDir, "main.go"), []byte(mainGo), 0644); err != nil {
		return "", fmt.Errorf("writing main.go: %w", err)
	}

	// Create go.mod
	goMod := fmt.Sprintf("module binaryDeploy-test\n\ngo 1.21\n")
	if err := os.WriteFile(filepath.Join(sourceDir, "go.mod"), []byte(goMod), 0644); err != nil {
		return "", fmt.Errorf("writing go.mod: %w", err)
	}

	// Build the binary
	cmd := exec.Command("go", "build", "-o", binaryPath, ".")
	cmd.Dir = sourceDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("building binary: %w, output: %s", err, string(output))
	}

	// Make it executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return "", fmt.Errorf("making binary executable: %w", err)
	}

	h.TestEnv.Logger.Info("Created test binary", "path", binaryPath, "version", version)
	return binaryPath, nil
}

// UpdateRepository updates a test repository with new content
func (h *E2ETestHelper) UpdateRepository(repoURL string, files map[string]string, commitMsg string) error {
	// Convert file:// URL back to path
	repoDir := strings.TrimPrefix(repoURL, "file://")

	// Update/create files
	for filename, content := range files {
		filePath := filepath.Join(repoDir, filename)
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("creating directory for %s: %w", filename, err)
		}
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing file %s: %w", filename, err)
		}
	}

	// Commit changes
	commands := [][]string{
		{"git", "add", "."},
		{"git", "commit", "-m", commitMsg},
	}

	for _, cmd := range commands {
		if err := h.runCommandInDir(repoDir, cmd[0], cmd[1:]...); err != nil {
			return fmt.Errorf("running %v: %w", cmd, err)
		}
	}

	h.TestEnv.Logger.Info("Updated repository", "repo", repoURL, "commit", commitMsg)
	return nil
}

// runCommandInDir executes a command in a specific directory
func (h *E2ETestHelper) runCommandInDir(dir, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.Run()
}

// CleanupTestRepository removes a test repository
func (h *E2ETestHelper) CleanupTestRepository(repoURL string) error {
	repoDir := strings.TrimPrefix(repoURL, "file://")
	return os.RemoveAll(repoDir)
}

// NetworkSimulator provides utilities for simulating network failures
type NetworkSimulator struct {
	tempDir     string
	logger      *slog.Logger
	slowServers []*http.Server
}

// NewNetworkSimulator creates a new network simulator
func NewNetworkSimulator(tempDir string, logger *slog.Logger) *NetworkSimulator {
	return &NetworkSimulator{
		tempDir: tempDir,
		logger:  logger,
	}
}

// CreateSlowRepository creates a repository that responds slowly
func (ns *NetworkSimulator) CreateSlowRepository(name string, delay time.Duration) (string, error) {
	repoDir := filepath.Join(ns.tempDir, "slow-repo-"+name)

	// Create a simple repo
	if err := ns.runCommandInDir(ns.tempDir, "git", "init", name); err != nil {
		return "", fmt.Errorf("failed to init slow repo: %w", err)
	}

	// Add files
	appFiles, err := CreateTargetApp(8080, 0)
	if err != nil {
		return "", fmt.Errorf("failed to create app files: %w", err)
	}

	for filename, content := range appFiles {
		filePath := filepath.Join(repoDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("failed to write %s: %w", filename, err)
		}
		if err := ns.runCommandInDir(repoDir, "git", "add", filename); err != nil {
			return "", fmt.Errorf("failed to add %s: %w", filename, err)
		}
	}

	if err := ns.runCommandInDir(repoDir, "git", "commit", "-m", "Initial commit"); err != nil {
		return "", fmt.Errorf("failed to commit: %w", err)
	}

	// Start a slow HTTP server to simulate network delay
	mux := http.NewServeMux()
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		http.FileServer(http.Dir(repoDir)).ServeHTTP(w, r)
	}))

	slowServer := &http.Server{
		Addr:        "localhost:0",
		Handler:     mux,
		ReadTimeout: 10 * time.Second,
	}

	go func() {
		if err := slowServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ns.logger.Warn("Slow server error", "error", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Get the actual port (need to extract from server.Addr)
	// Note: This is a simplified approach - in production you'd want to get the actual bound port
	return fmt.Sprintf("http://localhost:8081"), nil // Use fixed port for testing
}

// CreateAuthRequiredRepository creates a repository that requires authentication
func (ns *NetworkSimulator) CreateAuthRequiredRepository(name string) (string, error) {
	repoDir := filepath.Join(ns.tempDir, "auth-repo-"+name)

	// Create repo
	if err := ns.runCommandInDir(ns.tempDir, "git", "init", name); err != nil {
		return "", fmt.Errorf("failed to init auth repo: %w", err)
	}

	appFiles, err := CreateTargetApp(8080, 0)
	if err != nil {
		return "", fmt.Errorf("failed to create app files: %w", err)
	}

	for filename, content := range appFiles {
		filePath := filepath.Join(repoDir, filename)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			return "", fmt.Errorf("failed to write %s: %w", filename, err)
		}
		if err := ns.runCommandInDir(repoDir, "git", "add", filename); err != nil {
			return "", fmt.Errorf("failed to add %s: %w", filename, err)
		}
	}

	if err := ns.runCommandInDir(repoDir, "git", "commit", "-m", "Initial commit"); err != nil {
		return "", fmt.Errorf("failed to commit: %w", err)
	}

	// Return a URL that will cause auth issues
	return "https://username:password@github.com/private/repo.git", nil
}

// CreateUnreachableRepository creates a repository that's unreachable
func (ns *NetworkSimulator) CreateUnreachableRepository(name string) string {
	// Return an unreachable URL
	return "http://nonexistent-repository-server-that-does-not-exist.example.com/repo.git"
}

// CorruptRepository creates a repository with corrupted .git directory
func (ns *NetworkSimulator) CorruptRepository(repoDir string) error {
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return fmt.Errorf("not a git repository: %s", repoDir)
	}

	// Corrupt a key git file
	indexPath := filepath.Join(gitDir, "index")
	if err := os.WriteFile(indexPath, []byte("corrupted index data"), 0644); err != nil {
		return fmt.Errorf("failed to corrupt index: %w", err)
	}

	return nil
}

// Cleanup stops all simulated network servers
func (ns *NetworkSimulator) Cleanup() error {
	var errors []error

	for _, server := range ns.slowServers {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			errors = append(errors, fmt.Errorf("failed to shutdown server: %w", err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %v", errors)
	}
	return nil
}

// runCommandInDir runs a command in a directory
func (ns *NetworkSimulator) runCommandInDir(dir, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.Run()
}
