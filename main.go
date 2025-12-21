package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type Config struct {
	Port            string   `json:"port"`
	Secret          string   `json:"secret"`
	RepoURL         string   `json:"repo_url"`
	DeployDir       string   `json:"deploy_dir"`
	BuildCommand    string   `json:"build_command"`
	RestartCommand  string   `json:"restart_command"`
	AllowedBranches []string `json:"allowed_branches"`
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

var config Config

func main() {
	loadConfig()

	server := &http.Server{
		Addr:    ":" + config.Port,
		Handler: setupRoutes(),
	}

	go func() {
		log.Printf("Starting webhook server on port %s", config.Port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited")
}

func loadConfig() {
	configFile := "config.json"
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		log.Fatalf("Config file %s not found", configFile)
	}

	data, err := os.ReadFile(configFile)
	if err != nil {
		log.Fatalf("Failed to read config file: %v", err)
	}

	if err := json.Unmarshal(data, &config); err != nil {
		log.Fatalf("Failed to parse config file: %v", err)
	}

	if config.Port == "" {
		config.Port = "8080"
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
	if r.Method != http.MethodPost {
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
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}
	defer r.Body.Close()

	if !verifySignature(body, signature) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	var payload GitHubPushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	branch := extractBranchFromRef(payload.Ref)
	if !isAllowedBranch(branch) {
		log.Printf("Branch %s is not in allowed branches", branch)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Branch %s is not configured for auto-deployment", branch)
		return
	}

	log.Printf("Received push event for branch %s, repository %s", branch, payload.Repository.Name)

	go func() {
		if err := deploy(); err != nil {
			log.Printf("Deployment failed: %v", err)
		} else {
			log.Printf("Deployment completed successfully")
		}
	}()

	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Deployment started for branch %s", branch)
}

func verifySignature(body []byte, signature string) bool {
	if config.Secret == "" {
		return true
	}

	expectedSig := "sha256=" + computeHMAC(body, config.Secret)
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
	if len(config.AllowedBranches) == 0 {
		return true
	}
	for _, allowed := range config.AllowedBranches {
		if branch == allowed {
			return true
		}
	}
	return false
}

func deploy() error {
	log.Println("Starting deployment process...")

	if err := os.MkdirAll(config.DeployDir, 0755); err != nil {
		return fmt.Errorf("failed to create deploy directory: %w", err)
	}

	repoDir := filepath.Join(config.DeployDir, "repo")

	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		log.Printf("Cloning repository to %s", repoDir)
		if err := runCommand("git", "clone", config.RepoURL, repoDir); err != nil {
			return fmt.Errorf("failed to clone repository: %w", err)
		}
	} else {
		log.Printf("Updating repository in %s", repoDir)
		if err := runCommandInDir(repoDir, "git", "fetch", "origin"); err != nil {
			return fmt.Errorf("failed to fetch updates: %w", err)
		}
		if err := runCommandInDir(repoDir, "git", "reset", "--hard", "origin/HEAD"); err != nil {
			return fmt.Errorf("failed to reset repository: %w", err)
		}
	}

	if config.BuildCommand != "" {
		log.Printf("Running build command: %s", config.BuildCommand)
		parts := strings.Fields(config.BuildCommand)
		if err := runCommandInDir(repoDir, parts[0], parts[1:]...); err != nil {
			return fmt.Errorf("build failed: %w", err)
		}
	}

	if config.RestartCommand != "" {
		log.Printf("Running restart command: %s", config.RestartCommand)
		parts := strings.Fields(config.RestartCommand)
		if err := runCommand("", parts[0], parts[1:]...); err != nil {
			return fmt.Errorf("restart failed: %w", err)
		}
	}

	return nil
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
