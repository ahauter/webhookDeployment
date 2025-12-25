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
	"sync"
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

type UpdateStatus struct {
	IsRunning   bool      `json:"is_running"`
	StartTime   time.Time `json:"start_time"`
	Message     string    `json:"message"`
	Error       string    `json:"error,omitempty"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

var (
	appConfig      *config.DeployConfig
	processManager *processmanager.ProcessManager
	updateStatus   = struct {
		sync.RWMutex
		target UpdateStatus `json:"target"`
		self   UpdateStatus `json:"self"`
	}{
		target: UpdateStatus{IsRunning: false},
		self:   UpdateStatus{IsRunning: false},
	}
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

	// Auto-start target app after server initialization
	go func() {
		// Give server a moment to start up
		time.Sleep(3 * time.Second)

		slog.Info("Auto-starting target application", "repo", appConfig.TargetRepoURL)
		if err := deployTargetRepo(appConfig.TargetRepoURL); err != nil {
			slog.Error("Auto-start deployment failed", "error", err)
		} else {
			slog.Info("Target application auto-started successfully")
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

	// Create base JSON handler for file logging
	baseHandler := slog.NewJSONHandler(logFile, nil)

	// Wrap with streaming handler for real-time logs
	globalLogStreamer = NewLogStreamer(baseHandler, appConfig.LogBufferSize)

	logger := slog.New(globalLogStreamer)
	slog.SetDefault(logger)
}

func loadConfig() {
	configFile := "deploy.config"
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: deploy.config file not found\n")
		fmt.Fprintf(os.Stderr, "Please create a deploy.config file with your application and binary configuration.\n")
		fmt.Fprintf(os.Stderr, "\nExample deploy.config:\n")
		fmt.Fprintf(os.Stderr, "# Application Configuration (required)\n")
		fmt.Fprintf(os.Stderr, "target_repo_url=https://github.com/user/myapp.git\n")
		fmt.Fprintf(os.Stderr, "allowed_branches=main\n")
		fmt.Fprintf(os.Stderr, "secret=your-webhook-secret-here\n")
		fmt.Fprintf(os.Stderr, "\n# Application Deployment Settings\n")
		fmt.Fprintf(os.Stderr, "build_command=go build -o myapp .\n")
		fmt.Fprintf(os.Stderr, "run_command=./myapp\n")
		fmt.Fprintf(os.Stderr, "\n# BinaryDeploy Configuration (optional)\n")
		fmt.Fprintf(os.Stderr, "# port=8080\n")
		fmt.Fprintf(os.Stderr, "# log_file=./binaryDeploy.log\n")
		fmt.Fprintf(os.Stderr, "# self_update_repo_url=https://github.com/ahauter/binaryDeploy-updater.git\n")
		os.Exit(1)
	}

	// Load configuration using the config package
	deployConfig, err := config.LoadDeployConfig(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading deploy.config: %v\n", err)
		os.Exit(1)
	}

	// Validate required fields
	if err := config.ValidateConfig(deployConfig); err != nil {
		fmt.Fprintf(os.Stderr, "Configuration validation failed: %v\n", err)
		os.Exit(1)
	}

	// Show warnings for any default values being used
	warnings := config.GetDefaultWarnings(deployConfig)
	for _, warning := range warnings {
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
	}

	appConfig = deployConfig
}

func logsOnlyHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	// Full-screen logs page HTML
	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Binary Deploy - Live Logs</title>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&family=JetBrains+Mono:wght@400;500;600&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-color: #0d1117;
            --card-bg: #161b22;
            --border-color: #30363d;
            --text-primary: #e6edf3;
            --text-secondary: #8b949e;
            --text-muted: #656d76;
        }

        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
            background: var(--bg-color);
            color: var(--text-primary);
            height: 100vh;
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }

        .header {
            background: var(--card-bg);
            border-bottom: 1px solid var(--border-color);
            padding: 1rem 2rem;
            display: flex;
            justify-content: space-between;
            align-items: center;
            flex-shrink: 0;
        }

        .header-title {
            display: flex;
            align-items: center;
            gap: 1rem;
            font-size: 1.25rem;
            font-weight: 600;
        }

        .header-controls {
            display: flex;
            gap: 1rem;
            align-items: center;
        }

        .btn {
            background: var(--card-bg);
            color: var(--text-primary);
            border: 1px solid var(--border-color);
            padding: 0.5rem 1rem;
            border-radius: 0.375rem;
            cursor: pointer;
            font-weight: 500;
            font-size: 0.875rem;
            transition: all 0.2s ease;
            display: flex;
            align-items: center;
            gap: 0.5rem;
            text-decoration: none;
        }

        .btn:hover {
            background: #21262d;
            transform: translateY(-1px);
        }

        .btn:active {
            transform: translateY(0);
        }

        .log-status {
            font-size: 0.875rem;
            font-weight: 500;
        }

        .log-container-wrapper {
            flex: 1;
            display: flex;
            flex-direction: column;
            padding: 1rem;
            overflow: hidden;
        }

        .log-container {
            background: #0d1117;
            color: #e6edf3;
            font-family: 'JetBrains Mono', 'Fira Code', 'Consolas', 'Monaco', 'Courier New', monospace;
            font-size: 0.8rem;
            flex: 1;
            overflow-y: auto;
            padding: 1rem;
            border-radius: 0.375rem;
            line-height: 1.6;
            border: 1px solid var(--border-color);
        }

        .log-entry {
            margin-bottom: 0.5rem;
            padding: 0.5rem;
            border-radius: 0.375rem;
            word-break: break-all;
            position: relative;
            transition: all 0.2s ease;
            border-left: 3px solid transparent;
            animation: logFadeIn 0.3s ease-in-out;
        }

        @keyframes logFadeIn {
            from {
                opacity: 0;
                transform: translateY(-10px);
            }
            to {
                opacity: 1;
                transform: translateY(0);
            }
        }

        .log-entry:hover {
            background: rgba(255, 255, 255, 0.05);
            transform: translateX(2px);
        }

        .log-entry.error {
            background: linear-gradient(135deg, rgba(239, 68, 68, 0.15), rgba(239, 68, 68, 0.05));
            border-left-color: #ef4444;
            color: #fca5a5;
        }

        .log-entry.error .log-timestamp,
        .log-entry.error .log-level {
            color: #fca5a5 !important;
        }

        .log-entry.warn {
            background: linear-gradient(135deg, rgba(245, 158, 11, 0.15), rgba(245, 158, 11, 0.05));
            border-left-color: #f59e0b;
            color: #fcd34d;
        }

        .log-entry.warn .log-timestamp,
        .log-entry.warn .log-level {
            color: #fcd34d !important;
        }

        .log-entry.info {
            background: linear-gradient(135deg, rgba(59, 130, 246, 0.15), rgba(59, 130, 246, 0.05));
            border-left-color: #3b82f6;
            color: #93c5fd;
        }

        .log-entry.info .log-timestamp,
        .log-entry.info .log-level {
            color: #93c5fd !important;
        }

        .log-entry.debug {
            background: linear-gradient(135deg, rgba(139, 92, 246, 0.15), rgba(139, 92, 246, 0.05));
            border-left-color: #8b5cf6;
            color: #c4b5fd;
        }

        .log-entry.debug .log-timestamp,
        .log-entry.debug .log-level {
            color: #c4b5fd !important;
        }

        .log-timestamp {
            color: #8b949e;
            font-size: 0.75rem;
            font-weight: 500;
            text-transform: uppercase;
            letter-spacing: 0.05em;
            margin-right: 0.75rem;
        }

        .log-level {
            font-weight: 600;
            font-size: 0.8rem;
            padding: 0.125rem 0.5rem;
            border-radius: 0.375rem;
            margin-right: 0.75rem;
            text-transform: uppercase;
            letter-spacing: 0.05em;
        }

        .log-message {
            color: #e6edf3;
            font-weight: 400;
        }

        .log-fields {
            margin-top: 0.25rem;
            font-size: 0.8rem;
            color: #8b949e;
            font-style: italic;
        }

        .log-field {
            margin-right: 1rem;
        }

        .log-field-key {
            color: #f97316;
            font-weight: 500;
        }

        .log-field-value {
            color: #10b981;
        }

        .log-container::-webkit-scrollbar {
            width: 8px;
        }

        .log-container::-webkit-scrollbar-track {
            background: #21262d;
            border-radius: 0.375rem;
        }

        .log-container::-webkit-scrollbar-thumb {
            background: #30363d;
            border-radius: 0.375rem;
            border: 1px solid #21262d;
        }

        .log-container::-webkit-scrollbar-thumb:hover {
            background: #484f58;
        }

        .empty-state {
            text-align: center;
            padding: 3rem 1rem;
            color: var(--text-muted);
        }

        .empty-state-icon {
            font-size: 3rem;
            margin-bottom: 1rem;
            opacity: 0.5;
        }

        .empty-state-text {
            font-weight: 500;
            margin-bottom: 0.5rem;
        }

        .empty-state-subtext {
            font-size: 0.875rem;
            opacity: 0.7;
        }

        .connecting {
            animation: pulse 1.5s infinite;
        }

        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }

        .error {
            animation: blink 2s infinite;
        }

        @keyframes blink {
            0%, 50%, 100% { opacity: 1; }
            25%, 75% { opacity: 0.3; }
        }
    </style>
</head>
<body>
    <header class="header">
        <div class="header-title">
            <span>📋</span>
            <span>Binary Deploy - Live Logs</span>
            <span class="log-status" id="log-status">🟡 Connecting...</span>
        </div>
        <div class="header-controls">
            <button class="btn" onclick="toggleLogStream()" id="logToggleBtn">
                <span>⏸️</span>
                <span>Pause</span>
            </button>
            <button class="btn" onclick="clearLogs()" id="logClearBtn">
                <span>🗑️</span>
                <span>Clear</span>
            </button>
            <a href="/monitor" class="btn" target="_blank">
                <span>🔙</span>
                <span>Dashboard</span>
            </a>
        </div>
    </header>

    <div class="log-container-wrapper">
        <div class="log-container" id="log-container">
            <div class="empty-state">
                <div class="empty-state-icon">⏳</div>
                <div class="empty-state-text">Connecting to log stream...</div>
                <div class="empty-state-subtext">Real-time logs will appear here</div>
            </div>
        </div>
    </div>

    <script>
        let eventSource;
        let isLogStreamActive = true;
        let logEntryCount = 0;
        let maxLogEntries = 1000;

        function connectLogStream() {
            const statusElement = document.getElementById('log-status');
            statusElement.textContent = '🟡 Connecting...';
            statusElement.className = 'log-status connecting';

            eventSource = new EventSource('/logs');
            
            eventSource.onopen = function() {
                statusElement.textContent = '🟢 Connected';
                statusElement.className = 'log-status';
                console.log('Log stream connected');
            };
            
            eventSource.onmessage = function(event) {
                try {
                    const logEntry = JSON.parse(event.data);
                    if (isLogStreamActive) {
                        appendLogEntry(logEntry);
                    }
                } catch (error) {
                    console.error('Error parsing log entry:', error, event.data);
                }
            };
            
            eventSource.onerror = function() {
                statusElement.textContent = '🔴 Disconnected';
                statusElement.className = 'log-status error';
                console.error('Log stream disconnected, attempting to reconnect...');
                
                setTimeout(() => {
                    connectLogStream();
                }, 5000);
            };
        }

        function appendLogEntry(logEntry) {
            const container = document.getElementById('log-container');
            
            if (logEntryCount === 0) {
                container.innerHTML = '';
            }

            const entry = document.createElement('div');
            entry.className = 'log-entry ' + logEntry.level.toLowerCase();
            
            const timestamp = new Date(logEntry.timestamp).toLocaleTimeString();
            
            let logHTML = '<span class="log-timestamp">' + timestamp + '</span>' +
                '<span class="log-level" style="background-color: ' + logEntry.color + '20; color: ' + logEntry.color + '; border: 1px solid ' + logEntry.color + '40;">' + logEntry.level + '</span>' +
                '<span class="log-message">' + logEntry.message + '</span>';

            if (logEntry.fields && Object.keys(logEntry.fields).length > 0) {
                const fieldParts = [];
                for (const [key, value] of Object.entries(logEntry.fields)) {
                    fieldParts.push('<span class="log-field"><span class="log-field-key">' + key + '</span>=<span class="log-field-value">' + value + '</span></span>');
                }
                logHTML += '<div class="log-fields">' + fieldParts.join(' ') + '</div>';
            }

            entry.innerHTML = logHTML;
            container.appendChild(entry);
            logEntryCount++;

            while (container.children.length > maxLogEntries) {
                container.removeChild(container.firstChild);
            }

            container.scrollTop = container.scrollHeight;

            if (logEntry.level === 'ERROR') {
                entry.style.animation = 'pulse 2s';
            }
        }

        function toggleLogStream() {
            isLogStreamActive = !isLogStreamActive;
            const btn = document.getElementById('logToggleBtn');
            
            if (isLogStreamActive) {
                btn.innerHTML = '<span>⏸️</span><span>Pause</span>';
            } else {
                btn.innerHTML = '<span>▶️</span><span>Resume</span>';
            }
        }

        function clearLogs() {
            const container = document.getElementById('log-container');
            container.innerHTML = '<div class="empty-state">' +
                '<div class="empty-state-icon">🗑️</div>' +
                '<div class="empty-state-text">Logs cleared</div>' +
                '<div class="empty-state-subtext">New logs will appear here</div>' +
                '</div>';
            logEntryCount = 0;
        }

        // Initialize
        connectLogStream();

        // Keyboard shortcut for pause/resume
        document.addEventListener('keydown', (e) => {
            if (e.code === 'Space') {
                e.preventDefault();
                toggleLogStream();
            }
        });
    </script>
</body>
</html>`

	fmt.Fprintf(w, html)
}

