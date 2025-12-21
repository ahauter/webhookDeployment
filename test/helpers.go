package test

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"binaryDeploy/processmanager"
)

// TestConfig holds configuration for integration tests
type TestConfig struct {
	Port              string   `json:"port"`
	Secret            string   `json:"secret"`
	TargetRepoURL     string   `json:"target_repo_url"`
	SelfUpdateRepoURL string   `json:"self_update_repo_url"`
	DeployDir         string   `json:"deploy_dir"`
	SelfUpdateDir     string   `json:"self_update_dir"`
	AllowedBranches   []string `json:"allowed_branches"`
	LogFile           string   `json:"log_file"`
}

// TestEnvironment represents the test environment setup
type TestEnvironment struct {
	T               *testing.T
	Config          *TestConfig
	ReposDir        string
	DeployDir       string
	SelfUpdateDir   string
	LogFile         string
	ProcessManager  *processmanager.ProcessManager
	Server          *httptest.Server
	OriginalConfig  string
	CreatedBranches map[string][]string  // Tracks branches created per repository
	TestID          string               // Unique identifier for test suite
	Repos           map[string]*MockRepo // Tracks created repositories
}

// GitHubPushPayload represents a GitHub webhook payload
type GitHubPushPayload struct {
	Ref        string `json:"ref"`
	Repository struct {
		Name     string `json:"name"`
		CloneURL string `json:"clone_url"`
	} `json:"repository"`
	HeadCommit struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"head_commit"`
}

// MockRepo represents a mock git repository
type MockRepo struct {
	Name       string
	Path       string
	URL        string
	LastCommit string
}

// SetupTestEnvironment creates a complete test environment
func SetupTestEnvironment(t *testing.T) *TestEnvironment {
	t.Helper()

	// Generate unique test ID for this test suite
	testID := fmt.Sprintf("suite-%d-%s", time.Now().Unix(), randomString(4))

	env := &TestEnvironment{
		T:               t,
		CreatedBranches: make(map[string][]string),
		TestID:          testID,
	}

	// Create temporary directories
	tempDir := t.TempDir()
	env.ReposDir = filepath.Join(tempDir, "repos")
	env.DeployDir = filepath.Join(tempDir, "deployments")
	env.SelfUpdateDir = filepath.Join(tempDir, "self_update")
	env.LogFile = filepath.Join(tempDir, "test.log")

	// Create directories
	if err := os.MkdirAll(env.ReposDir, 0755); err != nil {
		t.Fatalf("Failed to create repos dir: %v", err)
	}
	if err := os.MkdirAll(env.DeployDir, 0755); err != nil {
		t.Fatalf("Failed to create deploy dir: %v", err)
	}
	if err := os.MkdirAll(env.SelfUpdateDir, 0755); err != nil {
		t.Fatalf("Failed to create self update dir: %v", err)
	}

	// Setup test configuration
	env.Config = &TestConfig{
		Port:            "8081",
		Secret:          "test-webhook-secret",
		AllowedBranches: []string{"main", "test-branch"},
		LogFile:         env.LogFile,
	}

	// Save original config file
	if _, err := os.Stat("config.json"); err == nil {
		data, err := os.ReadFile("config.json")
		if err != nil {
			t.Fatalf("Failed to read original config: %v", err)
		}
		env.OriginalConfig = string(data)
	}

	// Initialize process manager
	env.ProcessManager = processmanager.NewProcessManager()

	// Setup test logger
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	slog.SetDefault(logger)

	t.Cleanup(func() {
		env.Cleanup()
	})

	return env
}

// CreateMockRepositories creates mock git repositories for testing
func (env *TestEnvironment) CreateMockRepositories() []*MockRepo {
	// Create target app repository
	targetRepo := env.CreateMockRepository("test_target_app", func(path string) {
		// Create main.go
		mainGo := filepath.Join(path, "main.go")
		content := `package main

import "fmt"

func main() {
    fmt.Println("Hello from test target app!")
}`
		if err := os.WriteFile(mainGo, []byte(content), 0644); err != nil {
			env.T.Fatalf("Failed to create main.go: %v", err)
		}

		// Create go.mod
		goMod := filepath.Join(path, "go.mod")
		goModContent := `module test_target_app

go 1.21`
		if err := os.WriteFile(goMod, []byte(goModContent), 0644); err != nil {
			env.T.Fatalf("Failed to create go.mod: %v", err)
		}

		// Create deploy.config
		deployConfig := filepath.Join(path, "deploy.config")
		configContent := `# Target App Test Configuration
build_command=echo "Building test app" && go build -o test_app_binary .
run_command=./test_app_binary
working_dir=./
environment=test
port=8082
restart_delay=2
max_restarts=3`
		if err := os.WriteFile(deployConfig, []byte(configContent), 0644); err != nil {
			env.T.Fatalf("Failed to create deploy.config: %v", err)
		}
	})

	// Create binaryDeploy update repository
	updateRepo := env.CreateMockRepository("test_binarydeploy_updater", func(path string) {
		// Create main.go
		mainGo := filepath.Join(path, "main.go")
		content := `package main

import "fmt"

func main() {
    fmt.Println("Updated binaryDeploy test version!")
}`
		if err := os.WriteFile(mainGo, []byte(content), 0644); err != nil {
			env.T.Fatalf("Failed to create main.go: %v", err)
		}

		// Create go.mod
		goMod := filepath.Join(path, "go.mod")
		goModContent := `module test_binarydeploy_updater

go 1.21`
		if err := os.WriteFile(goMod, []byte(goModContent), 0644); err != nil {
			env.T.Fatalf("Failed to create go.mod: %v", err)
		}

		// Create deploy.config
		deployConfig := filepath.Join(path, "deploy.config")
		configContent := `# BinaryDeploy Self-Update Test Configuration
build_command=echo "Building binaryDeploy test" && go build -o binaryDeploy_test .
restart_command=echo "Mock restart command executed for binaryDeploy"
backup_binary=./test/binaryDeploy_test.backup`
		if err := os.WriteFile(deployConfig, []byte(configContent), 0644); err != nil {
			env.T.Fatalf("Failed to create deploy.config: %v", err)
		}
	})

	// Update config with repository URLs
	env.Config.TargetRepoURL = targetRepo.URL
	env.Config.SelfUpdateRepoURL = updateRepo.URL
	env.Config.DeployDir = env.DeployDir
	env.Config.SelfUpdateDir = env.SelfUpdateDir

	// Create unique test branches for each repository
	env.CreateTestBranch(targetRepo, "MockRepoSetup")
	env.CreateTestBranch(updateRepo, "MockRepoSetup")

	// Store repos in map for easy access
	if env.Repos == nil {
		env.Repos = make(map[string]*MockRepo)
	}
	env.Repos["test_target_app"] = targetRepo
	env.Repos["test_binarydeploy_updater"] = updateRepo

	return []*MockRepo{targetRepo, updateRepo}
}

// CreateMockRepository creates a mock git repository with custom content
func (env *TestEnvironment) CreateMockRepository(name string, setupFunc func(string)) *MockRepo {
	repoPath := filepath.Join(env.ReposDir, name)

	// Create repository directory first
	if err := os.MkdirAll(repoPath, 0755); err != nil {
		env.T.Fatalf("Failed to create repo directory: %v", err)
	}

	// Initialize git repository
	if err := runCommand(repoPath, "git", "init"); err != nil {
		env.T.Fatalf("Failed to init git repo: %v", err)
	}

	if err := runCommand(repoPath, "git", "config", "user.email", "test@example.com"); err != nil {
		env.T.Fatalf("Failed to set git user email: %v", err)
	}

	if err := runCommand(repoPath, "git", "config", "user.name", "Test User"); err != nil {
		env.T.Fatalf("Failed to set git user name: %v", err)
	}

	// Run custom setup function
	if setupFunc != nil {
		setupFunc(repoPath)
	}

	// Initial commit
	if err := runCommand(repoPath, "git", "add", "."); err != nil {
		env.T.Fatalf("Failed to add files: %v", err)
	}

	if err := runCommand(repoPath, "git", "commit", "-m", "Initial commit"); err != nil {
		env.T.Fatalf("Failed to commit: %v", err)
	}

	// Get the commit hash
	commitHash, err := runCommandOutput(repoPath, "git", "rev-parse", "HEAD")
	if err != nil {
		env.T.Fatalf("Failed to get commit hash: %v", err)
	}

	repo := &MockRepo{
		Name:       name,
		Path:       repoPath,
		URL:        "file://" + repoPath,
		LastCommit: strings.TrimSpace(commitHash),
	}

	return repo
}

// CreateBranch creates a new branch in the repository
func (env *TestEnvironment) CreateBranch(repo *MockRepo, branchName string) {
	if err := runCommand(repo.Path, "git", "checkout", "-b", branchName); err != nil {
		env.T.Fatalf("Failed to create branch %s: %v", branchName, err)
	}

	// Make a small change on the branch
	mainGo := filepath.Join(repo.Path, "main.go")
	if err := os.WriteFile(mainGo, []byte(fmt.Sprintf("// Change on branch %s at %s\n%s",
		branchName, time.Now().Format(time.RFC3339), "// Updated content")), 0644); err != nil {
		env.T.Fatalf("Failed to update main.go: %v", err)
	}

	if err := runCommand(repo.Path, "git", "add", "main.go"); err != nil {
		env.T.Fatalf("Failed to add changes: %v", err)
	}

	if err := runCommand(repo.Path, "git", "commit", "-m", fmt.Sprintf("Add feature on %s", branchName)); err != nil {
		env.T.Fatalf("Failed to commit changes: %v", err)
	}

	// Switch back to main
	if err := runCommand(repo.Path, "git", "checkout", "main"); err != nil {
		env.T.Fatalf("Failed to checkout main: %v", err)
	}

	// Update the repo's last commit for the new branch
	commitHash, err := runCommandOutput(repo.Path, "git", "rev-parse", branchName)
	if err != nil {
		env.T.Fatalf("Failed to get branch commit hash: %v", err)
	}
	repo.LastCommit = strings.TrimSpace(commitHash)
}

// MakeCommit creates a new commit in the repository
func (env *TestEnvironment) MakeCommit(repo *MockRepo, message string) string {
	// Check current branch to prevent commits to main during tests
	currentBranch, err := runCommandOutput(repo.Path, "git", "branch", "--show-current")
	if err != nil {
		env.T.Fatalf("Failed to get current branch: %v", err)
	}

	currentBranch = strings.TrimSpace(currentBranch)
	if currentBranch == "main" {
		env.T.Fatalf("Cannot make commits to main branch during tests. Current branch: %s. Use CreateTestBranch() first.", currentBranch)
	}

	// Make a change
	mainGo := filepath.Join(repo.Path, "main.go")
	if err := os.WriteFile(mainGo, []byte(fmt.Sprintf("// Updated at %s: %s\n%s",
		time.Now().Format(time.RFC3339), message, "// Updated content")), 0644); err != nil {
		env.T.Fatalf("Failed to update main.go: %v", err)
	}

	if err := runCommand(repo.Path, "git", "add", "main.go"); err != nil {
		env.T.Fatalf("Failed to add changes: %v", err)
	}

	if err := runCommand(repo.Path, "git", "commit", "-m", message); err != nil {
		env.T.Fatalf("Failed to commit changes: %v", err)
	}

	// Get the new commit hash
	commitHash, err := runCommandOutput(repo.Path, "git", "rev-parse", "HEAD")
	if err != nil {
		env.T.Fatalf("Failed to get commit hash: %v", err)
	}

	repo.LastCommit = strings.TrimSpace(commitHash)
	return repo.LastCommit
}

// WriteTestConfig writes the test configuration to config.json
func (env *TestEnvironment) WriteTestConfig() {
	configData, err := json.MarshalIndent(env.Config, "", "  ")
	if err != nil {
		env.T.Fatalf("Failed to marshal config: %v", err)
	}

	if err := os.WriteFile("config.json", configData, 0644); err != nil {
		env.T.Fatalf("Failed to write config: %v", err)
	}
}

// GenerateWebhookPayload generates a GitHub webhook payload
func (env *TestEnvironment) GenerateWebhookPayload(repoURL, repoName, branch, commitHash, commitMessage string) string {
	payload := GitHubPushPayload{
		Ref: fmt.Sprintf("refs/heads/%s", branch),
		Repository: struct {
			Name     string `json:"name"`
			CloneURL string `json:"clone_url"`
		}{
			Name:     repoName,
			CloneURL: repoURL,
		},
		HeadCommit: struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		}{
			ID:      commitHash,
			Message: commitMessage,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		env.T.Fatalf("Failed to marshal payload: %v", err)
	}

	return string(data)
}

// GenerateHMACSignature generates HMAC-SHA256 signature
func (env *TestEnvironment) GenerateHMACSignature(payload string) string {
	h := hmac.New(sha256.New, []byte(env.Config.Secret))
	h.Write([]byte(payload))
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

// SendWebhookRequest sends a webhook request to the server
func (env *TestEnvironment) SendWebhookRequest(payload string, expectedStatusCode int) (*http.Response, error) {
	signature := env.GenerateHMACSignature(payload)

	req := httptest.NewRequest("POST", "/webhook", bytes.NewReader([]byte(payload)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", signature)

	w := httptest.NewRecorder()

	// We'll need to import and use the actual webhook handler
	// For now, return a mock response
	return w.Result(), nil
}

// VerifyProcessState verifies the process manager state
func (env *TestEnvironment) VerifyProcessState(expectedRunning bool, expectedPID int) {
	if env.ProcessManager.IsRunning() != expectedRunning {
		env.T.Errorf("Expected process running: %v, got: %v", expectedRunning, env.ProcessManager.IsRunning())
	}

	if expectedPID > 0 && env.ProcessManager.GetCurrentPID() != expectedPID {
		env.T.Errorf("Expected PID: %d, got: %d", expectedPID, env.ProcessManager.GetCurrentPID())
	}
}

// WaitForProcess waits for a process to be in the expected state
func (env *TestEnvironment) WaitForProcess(expectedRunning bool, timeout time.Duration) {
	timeoutChan := time.After(timeout)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeoutChan:
			env.T.Fatalf("Timeout waiting for process state: running=%v", expectedRunning)
		case <-ticker.C:
			if env.ProcessManager.IsRunning() == expectedRunning {
				return
			}
		}
	}
}

// Cleanup cleans up the test environment
func (env *TestEnvironment) Cleanup() {
	// Shutdown process manager
	if env.ProcessManager != nil {
		if err := env.ProcessManager.Shutdown(); err != nil {
			env.T.Logf("Error shutting down process manager: %v", err)
		}
	}

	// Restore original config
	if env.OriginalConfig != "" {
		if err := os.WriteFile("config.json", []byte(env.OriginalConfig), 0644); err != nil {
			env.T.Logf("Error restoring original config: %v", err)
		}
	} else {
		// Remove config file if it didn't exist before
		if _, err := os.Stat("config.json"); err == nil {
			if err := os.Remove("config.json"); err != nil {
				env.T.Logf("Error removing config file: %v", err)
			}
		}
	}

	// Close server if it exists
	if env.Server != nil {
		env.Server.Close()
	}

	// Clean up all test branches
	env.CleanupBranches()
}

// Helper functions

func runCommand(dir, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	return cmd.Run()
}

func runCommandOutput(dir, command string, args ...string) (string, error) {
	cmd := exec.Command(command, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	output, err := cmd.Output()
	return string(output), err
}

// randomString generates a random string of specified length
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
}

// GetRepository gets a repository by name from the test environment
func (env *TestEnvironment) GetRepository(name string) *MockRepo {
	if env.Repos == nil {
		env.T.Fatalf("No repositories created. Call CreateMockRepositories() first.")
	}
	repo, exists := env.Repos[name]
	if !exists {
		env.T.Fatalf("Repository '%s' not found. Available repos: %v", name, env.getRepoNames())
	}
	return repo
}

// getRepoNames returns a list of repository names
func (env *TestEnvironment) getRepoNames() []string {
	names := make([]string, 0, len(env.Repos))
	for name := range env.Repos {
		names = append(names, name)
	}
	return names
}

// CreateTestBranch creates a unique test branch and tracks it for cleanup
func (env *TestEnvironment) CreateTestBranch(repo *MockRepo, testName string) string {
	// Generate unique branch name: test-{timestamp}-{testName}-{random}
	timestamp := time.Now().Unix()
	randSuffix := randomString(6)
	branchName := fmt.Sprintf("test-%d-%s-%s", timestamp, testName, randSuffix)

	// Create and checkout the branch
	if err := runCommand(repo.Path, "git", "checkout", "-b", branchName); err != nil {
		env.T.Fatalf("Failed to create branch %s: %v", branchName, err)
	}

	// Make a small change on the branch
	mainGo := filepath.Join(repo.Path, "main.go")
	if err := os.WriteFile(mainGo, []byte(fmt.Sprintf("// Change on branch %s at %s\n%s",
		branchName, time.Now().Format(time.RFC3339), "// Updated content")), 0644); err != nil {
		env.T.Fatalf("Failed to update main.go: %v", err)
	}

	if err := runCommand(repo.Path, "git", "add", "main.go"); err != nil {
		env.T.Fatalf("Failed to add changes: %v", err)
	}

	if err := runCommand(repo.Path, "git", "commit", "-m", fmt.Sprintf("Add feature on %s", branchName)); err != nil {
		env.T.Fatalf("Failed to commit changes: %v", err)
	}

	// Track the branch for cleanup
	if env.CreatedBranches == nil {
		env.CreatedBranches = make(map[string][]string)
	}
	env.CreatedBranches[repo.Name] = append(env.CreatedBranches[repo.Name], branchName)

	// Update the repo's last commit for the new branch
	commitHash, err := runCommandOutput(repo.Path, "git", "rev-parse", "HEAD")
	if err != nil {
		env.T.Fatalf("Failed to get branch commit hash: %v", err)
	}
	repo.LastCommit = strings.TrimSpace(commitHash)

	// Stay on the created branch (don't switch back to main)
	return branchName
}

// CleanupBranches removes all tracked test branches
func (env *TestEnvironment) CleanupBranches() {
	if env.CreatedBranches == nil {
		return
	}

	// First, try to clean up from expected temporary repositories
	for repoName, branches := range env.CreatedBranches {
		// Find the repository path
		repoPath := filepath.Join(env.ReposDir, repoName)
		if _, err := os.Stat(repoPath); os.IsNotExist(err) {
			env.T.Logf("Repository %s not found for cleanup, skipping", repoName)
			continue
		}

		// Ensure we're on main branch
		if err := runCommand(repoPath, "git", "checkout", "main"); err != nil {
			env.T.Logf("Failed to checkout main for repo %s: %v", repoName, err)
			continue
		}

		// Delete each tracked branch
		for _, branch := range branches {
			// Force delete the branch (ignore errors if branch doesn't exist)
			if err := runCommand(repoPath, "git", "branch", "-D", branch); err != nil {
				env.T.Logf("Failed to delete branch %s in repo %s: %v", branch, repoName, err)
			} else {
				env.T.Logf("Successfully deleted branch %s in repo %s", branch, repoName)
			}
		}
	}

	// Also clean up any test branches that may have escaped to the main repository
	env.cleanupMainRepositoryBranches()

	// Clear the tracking map
	env.CreatedBranches = make(map[string][]string)
}

// cleanupMainRepositoryBranches removes test branches from the main repository
func (env *TestEnvironment) cleanupMainRepositoryBranches() {
	// Get current working directory (should be the main repository)
	wd, err := os.Getwd()
	if err != nil {
		env.T.Logf("Failed to get working directory: %v", err)
		return
	}

	// Get all test branches in the main repository
	output, err := runCommandOutput(wd, "git", "branch", "--list", "test-*")
	if err != nil {
		env.T.Logf("Failed to list test branches in main repository: %v", err)
		return
	}

	if output == "" {
		return // No test branches found
	}

	// Ensure we're on main branch
	if err := runCommand(wd, "git", "checkout", "main"); err != nil {
		env.T.Logf("Failed to checkout main in main repository: %v", err)
		return
	}

	// Parse and delete test branches
	branches := strings.Split(strings.TrimSpace(output), "\n")
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if branch == "" {
			continue
		}

		// Remove "* " prefix if present (current branch indicator)
		branch = strings.TrimPrefix(branch, "* ")

		if strings.HasPrefix(branch, "test-") {
			if err := runCommand(wd, "git", "branch", "-D", branch); err != nil {
				env.T.Logf("Failed to delete test branch %s from main repository: %v", branch, err)
			} else {
				env.T.Logf("Successfully cleaned up escaped test branch %s from main repository", branch)
			}
		}
	}
}

// AssertNoError is a helper to assert no error
func AssertNoError(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", msg, err)
	}
}

// AssertEqual is a helper to assert equality
func AssertEqual[T comparable](t *testing.T, expected, actual T, msg string) {
	t.Helper()
	if expected != actual {
		t.Fatalf("%s: expected %v, got %v", msg, expected, actual)
	}
}
