package test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"binaryDeploy/config"
	"binaryDeploy/processmanager"
	"binaryDeploy/updater"
)

// TestWebhookConfig mirrors main.go Config for testing
type TestWebhookConfig struct {
	Port              string   `json:"port"`
	Secret            string   `json:"secret"`
	TargetRepoURL     string   `json:"target_repo_url"`
	SelfUpdateRepoURL string   `json:"self_update_repo_url"`
	DeployDir         string   `json:"deploy_dir"`
	SelfUpdateDir     string   `json:"self_update_dir"`
	AllowedBranches   []string `json:"allowed_branches"`
	LogFile           string   `json:"log_file"`
}

// TestGitHubPushPayload mirrors main.go GitHubPushPayload
type TestGitHubPushPayload struct {
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

// WebhookTestServer manages a test webhook server with real processing
type WebhookTestServer struct {
	Server         *httptest.Server
	Config         TestWebhookConfig
	ProcessManager *processmanager.ProcessManager
	TestEnv        *RealisticTestEnv
	DeploymentDone chan struct{}
	mu             sync.RWMutex
	// Process tracking for safe verification
	DeploymentPIDs map[int]string // PID -> process name ("target-app" or "binaryDeploy")
	ProcessMutex   sync.Mutex
}

// StartRealWebhookServer starts the real webhook handler from main.go for testing
func StartRealWebhookServer(testEnv *RealisticTestEnv, config TestConfig) (*WebhookTestServer, error) {
	webhookConfig := TestWebhookConfig{
		Port:              config.Port,
		Secret:            config.Secret,
		TargetRepoURL:     config.TargetRepoURL,
		SelfUpdateRepoURL: config.SelfUpdateRepoURL,
		DeployDir:         config.DeployDir,
		SelfUpdateDir:     config.SelfUpdateDir,
		AllowedBranches:   config.AllowedBranches,
		LogFile:           config.LogFile,
	}

	processManager := processmanager.NewProcessManager()

	ws := &WebhookTestServer{
		Config:         webhookConfig,
		ProcessManager: processManager,
		TestEnv:        testEnv,
		DeploymentDone: make(chan struct{}, 1),
		DeploymentPIDs: make(map[int]string),
	}

	// Create server with proper routing like main.go
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", ws.webhookHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Webhook server is running")
	})
	server := httptest.NewServer(mux)
	ws.Server = server

	testEnv.ServerURL = server.URL
	testEnv.Logger.Info("Started real webhook server", "url", server.URL)

	return ws, nil
}

// Close closes the webhook server
func (ws *WebhookTestServer) Close() {
	if ws.Server != nil {
		ws.Server.Close()
	}
	if ws.ProcessManager != nil {
		ws.ProcessManager.Shutdown()
	}
}

// UpdateSelfUpdateRepoURL updates the self-update repository URL
func (ws *WebhookTestServer) UpdateSelfUpdateRepoURL(newURL string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.Config.SelfUpdateRepoURL = newURL
	ws.TestEnv.Logger.Info("Updated self-update repository URL", "new_url", newURL)
}

// UpdateTargetRepoURL updates the target repository URL
func (ws *WebhookTestServer) UpdateTargetRepoURL(newURL string) {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	ws.Config.TargetRepoURL = newURL
	ws.TestEnv.Logger.Info("Updated target repository URL", "new_url", newURL)
}

// TrackDeploymentPID tracks a deployment PID for safe verification
func (ws *WebhookTestServer) TrackDeploymentPID(pid int, processName string) {
	ws.ProcessMutex.Lock()
	defer ws.ProcessMutex.Unlock()
	ws.DeploymentPIDs[pid] = processName
	ws.TestEnv.Logger.Info("Tracking deployment PID", "pid", pid, "name", processName)
}

