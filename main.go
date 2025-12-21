package main

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
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"binaryDeploy/config"
	"binaryDeploy/processmanager"
	"binaryDeploy/updater"
)

// Helper function to get minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type Config struct {
	Port              string   `json:"port"`
	Secret            string   `json:"secret"`
	TargetRepoURL     string   `json:"target_repo_url"`
	SelfUpdateRepoURL string   `json:"self_update_repo_url"`
	DeployDir         string   `json:"deploy_dir"`
	SelfUpdateDir     string   `json:"self_update_dir"`
	AllowedBranches   []string `json:"allowed_branches"`
	LogFile           string   `json:"log_file"`
}

type GitHubPushPayload struct {
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

var (
	appConfig      Config
	processManager *processmanager.ProcessManager
)

func main() {
	loadConfig()
	setupLogger()

	// Initialize process manager
	processManager = processmanager.NewProcessManager()

	server := &http.Server{
		Addr:    ":" + appConfig.Port,
		Handler: setupRoutes(),
	}

	go func() {
		slog.Info("Starting webhook server", "port", appConfig.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down server...")

	// Shutdown process manager first
	if err := processManager.Shutdown(); err != nil {
		slog.Error("Failed to shutdown process manager", "error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
	}

	slog.Info("Server exited")
}

func setupLogger() {
	if appConfig.LogFile == "" {
		appConfig.LogFile = "./binaryDeploy.log"
	}

	logFile, err := os.OpenFile(appConfig.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}

	logger := slog.New(slog.NewJSONHandler(logFile, nil))
	slog.SetDefault(logger)
}

func loadConfig() {
	configFile := "config.json"
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		slog.Error("Config file not found", "file", configFile)
		os.Exit(1)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		slog.Error("Failed to read config file", "error", err)
		os.Exit(1)
	}

	if err := json.Unmarshal(data, &appConfig); err != nil {
		slog.Error("Failed to parse config file", "error", err)
		os.Exit(1)
	}

	if appConfig.Port == "" {
		appConfig.Port = "8080"
	}
}

func setupRoutes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", webhookHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Webhook server is running")
	})
	return mux
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	// Log incoming request details
	slog.Info("Incoming webhook request",
		"method", r.Method,
		"path", r.URL.Path,
		"remote_addr", r.RemoteAddr,
		"user_agent", r.Header.Get("User-Agent"),
		"content_type", r.Header.Get("Content-Type"),
		"signature_present", r.Header.Get("X-Hub-Signature-256") != "")

	if r.Method != http.MethodPost {
		slog.Warn("Invalid HTTP method received", "method", r.Method)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		http.Error(w, "Missing signature", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("Failed to read request body", "error", err)
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	slog.Info("Request body read successfully", "body_size", len(body))

	if !verifySignature(body, signature) {
		slog.Warn("Invalid signature verification",
			"received_signature", signature,
			"body_size", len(body))
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	slog.Info("Signature verification successful")

	var payload GitHubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Error("Failed to unmarshal JSON payload", "error", err, "body_preview", string(body[:min(200, len(body))]))
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	slog.Info("Payload parsed successfully",
		"repository", payload.Repository.Name,
		"ref", payload.Ref,
		"branch", extractBranchFromRef(payload.Ref),
		"commit_id", payload.HeadCommit.ID[:min(8, len(payload.HeadCommit.ID))])

	branch := extractBranchFromRef(payload.Ref)
	if !isAllowedBranch(branch) {
		slog.Info("Branch not in allowed branches", "branch", branch)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Branch %s is not configured for auto-deployment", branch)
		return
	}

	slog.Info("Received push event", "branch", branch, "repository", payload.Repository.Name)

	// Check if this is a self-update or target repo deployment
	if payload.Repository.URL == appConfig.SelfUpdateRepoURL {
		go func() {
			if err := deploySelfUpdate(); err != nil {
				slog.Error("Deployment failed", "error", err)
			} else {
				slog.Info("Deployment completed successfully")
			}
		}()
	} else if payload.Repository.URL == appConfig.TargetRepoURL {
		go func() {
			if err := deployTargetRepo(payload.Repository.URL); err != nil {
				slog.Error("Deployment failed", "error", err)
			} else {
				slog.Info("Deployment completed successfully")
			}
		}()
	} else {
		slog.Info("Unknown repository", "url", payload.Repository.URL)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Repository not configured for deployment")
		return
	}

	w.WriteHeader(http.StatusOK)
}

func verifySignature(body []byte, signature string) bool {
	if appConfig.Secret == "" {
		return true
	}

	expectedSig := "sha256=" + computeHMAC(body, appConfig.Secret)
	return hmac.Equal([]byte(signature), []byte(expectedSig))
}

func computeHMAC(data []byte, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

func extractBranchFromRef(ref string) string {
	return strings.TrimPrefix(ref, "refs/heads/")
}

func isAllowedBranch(branch string) bool {
	if len(appConfig.AllowedBranches) == 0 {
		return true
	}
	for _, allowed := range appConfig.AllowedBranches {
		if branch == allowed {
			return true
		}
	}
	return false
}

func deployTargetRepo(repoURL string) error {
	slog.Info("Starting deployment process", "repo_url", repoURL)

	if err := os.MkdirAll(appConfig.DeployDir, 0755); err != nil {
		return fmt.Errorf("failed to create deploy directory: %w", err)
	}

	repoDir := filepath.Join(appConfig.DeployDir, "repo")

	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		slog.Info("Cloning repository", "path", repoDir)
		if err := runCommandInDir("", "git", "clone", repoURL, repoDir); err != nil {
			return fmt.Errorf("failed to clone repository: %w", err)
		}
	} else {
		slog.Info("Updating repository", "path", repoDir)
		if err := runCommandInDir(repoDir, "git", "fetch", "origin"); err != nil {
			return fmt.Errorf("failed to fetch updates: %w", err)
		}
		if err := runCommandInDir(repoDir, "git", "reset", "--hard", "origin/HEAD"); err != nil {
			return fmt.Errorf("failed to reset repository: %w", err)
		}
	}

	// Read deploy config from the cloned repository
	configPath := filepath.Join(repoDir, "deploy.config")
	deployConfig, err := config.LoadDeployConfig(configPath)
	if err != nil {
		return fmt.Errorf("reading deploy config: %w", err)
	}

	// Run build command
	if deployConfig.BuildCommand != "" {
		slog.Info("Running build command", "command", deployConfig.BuildCommand)
		if err := runShellCommandInDir(repoDir, deployConfig.BuildCommand); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
	}

	// Start the process using the process manager
	workingDir := repoDir
	if deployConfig.WorkingDir != "" {
		workingDir = filepath.Join(repoDir, deployConfig.WorkingDir)
	}

	slog.Info("Starting application process", "command", deployConfig.RunCommand, "working_dir", workingDir)
	if err := processManager.StartProcess(deployConfig, workingDir); err != nil {
		return fmt.Errorf("failed to start application process: %w", err)
	}

	return nil
}

func deploySelfUpdate() error {
	slog.Info("Starting self-update process")

	// Get current binary path
	currentBinary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("getting current binary path: %w", err)
	}

	// Create self-updater
	updaterInstance := updater.NewSelfUpdater(currentBinary, appConfig.SelfUpdateDir)

	// Perform self-update
	return updaterInstance.Update(appConfig.SelfUpdateRepoURL, "main")
}

func runCommand(dir, command string, args ...string) error {
	return runCommandInDir(dir, command, args...)
}

func runCommandInDir(dir, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func runShellCommandInDir(dir, shellCommand string) error {
	cmd := exec.Command("sh", "-c", shellCommand)
	if dir != "" {
		cmd.Dir = dir
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