func setupRoutes() http.Handler {
	mux := http.NewServeMux()

	// Convert comma-separated branches to array for monitor
	allowedBranches := strings.Split(appConfig.AllowedBranches, ",")
	for i, branch := range allowedBranches {
		allowedBranches[i] = strings.TrimSpace(branch)
	}

	// Create monitor handler with server config
	serverConfig := &monitor.ServerConfig{
		Port:              appConfig.Port,
		Secret:            appConfig.Secret,
		TargetRepoURL:     appConfig.TargetRepoURL,
		SelfUpdateRepoURL: appConfig.SelfUpdateRepoURL,
		DeployDir:         appConfig.DeployDir,
		SelfUpdateDir:     appConfig.SelfUpdateDir,
		AllowedBranches:   allowedBranches,
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
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Force update target app endpoint
	mux.HandleFunc("/update-target", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			// Mark update as starting
			updateStatus.Lock()
			updateStatus.target = UpdateStatus{
				IsRunning: true,
				StartTime: time.Now(),
				Message:   "Target app update started",
			}
			updateStatus.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{
				"status":    "Target app update started",
				"timestamp": time.Now().Format(time.RFC3339),
			})

			// Run deployment asynchronously
			go func() {
				if err := deployTargetRepo(appConfig.TargetRepoURL); err != nil {
					slog.Error("Manual target app update failed", "error", err)
					updateStatus.Lock()
					updateStatus.target.IsRunning = false
					updateStatus.target.Error = err.Error()
					updateStatus.target.Message = "Target app update failed"
					updateStatus.target.CompletedAt = time.Now()
					updateStatus.Unlock()
				} else {
					slog.Info("Manual target app update completed successfully")
					updateStatus.Lock()
					updateStatus.target.IsRunning = false
					updateStatus.target.Message = "Target app update completed successfully"
					updateStatus.target.CompletedAt = time.Now()
					updateStatus.Unlock()
				}
			}()
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Update status endpoint
	mux.HandleFunc("/update-status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			updateStatus.RLock()
			status := map[string]interface{}{
				"target": updateStatus.target,
				"self":   updateStatus.self,
			}
			updateStatus.RUnlock()
			json.NewEncoder(w).Encode(status)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Force update self endpoint
	mux.HandleFunc("/update-self", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			// Mark update as starting
			updateStatus.Lock()
			updateStatus.self = UpdateStatus{
				IsRunning: true,
				StartTime: time.Now(),
				Message:   "Self update started",
			}
			updateStatus.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{
				"status":    "Self update started",
				"timestamp": time.Now().Format(time.RFC3339),
			})

			// Run self update asynchronously
			go func() {
				if err := deploySelfUpdate(); err != nil {
					slog.Error("Manual self update failed", "error", err)
					updateStatus.Lock()
					updateStatus.self.IsRunning = false
					updateStatus.self.Error = err.Error()
					updateStatus.self.Message = "Self update failed"
					updateStatus.self.CompletedAt = time.Now()
					updateStatus.Unlock()
				} else {
					slog.Info("Manual self update completed successfully")
					updateStatus.Lock()
					updateStatus.self.IsRunning = false
					updateStatus.self.Message = "Self update completed successfully"
					updateStatus.self.CompletedAt = time.Now()
					updateStatus.Unlock()
				}
			}()
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// SSE endpoint for real-time log streaming
	mux.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		// Set SSE headers
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Cache-Control")

		// Get flusher for SSE
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Create client channel
		clientChan := make(chan []byte, 100)
		globalLogStreamer.AddClient(clientChan)
		defer globalLogStreamer.RemoveClient(clientChan)

		// Send buffered logs first
		for _, logEntry := range globalLogStreamer.GetBufferedLogs() {
			fmt.Fprintf(w, "data: %s\n\n", logEntry)
			flusher.Flush()
		}

		// Stream new logs
		for {
			select {
			case logEntry := <-clientChan:
				fmt.Fprintf(w, "data: %s\n\n", logEntry)
				flusher.Flush()
			case <-r.Context().Done():
				return
			}
		}
	})

	// Logs-only page endpoint
	mux.HandleFunc("/logs-only", logsOnlyHandler)

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
	// Only require signature if secret is configured
	if appConfig.Secret != "" && signature == "" {
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

	// Check if this is a self-update deployment
	if payload.Repository.URL == appConfig.SelfUpdateRepoURL {
		// Mark self-update as starting
		updateStatus.Lock()
		updateStatus.self = UpdateStatus{
			IsRunning: true,
			StartTime: time.Now(),
			Message:   fmt.Sprintf("Webhook self-update triggered for %s", payload.Repository.Name),
		}
		updateStatus.Unlock()

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Self-update deployment triggered for %s", payload.Repository.Name)
		go func() {
			if err := deploySelfUpdate(); err != nil {
				slog.Error("Self-update deployment failed", "error", err)
				updateStatus.Lock()
				updateStatus.self.IsRunning = false
				updateStatus.self.Error = err.Error()
				updateStatus.self.Message = "Webhook self-update failed"
				updateStatus.self.CompletedAt = time.Now()
				updateStatus.Unlock()
			} else {
				slog.Info("Self-update deployment completed successfully")
				updateStatus.Lock()
				updateStatus.self.IsRunning = false
				updateStatus.self.Message = "Webhook self-update completed successfully"
				updateStatus.self.CompletedAt = time.Now()
				updateStatus.Unlock()
			}
		}()
	} else {
		// Mark target update as starting
		updateStatus.Lock()
		updateStatus.target = UpdateStatus{
			IsRunning: true,
			StartTime: time.Now(),
			Message:   fmt.Sprintf("Webhook deployment triggered for %s", payload.Repository.Name),
		}
		updateStatus.Unlock()

		// Deploy any repository (repo-agnostic approach)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Deployment triggered for %s", payload.Repository.Name)
		go func() {
			if err := deployTargetRepo(payload.Repository.URL); err != nil {
				slog.Error("Target deployment failed", "error", err)
				updateStatus.Lock()
				updateStatus.target.IsRunning = false
				updateStatus.target.Error = err.Error()
				updateStatus.target.Message = "Webhook deployment failed"
				updateStatus.target.CompletedAt = time.Now()
				updateStatus.Unlock()
			} else {
				slog.Info("Target deployment completed successfully")
				updateStatus.Lock()
				updateStatus.target.IsRunning = false
				updateStatus.target.Message = "Webhook deployment completed successfully"
				updateStatus.target.CompletedAt = time.Now()
				updateStatus.Unlock()
			}
		}()
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
	allowedBranches := strings.Split(appConfig.AllowedBranches, ",")
	if len(allowedBranches) == 0 || (len(allowedBranches) == 1 && allowedBranches[0] == "") {
		return true
	}
	for _, allowed := range allowedBranches {
		allowed = strings.TrimSpace(allowed)
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

	// Use deploy config from main configuration (not from cloned repo)
	deployConfig := appConfig

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