// GetDeploymentPIDs returns all tracked deployment PIDs
func (ws *WebhookTestServer) GetDeploymentPIDs() map[int]string {
	ws.ProcessMutex.Lock()
	defer ws.ProcessMutex.Unlock()

	result := make(map[int]string)
	for pid, name := range ws.DeploymentPIDs {
		result[pid] = name
	}
	return result
}

// UpdateDeploymentPIDs updates the tracked PIDs based on current ProcessManager state
func (ws *WebhookTestServer) UpdateDeploymentPIDs(processName string) {
	pid := ws.ProcessManager.GetCurrentPID()
	if pid != 0 {
		ws.TrackDeploymentPID(pid, processName)
	} else {
		ws.TestEnv.Logger.Warn("ProcessManager reports no current process", "process_name", processName)
	}
}

// webhookHandler implements the real webhook handler logic from main.go
func (ws *WebhookTestServer) webhookHandler(w http.ResponseWriter, r *http.Request) {
	ws.TestEnv.Logger.Info("Incoming webhook request",
		"method", r.Method,
		"path", r.URL.Path,
		"remote_addr", r.RemoteAddr,
		"user_agent", r.Header.Get("User-Agent"),
		"content_type", r.Header.Get("Content-Type"),
		"signature_present", r.Header.Get("X-Hub-Signature-256") != "")

	if r.Method != http.MethodPost {
		ws.TestEnv.Logger.Warn("Invalid HTTP method received", "method", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		http.Error(w, "Missing signature", http.StatusUnauthorized)
		return
	}

	// Read actual body
	bodyBuffer := new(bytes.Buffer)
	_, err := bodyBuffer.ReadFrom(r.Body)
	if err != nil {
		ws.TestEnv.Logger.Error("Failed to read request body", "error", err)
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	body := bodyBuffer.Bytes()

	ws.TestEnv.Logger.Info("Request body read successfully", "body_size", len(body))

	if !ws.verifySignature(body, signature) {
		ws.TestEnv.Logger.Warn("Invalid signature verification",
			"received_signature", signature,
			"body_size", len(body))
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	ws.TestEnv.Logger.Info("Signature verification successful")

	var payload TestGitHubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		ws.TestEnv.Logger.Error("Failed to unmarshal JSON payload", "error", err, "body_preview", string(body[:min(200, len(body))]))
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	ws.TestEnv.Logger.Info("Payload parsed successfully",
		"repository", payload.Repository.Name,
		"ref", payload.Ref,
		"branch", ws.extractBranchFromRef(payload.Ref),
		"commit_id", payload.HeadCommit.ID[:min(8, len(payload.HeadCommit.ID))])

	branch := ws.extractBranchFromRef(payload.Ref)
	if !ws.isAllowedBranch(branch) {
		ws.TestEnv.Logger.Info("Branch not in allowed branches", "branch", branch)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Branch %s is not configured for auto-deployment", branch)
		return
	}

	ws.TestEnv.Logger.Info("Received push event", "branch", branch, "repository", payload.Repository.Name)

	// Check if this is a self-update or target repo deployment
	if payload.Repository.URL == ws.Config.SelfUpdateRepoURL {
		ws.TestEnv.Logger.Info("Detected self-update repository")
		go func() {
			if err := ws.deploySelfUpdate(); err != nil {
				ws.TestEnv.Logger.Error("Self-update deployment failed", "error", err)
			} else {
				ws.TestEnv.Logger.Info("Self-update deployment completed successfully")
				select {
				case ws.DeploymentDone <- struct{}{}:
				default:
				}
			}
		}()
	} else if payload.Repository.URL == ws.Config.TargetRepoURL {
		ws.TestEnv.Logger.Info("Detected target repository")
		go func() {
			if err := ws.deployTargetRepo(payload.Repository.URL); err != nil {
				ws.TestEnv.Logger.Error("Target deployment failed", "error", err)
			} else {
				ws.TestEnv.Logger.Info("Target deployment completed successfully")
				// Track the deployment PID
				ws.UpdateDeploymentPIDs("target-app")
				select {
				case ws.DeploymentDone <- struct{}{}:
				default:
				}
			}
		}()
	} else {
		ws.TestEnv.Logger.Info("Unknown repository", "url", payload.Repository.URL)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Repository not configured for deployment")
		return
	}

	w.WriteHeader(http.StatusOK)
}

// verifySignature implements signature verification from main.go
func (ws *WebhookTestServer) verifySignature(body []byte, signature string) bool {
	if ws.Config.Secret == "" {
		return true
	}

	expectedSig := "sha256=" + ws.computeHMAC(body, ws.Config.Secret)
	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

// computeHMAC computes HMAC from main.go
func (ws *WebhookTestServer) computeHMAC(data []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

// extractBranchFromRef extracts branch from ref
func (ws *WebhookTestServer) extractBranchFromRef(ref string) string {
	return strings.TrimPrefix(ref, "refs/heads/")
}

// isAllowedBranch checks if branch is allowed
func (ws *WebhookTestServer) isAllowedBranch(branch string) bool {
	if len(ws.Config.AllowedBranches) == 0 {
		return true
	}
	for _, allowed := range ws.Config.AllowedBranches {
		if branch == allowed {
			return true
		}
	}
	return false
}

// deploySelfUpdate implements self-update deployment
func (ws *WebhookTestServer) deploySelfUpdate() error {
	// Get current binary path - for testing, we'll use a test binary
	currentBinary := filepath.Join(ws.TestEnv.TempDir, "binaryDeploy-current")

	// Create self-updater
	updaterInstance := updater.NewSelfUpdater(currentBinary, ws.Config.SelfUpdateDir)

	// Perform self-update
	return updaterInstance.Update(ws.Config.SelfUpdateRepoURL, "main")
}

// deployTargetRepo implements target repository deployment
func (ws *WebhookTestServer) deployTargetRepo(repoURL string) error {
	ws.TestEnv.Logger.Info("Starting target deployment process", "repo_url", repoURL, "deploy_dir", ws.Config.DeployDir)

	if err := os.MkdirAll(ws.Config.DeployDir, 0755); err != nil {
		return fmt.Errorf("failed to create deploy directory: %w", err)
	}

	repoDir := filepath.Join(ws.Config.DeployDir, "repo")

	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		ws.TestEnv.Logger.Info("Cloning repository", "path", repoDir)
		if err := ws.runCommandInDir("", "git", "clone", repoURL, repoDir); err != nil {
			return fmt.Errorf("failed to clone repository: %w", err)
		}
	} else {
		ws.TestEnv.Logger.Info("Updating repository", "path", repoDir)
		if err := ws.runCommandInDir(repoDir, "git", "fetch", "origin"); err != nil {
			return fmt.Errorf("failed to fetch updates: %w", err)
		}
		if err := ws.runCommandInDir(repoDir, "git", "reset", "--hard", "origin/HEAD"); err != nil {
			return fmt.Errorf("failed to reset repository: %w", err)
		}
	}

	// Read deploy config from cloned repository
	configPath := filepath.Join(repoDir, "deploy.config")
	deployConfig, err := config.LoadDeployConfig(configPath)
	if err != nil {
		return fmt.Errorf("reading deploy config: %w", err)
	}

	// Run build command
	if deployConfig.BuildCommand != "" {
		ws.TestEnv.Logger.Info("Running build command", "command", deployConfig.BuildCommand)
		if err := ws.runShellCommandInDir(repoDir, deployConfig.BuildCommand); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
	}

	// Start the process using the process manager
	workingDir := repoDir
	if deployConfig.WorkingDir != "" {
		workingDir = filepath.Join(repoDir, deployConfig.WorkingDir)
	}

	ws.TestEnv.Logger.Info("Starting application process", "command", deployConfig.RunCommand, "working_dir", workingDir)
	if err := ws.ProcessManager.StartProcess(deployConfig, workingDir); err != nil {
		return fmt.Errorf("failed to start application process: %w", err)
	}

	ws.TestEnv.Logger.Info("Target deployment completed successfully")
	return nil
}

// runCommandInDir runs a command in a directory
func (ws *WebhookTestServer) runCommandInDir(dir, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	return cmd.Run()
}

// runShellCommandInDir runs a shell command in a directory
func (ws *WebhookTestServer) runShellCommandInDir(dir, shellCommand string) error {
	cmd := exec.Command("sh", "-c", shellCommand)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// min helper function
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// CreateE2EConfig creates a test configuration for E2E testing
func CreateE2EConfig(testEnv *RealisticTestEnv, selfUpdateRepoURL string) TestConfig {
	return TestConfig{
		Port:              "8080",
		Secret:            "test-secret",
		TargetRepoURL:     "file:///tmp/target-repo",
		SelfUpdateRepoURL: selfUpdateRepoURL,
		DeployDir:         filepath.Join(testEnv.DeployDir, "target"),
		SelfUpdateDir:     filepath.Join(testEnv.DeployDir, "selfupdate"),
		AllowedBranches:   []string{"main", "master"},
		LogFile:           filepath.Join(testEnv.LogDir, "test.log"),
	}
}

// TestE2E_SelfUpdate_HappyPath tests the complete self-update workflow
func TestE2E_SelfUpdate_HappyPath(t *testing.T) {
	// Create realistic test environment
	testEnv, err := NewRealisticTestEnv("SelfUpdate_HappyPath")
	if err != nil {
		t.Fatalf("Failed to create test environment: %v", err)
	}
	defer func() {
		if err := testEnv.Cleanup(); err != nil {
			t.Logf("Warning: cleanup failed: %v", err)
		}
	}()

	// Create E2E helper
	helper := NewE2ETestHelper(testEnv)

	// Create initial self-update repository (v1.0.0)
	initialAppFiles, err := CreateSelfUpdateApp("1.0.0")
	if err != nil {
		t.Fatalf("Failed to create initial app template: %v", err)
	}

	initialRepoURL, err := helper.CreateTestRepository("selfupdate-initial", initialAppFiles)
	if err != nil {
		t.Fatalf("Failed to create initial repository: %v", err)
	}
	defer helper.CleanupTestRepository(initialRepoURL)

	// Create updated self-update repository (v2.0.0)
	updatedAppFiles, err := CreateSelfUpdateApp("2.0.0")
	if err != nil {
		t.Fatalf("Failed to create updated app template: %v", err)
	}

	updatedRepoURL, err := helper.CreateTestRepository("selfupdate-updated", updatedAppFiles)
	if err != nil {
		t.Fatalf("Failed to create updated repository: %v", err)
	}
	defer helper.CleanupTestRepository(updatedRepoURL)

	// Create initial binary in temp location
	initialBinaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-current")
	binaryPath, err := helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
	if err != nil {
		t.Fatalf("Failed to create initial binary: %v", err)
	}

	// Copy to expected location
	if err := copyFile(binaryPath, initialBinaryPath); err != nil {
		t.Fatalf("Failed to copy initial binary: %v", err)
	}

	// Make sure it's executable
	if err := os.Chmod(initialBinaryPath, 0755); err != nil {
		t.Fatalf("Failed to make binary executable: %v", err)
	}

	// Create test configuration
	config := CreateE2EConfig(testEnv, initialRepoURL)

	// Start webhook server
	ws, err := StartRealWebhookServer(testEnv, config)
	if err != nil {
		t.Fatalf("Failed to start webhook server: %v", err)
	}
	defer ws.Close()

	// Test initial binary works
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := CreateTestCommand(ctx, initialBinaryPath, "--version")
	if err := cmd.Run(); err != nil {
		t.Fatalf("Initial binary failed --version check: %v", err)
	}

	// Send webhook to trigger self-update with initial repo (should work but no change)
	err = helper.SendWebhook(ws.Server.URL, initialRepoURL, "main", "initial-commit", config.Secret)
	if err != nil {
		t.Fatalf("Failed to send initial webhook: %v", err)
	}

	// Wait a bit for processing
	time.Sleep(2 * time.Second)

	// Now test with updated repository
	ws.UpdateSelfUpdateRepoURL(updatedRepoURL)

	// Send webhook to trigger self-update
	err = helper.SendWebhook(ws.Server.URL, updatedRepoURL, "main", "update-commit", config.Secret)
	if err != nil {
		t.Fatalf("Failed to send update webhook: %v", err)
	}

	// Wait for self-update to complete (up to 30 seconds)
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	updated := false
	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for self-update to complete")
		default:
			// Check if binary was updated by testing its version
			cmd := CreateTestCommand(context.Background(), initialBinaryPath, "--version")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Logf("Version check failed (retrying...): %v", err)
			} else if string(output) != "binaryDeploy-test version 1.0.0\n" {
				// Binary was updated!
				updated = true
				break
			}
			time.Sleep(1 * time.Second)
		}
	}

	if !updated {
		t.Fatal("Binary was not updated after webhook")
	}

	// Verify the updated binary works
	cmd = CreateTestCommand(context.Background(), initialBinaryPath, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Updated binary failed to run: %v", err)
	}

	expectedOutput := "binaryDeploy-test version 2.0.0\n"
	if string(output) != expectedOutput {
		t.Fatalf("Expected output %q, got %q", expectedOutput, string(output))
	}

	// Verify backup was created
	backupPath := initialBinaryPath + ".backup"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatalf("Backup binary was not created at %s", backupPath)
	}

	// Test rollback functionality by corrupting the current binary
	if err := os.WriteFile(initialBinaryPath, []byte("corrupted"), 0644); err != nil {
		t.Fatalf("Failed to corrupt binary for rollback test: %v", err)
	}

	// Restore from backup (simulate rollback)
	if err := copyFile(backupPath, initialBinaryPath); err != nil {
		t.Fatalf("Failed to restore from backup: %v", err)
	}

	// Make sure it's executable after restore
	if err := os.Chmod(initialBinaryPath, 0755); err != nil {
		t.Fatalf("Failed to make rolled back binary executable: %v", err)
	}

	// Verify rollback worked
	cmd = CreateTestCommand(context.Background(), initialBinaryPath, "--version")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Rolled back binary failed to run: %v", err)
	}

	// Should have the backed up version (which is 1.0.0 before the final update) after rollback
	if string(output) != "binaryDeploy-test version 1.0.0\n" {
		t.Fatalf("Rollback failed, expected version 1.0.0, got %s", string(output))
	}

	testEnv.Logger.Info("Self-update E2E test completed successfully",
		"initial_binary", initialBinaryPath,
		"backup_path", backupPath,
		"final_version", string(output))

	// Check for any orphaned binaryDeploy processes using safe verification
	t.Log("Checking for orphaned binaryDeploy processes using safe verification...")

	// Create safe process verifier
	verifier := NewSafeProcessVerifier(testEnv.TempDir, testEnv.Logger)

	// Get tracked deployment PIDs from webhook server (for binaryDeploy self-updates)
	trackedPIDs := ws.GetDeploymentPIDs()

	if len(trackedPIDs) == 0 {
		// Fallback to finding processes by safe criteria
		processes, err := verifier.FindProcessesByName("binaryDeploy")
		if err != nil {
			t.Fatalf("Failed to find binaryDeploy processes: %v", err)
		}

		if len(processes) == 0 {
			t.Logf("Note: No binaryDeploy processes found")
		} else {
			t.Logf("Found %d binaryDeploy process(es)", len(processes))

			// Verify and clean up each process safely
			for _, proc := range processes {
				// Safety verification - this will fail if process is not from our test environment
				if err := verifier.VerifyDeploymentProcess(proc.PID, "binaryDeploy"); err != nil {
					t.Fatalf("❌ SAFETY VIOLATION: BinaryDeploy process %d failed verification: %v", proc.PID, err)
				}

				// Safe kill
				if err := verifier.SafeKillProcess(proc.PID, "binaryDeploy"); err != nil {
					t.Fatalf("Failed to safely kill binaryDeploy process %d: %v", proc.PID, err)
				} else {
					t.Logf("Safely cleaned up binaryDeploy process %d", proc.PID)
				}
			}
		}
	} else {
		// Use tracked PIDs
		for pid, name := range trackedPIDs {
			if err := verifier.VerifyDeploymentProcess(pid, name); err != nil {
				t.Logf("Note: Deployment process %d (%s) is no longer running: %v", pid, name, err)
				continue
			}

			t.Logf("Found deployment process %d (%s)", pid, name)

			// Safe kill
			if err := verifier.SafeKillProcess(pid, name); err != nil {
				t.Fatalf("Failed to safely kill deployment process %d: %v", pid, err)
			} else {
				t.Logf("Safely cleaned up deployment process %d (%s)", pid, name)
			}
		}
	}
}

