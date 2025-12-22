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
	"binaryDeploy/monitor"
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
	// Handle command line flags
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version":
			fmt.Println("binaryDeploy version 1.0.0")
			return
		case "--help":
			fmt.Println("BinaryDeploy - Self-Updating Git Webhook Server")
			fmt.Println("Usage:")
			fmt.Println("  binaryDeploy              - Start webhook server")
			fmt.Println("  binaryDeploy --version    - Show version information")
			fmt.Println("  binaryDeploy --help       - Show this help message")
			return
		}
	}

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

	// Create monitor handler with server config
	serverConfig := &monitor.ServerConfig{
		Port:              appConfig.Port,
		Secret:            appConfig.Secret,
		TargetRepoURL:     appConfig.TargetRepoURL,
		SelfUpdateRepoURL: appConfig.SelfUpdateRepoURL,
		DeployDir:         appConfig.DeployDir,
		SelfUpdateDir:     appConfig.SelfUpdateDir,
		AllowedBranches:   appConfig.AllowedBranches,
		LogFile:           appConfig.LogFile,
	}

	monitorHandler := monitor.NewHandler(processManager, serverConfig)
	monitorHandler.RegisterRoutes(mux)

	mux.HandleFunc("/webhook", webhookHandler)

	// Manual deployment endpoint for testing
	mux.HandleFunc("/deploy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			if err := deployTargetRepo(appConfig.TargetRepoURL); err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			} else {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]string{"status": "deployment started"})
			}
			return
		}
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Webhook server is running")
	})
	return mux
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := map[string]interface{}{
		"server": map[string]interface{}{
			"port":             appConfig.Port,
			"target_repo":      appConfig.TargetRepoURL,
			"self_update_repo": appConfig.SelfUpdateRepoURL,
			"allowed_branches": appConfig.AllowedBranches,
		},
		"process":   processManager.GetWebStatus(),
		"timestamp": time.Now().Format(time.RFC3339),
	}

	json.NewEncoder(w).Encode(status)
}

func monitorHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Binary Deploy Monitor</title>
    <style>
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; margin: 0; background: #f5f5f5; }
        .container { max-width: 1200px; margin: 0 auto; padding: 20px; }
        .header { background: white; padding: 20px; border-radius: 8px; margin-bottom: 20px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .status-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 20px; margin-bottom: 20px; }
        .card { background: white; padding: 20px; border-radius: 8px; box-shadow: 0 2px 4px rgba(0,0,0,0.1); }
        .status-indicator { display: inline-block; width: 12px; height: 12px; border-radius: 50%; margin-right: 8px; }
        .status-running { background: #4CAF50; }
        .status-stopped { background: #f44336; }
        .status-item { margin: 8px 0; }
        .label { font-weight: 600; color: #666; }
        .value { color: #333; }
        .config-details { background: #f8f9fa; padding: 10px; border-radius: 4px; margin-top: 10px; font-family: monospace; font-size: 12px; }
        .refresh-btn { background: #007bff; color: white; border: none; padding: 8px 16px; border-radius: 4px; cursor: pointer; }
        .refresh-btn:hover { background: #0056b3; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h1>🚀 Binary Deploy Monitor</h1>
            <button class="refresh-btn" onclick="loadStatus()">Refresh</button>
            <span style="float: right; color: #666; font-size: 14px;" id="last-update"></span>
        </div>
        
        <div class="status-grid">
            <div class="card">
                <h3>📡 Server Status</h3>
                <div class="status-item">
                    <span class="label">Port:</span>
                    <span class="value" id="server-port">-</span>
                </div>
                <div class="status-item">
                    <span class="label">Target Repository:</span>
                    <span class="value" id="target-repo">-</span>
                </div>
                <div class="status-item">
                    <span class="label">Self-Update Repository:</span>
                    <span class="value" id="self-update-repo">-</span>
                </div>
                <div class="status-item">
                    <span class="label">Allowed Branches:</span>
                    <span class="value" id="allowed-branches">-</span>
                </div>
            </div>
            
            <div class="card">
                <h3>⚡ Process Status</h3>
                <div class="status-item">
                    <span class="label">Status:</span>
                    <span id="process-status">
                        <span class="status-indicator status-stopped"></span>
                        <span>Stopped</span>
                    </span>
                </div>
                <div class="status-item">
                    <span class="label">PID:</span>
                    <span class="value" id="process-pid">-</span>
                </div>
                <div class="status-item">
                    <span class="label">Uptime:</span>
                    <span class="value" id="process-uptime">-</span>
                </div>
                <div class="status-item">
                    <span class="label">Restart Count:</span>
                    <span class="value" id="restart-count">-</span>
                </div>
                <div class="status-item">
                    <span class="label">Command:</span>
                    <span class="value" id="process-command">-</span>
                </div>
                <div class="status-item">
                    <span class="label">Working Directory:</span>
                    <span class="value" id="working-dir">-</span>
                </div>
            </div>
        </div>
        
        <div class="card">
            <h3>⚙️ Process Configuration</h3>
            <div class="config-details" id="process-config">
                No process running
            </div>
        </div>
    </div>

    <script>
        function loadStatus() {
            fetch('/status')
                .then(response => response.json())
                .then(data => {
                    updateServerInfo(data.server);
                    updateProcessInfo(data.process);
                    document.getElementById('last-update').textContent = 'Last updated: ' + new Date(data.timestamp).toLocaleTimeString();
                })
                .catch(error => {
                    console.error('Error fetching status:', error);
                });
        }
        
        function updateServerInfo(server) {
            document.getElementById('server-port').textContent = server.port;
            document.getElementById('target-repo').textContent = server.target_repo || 'Not configured';
            document.getElementById('self-update-repo').textContent = server.self_update_repo || 'Not configured';
            document.getElementById('allowed-branches').textContent = server.allowed_branches.join(', ') || 'All branches';
        }
        
        function updateProcessInfo(process) {
            const statusElement = document.getElementById('process-status');
            if (process.running) {
                statusElement.innerHTML = '<span class="status-indicator status-running"></span><span>Running</span>';
                document.getElementById('process-pid').textContent = process.pid;
                document.getElementById('process-uptime').textContent = process.uptime;
                document.getElementById('restart-count').textContent = process.restart_count;
                document.getElementById('process-command').textContent = process.command;
                document.getElementById('working-dir').textContent = process.working_dir;
                
                const config = process.config;
                let configHtml = '<strong>Build Command:</strong> ' + (config.build_command || 'N/A') + '<br>' +
                               '<strong>Run Command:</strong> ' + (config.run_command || 'N/A') + '<br>' +
                               '<strong>Working Dir:</strong> ' + (config.working_dir || 'N/A') + '<br>' +
                               '<strong>Environment:</strong> ' + (config.environment || 'N/A') + '<br>' +
                               '<strong>Max Restarts:</strong> ' + (config.max_restarts || 0) + '<br>' +
                               '<strong>Restart Delay:</strong> ' + (config.restart_delay || 0) + 's';
                document.getElementById('process-config').innerHTML = configHtml;
            } else {
                statusElement.innerHTML = '<span class="status-indicator status-stopped"></span><span>Stopped</span>';
                document.getElementById('process-pid').textContent = '-';
                document.getElementById('process-uptime').textContent = '-';
                document.getElementById('restart-count').textContent = '0';
                document.getElementById('process-command').textContent = '-';
                document.getElementById('working-dir').textContent = '-';
                document.getElementById('process-config').innerHTML = 'No process running';
            }
        }
        
        // Auto-refresh every 5 seconds
        setInterval(loadStatus, 5000);
        
        // Initial load
        loadStatus();
    </script>
</body>
</html>`

	fmt.Fprintf(w, html)
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

	// Validate payload is not empty
	if len(body) == 0 {
		slog.Warn("Empty request body received")
		http.Error(w, "Empty request body", http.StatusBadRequest)
		return
	}

	// Validate JSON structure - reject empty objects
	trimmedBody := strings.TrimSpace(string(body))
	if trimmedBody == "{}" {
		slog.Warn("Empty JSON object received")
		http.Error(w, "Invalid JSON payload - empty object", http.StatusBadRequest)
		return
	}

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
		http.Error(w, "Invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Validate required GitHub webhook fields
	if payload.Repository.Name == "" {
		slog.Warn("Missing repository name in payload")
		http.Error(w, "Invalid payload - missing repository name", http.StatusBadRequest)
		return
	}
	if payload.Ref == "" {
		slog.Warn("Missing ref in payload")
		http.Error(w, "Invalid payload - missing ref", http.StatusBadRequest)
		return
	}
	if payload.HeadCommit.ID == "" {
		slog.Warn("Missing commit ID in payload")
		http.Error(w, "Invalid payload - missing commit ID", http.StatusBadRequest)
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
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Deployment triggered for %s", payload.Repository.Name)
		go func() {
			if err := deploySelfUpdate(); err != nil {
				slog.Error("Self-update deployment failed", "error", err)
			} else {
				slog.Info("Self-update deployment completed successfully")
			}
		}()
	} else if payload.Repository.URL == appConfig.TargetRepoURL {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Deployment triggered for %s", payload.Repository.Name)
		go func() {
			if err := deployTargetRepo(payload.Repository.URL); err != nil {
				slog.Error("Target deployment failed", "error", err)
			} else {
				slog.Info("Target deployment completed successfully")
			}
		}()
	} else {
		slog.Info("Unknown repository", "url", payload.Repository.URL)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Repository not configured for deployment")
		return
	}
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
		// Support wildcard patterns like "test-*"
		if strings.HasSuffix(allowed, "*") {
			prefix := strings.TrimSuffix(allowed, "*")
			if strings.HasPrefix(branch, prefix) {
				return true
			}
		} else if branch == allowed {
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