// TestE2E_SelfUpdate_Rollback tests self-update rollback functionality
func TestE2E_SelfUpdate_Rollback(t *testing.T) {
	// Create realistic test environment
	testEnv, err := NewRealisticTestEnv("SelfUpdate_Rollback")
	if err != nil {
		t.Fatalf("Failed to create test environment: %v", err)
	}
	defer func() {
		if err := testEnv.Cleanup(); err != nil {
			t.Logf("Warning: cleanup failed: %v", err)
		}
	}()

	// Create E2E helper
	helper := NewE2ETestHelper(testEnv)

	// Create initial self-update repository
	initialAppFiles, err := CreateSelfUpdateApp("1.0.0")
	if err != nil {
		t.Fatalf("Failed to create initial app template: %v", err)
	}

	initialRepoURL, err := helper.CreateTestRepository("selfupdate-initial", initialAppFiles)
	if err != nil {
		t.Fatalf("Failed to create initial repository: %v", err)
	}
	defer helper.CleanupTestRepository(initialRepoURL)

	// Create a failing repository (build will fail)
	failingAppFiles, err := CreateFailingApp("build")
	if err != nil {
		t.Fatalf("Failed to create failing app template: %v", err)
	}

	failingRepoURL, err := helper.CreateTestRepository("selfupdate-failing", failingAppFiles)
	if err != nil {
		t.Fatalf("Failed to create failing repository: %v", err)
	}
	defer helper.CleanupTestRepository(failingRepoURL)

	// Create initial binary
	initialBinaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-current")
	binaryPath, err := helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
	if err != nil {
		t.Fatalf("Failed to create initial binary: %v", err)
	}

	if err := copyFile(binaryPath, initialBinaryPath); err != nil {
		t.Fatalf("Failed to copy initial binary: %v", err)
	}

	if err := os.Chmod(initialBinaryPath, 0755); err != nil {
		t.Fatalf("Failed to make binary executable: %v", err)
	}

	// Store original version for verification
	cmd := CreateTestCommand(context.Background(), initialBinaryPath, "--version")
	originalOutput, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Initial binary failed: %v", err)
	}

	// Create test configuration
	config := CreateE2EConfig(testEnv, failingRepoURL)

	// Start webhook server
	ws, err := StartRealWebhookServer(testEnv, config)
	if err != nil {
		t.Fatalf("Failed to start webhook server: %v", err)
	}
	defer ws.Close()

	// Send webhook to trigger failing self-update
	err = helper.SendWebhook(ws.Server.URL, failingRepoURL, "main", "failing-commit", config.Secret)
	if err != nil {
		t.Fatalf("Failed to send failing webhook: %v", err)
	}

	// Wait for update to complete (should rollback)
	time.Sleep(10 * time.Second)

	// Verify original binary is still intact (rollback worked)
	cmd = CreateTestCommand(context.Background(), initialBinaryPath, "--version")
	currentOutput, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Binary failed after failed update (rollback failed): %v", err)
	}

	if string(currentOutput) != string(originalOutput) {
		t.Fatalf("Rollback failed! Expected %q, got %q", string(originalOutput), string(currentOutput))
	}

	// Verify backup exists (it should have been created)
	backupPath := initialBinaryPath + ".backup"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatalf("Backup was not created before rollback")
	}

	testEnv.Logger.Info("Self-update rollback test completed successfully",
		"binary_path", initialBinaryPath,
		"backup_path", backupPath)

	// Check for any orphaned binaryDeploy processes using safe verification
	t.Log("Checking for orphaned binaryDeploy processes using safe verification...")

	// Create safe process verifier
	verifier := NewSafeProcessVerifier(testEnv.TempDir, testEnv.Logger)

	// Get tracked deployment PIDs from webhook server
	trackedPIDs := ws.GetDeploymentPIDs()

	if len(trackedPIDs) == 0 {
		// Fallback to finding processes by safe criteria
		processes, err := verifier.FindProcessesByName("binaryDeploy")
		if err != nil {
			t.Fatalf("Failed to find binaryDeploy processes: %v", err)
		}

		if len(processes) == 0 {
			t.Logf("Note: No binaryDeploy processes found")
		} else {
			t.Logf("Found %d binaryDeploy process(es)", len(processes))

			// Verify and clean up each process safely
			for _, proc := range processes {
				// Safety verification - this will fail if process is not from our test environment
				if err := verifier.VerifyDeploymentProcess(proc.PID, "binaryDeploy"); err != nil {
					t.Fatalf("❌ SAFETY VIOLATION: BinaryDeploy process %d failed verification: %v", proc.PID, err)
				}

				// Safe kill
				if err := verifier.SafeKillProcess(proc.PID, "binaryDeploy"); err != nil {
					t.Fatalf("Failed to safely kill binaryDeploy process %d: %v", proc.PID, err)
				} else {
					t.Logf("Safely cleaned up binaryDeploy process %d", proc.PID)
				}
			}
		}
	} else {
		// Use tracked PIDs
		for pid, name := range trackedPIDs {
			if err := verifier.VerifyDeploymentProcess(pid, name); err != nil {
				t.Logf("Note: Deployment process %d (%s) is no longer running: %v", pid, name, err)
				continue
			}

			t.Logf("Found deployment process %d (%s)", pid, name)

			// Safe kill
			if err := verifier.SafeKillProcess(pid, name); err != nil {
				t.Fatalf("Failed to safely kill deployment process %d: %v", pid, err)
			} else {
				t.Logf("Safely cleaned up deployment process %d (%s)", pid, name)
			}
		}
	}
}

// CreateTestCommand creates a command for testing
func CreateTestCommand(ctx context.Context, binaryPath string, args ...string) *CmdWrapper {
	return &CmdWrapper{
		Path: binaryPath,
		Args: args,
		Ctx:  ctx,
	}
}

// CmdWrapper wraps exec.Cmd for testing
type CmdWrapper struct {
	Path string
	Args []string
	Ctx  context.Context
}

func (c *CmdWrapper) CombinedOutput() ([]byte, error) {
	cmd := exec.CommandContext(c.Ctx, c.Path, c.Args...)
	return cmd.CombinedOutput()
}

func (c *CmdWrapper) Run() error {
	cmd := exec.CommandContext(c.Ctx, c.Path, c.Args...)
	return cmd.Run()
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	// Get source file info to preserve permissions
	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return err
	}

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = destFile.ReadFrom(sourceFile)
	if err != nil {
		return err
	}

	// Preserve permissions
	return os.Chmod(dst, sourceInfo.Mode())
}
