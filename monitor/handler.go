package monitor

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"binaryDeploy/processmanager"
)

// ServerConfig represents the server configuration for the monitor
type ServerConfig struct {
	Port              string   `json:"port"`
	Secret            string   `json:"secret"`
	TargetRepoURL     string   `json:"target_repo_url"`
	SelfUpdateRepoURL string   `json:"self_update_repo_url"`
	DeployDir         string   `json:"deploy_dir"`
	SelfUpdateDir     string   `json:"self_update_dir"`
	AllowedBranches   []string `json:"allowed_branches"`
	LogFile           string   `json:"log_file"`
}

// Handler handles HTTP requests for the web monitoring interface
type Handler struct {
	processManager *processmanager.ProcessManager
	serverConfig   *ServerConfig
}

// NewHandler creates a new monitor handler
func NewHandler(pm *processmanager.ProcessManager, serverConfig *ServerConfig) *Handler {
	return &Handler{
		processManager: pm,
		serverConfig:   serverConfig,
	}
}

// RegisterRoutes registers monitoring routes with the given mux
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/status", h.statusHandler)
	mux.HandleFunc("/monitor", h.monitorHandler)
}

// statusHandler returns JSON with current system status
func (h *Handler) statusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	status := map[string]interface{}{
		"server": map[string]interface{}{
			"port":             h.serverConfig.Port,
			"target_repo":      h.serverConfig.TargetRepoURL,
			"self_update_repo": h.serverConfig.SelfUpdateRepoURL,
			"allowed_branches": h.serverConfig.AllowedBranches,
		},
		"process":   h.processManager.GetWebStatus(),
		"timestamp": time.Now().Format(time.RFC3339),
	}

	json.NewEncoder(w).Encode(status)
}

// monitorHandler serves the HTML monitoring dashboard
func (h *Handler) monitorHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html")

	html := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Binary Deploy Monitor</title>
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@300;400;500;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --primary-color: #2563eb;
            --primary-hover: #1d4ed8;
            --success-color: #10b981;
            --danger-color: #ef4444;
            --warning-color: #f59e0b;
            --bg-color: #f8fafc;
            --card-bg: #ffffff;
            --text-primary: #1e293b;
            --text-secondary: #64748b;
            --text-muted: #94a3b8;
            --border-color: #e2e8f0;
            --shadow-sm: 0 1px 2px 0 rgb(0 0 0 / 0.05);
            --shadow-md: 0 4px 6px -1px rgb(0 0 0 / 0.1);
            --shadow-lg: 0 10px 15px -3px rgb(0 0 0 / 0.1);
            --radius-sm: 0.375rem;
            --radius-md: 0.5rem;
            --radius-lg: 0.75rem;
        }

        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }

        body {
            font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
            background: linear-gradient(135deg, #f8fafc 0%, #f1f5f9 100%);
            color: var(--text-primary);
            line-height: 1.6;
            min-height: 100vh;
        }

        .container {
            max-width: 1280px;
            margin: 0 auto;
            padding: 2rem;
        }

        .header {
            background: var(--card-bg);
            padding: 2rem;
            border-radius: var(--radius-lg);
            margin-bottom: 2rem;
            box-shadow: var(--shadow-md);
            border: 1px solid var(--border-color);
            position: relative;
            overflow: hidden;
        }

        .header::before {
            content: '';
            position: absolute;
            top: 0;
            left: 0;
            right: 0;
            height: 4px;
            background: linear-gradient(90deg, var(--primary-color), #3b82f6);
        }

        .header-content {
            display: flex;
            align-items: center;
            justify-content: space-between;
            flex-wrap: wrap;
            gap: 1rem;
        }

        .title-section {
            display: flex;
            align-items: center;
            gap: 1rem;
        }

        .logo {
            width: 48px;
            height: 48px;
            background: linear-gradient(135deg, var(--primary-color), #3b82f6);
            border-radius: var(--radius-md);
            display: flex;
            align-items: center;
            justify-content: center;
            color: white;
            font-size: 1.5rem;
            font-weight: 600;
        }

        h1 {
            font-size: 2rem;
            font-weight: 700;
            color: var(--text-primary);
            margin: 0;
        }

        .subtitle {
            color: var(--text-secondary);
            font-size: 0.875rem;
            font-weight: 500;
            margin-top: 0.25rem;
        }

        .header-actions {
            display: flex;
            align-items: center;
            gap: 1rem;
        }

        .refresh-btn {
            background: var(--primary-color);
            color: white;
            border: none;
            padding: 0.75rem 1.5rem;
            border-radius: var(--radius-md);
            cursor: pointer;
            font-weight: 500;
            font-size: 0.875rem;
            transition: all 0.2s ease;
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .refresh-btn:hover {
            background: var(--primary-hover);
            transform: translateY(-1px);
            box-shadow: var(--shadow-md);
        }

        .refresh-btn:active {
            transform: translateY(0);
        }

        .refresh-icon {
            display: inline-block;
            width: 16px;
            height: 16px;
            border: 2px solid currentColor;
            border-top-color: transparent;
            border-radius: 50%;
            animation: spin 1s linear infinite;
        }

        .refresh-btn.loading .refresh-icon {
            animation: spin 1s linear infinite;
        }

        @keyframes spin {
            to { transform: rotate(360deg); }
        }

        .last-update {
            color: var(--text-muted);
            font-size: 0.75rem;
            font-weight: 500;
        }

        .status-grid {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(400px, 1fr));
            gap: 1.5rem;
            margin-bottom: 2rem;
        }

        .card {
            background: var(--card-bg);
            border-radius: var(--radius-lg);
            box-shadow: var(--shadow-md);
            border: 1px solid var(--border-color);
            overflow: hidden;
            transition: transform 0.2s ease, box-shadow 0.2s ease;
        }

        .card:hover {
            transform: translateY(-2px);
            box-shadow: var(--shadow-lg);
        }

        .card-header {
            padding: 1.5rem;
            border-bottom: 1px solid var(--border-color);
            background: linear-gradient(to bottom, #f8fafc, #ffffff);
        }

        .card-title {
            font-size: 1.125rem;
            font-weight: 600;
            color: var(--text-primary);
            display: flex;
            align-items: center;
            gap: 0.5rem;
        }

        .card-icon {
            font-size: 1.25rem;
        }

        .card-body {
            padding: 1.5rem;
        }

        .status-grid-item {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 0.75rem 0;
            border-bottom: 1px solid var(--border-color);
        }

        .status-grid-item:last-child {
            border-bottom: none;
        }

        .status-label {
            font-weight: 500;
            color: var(--text-secondary);
            font-size: 0.875rem;
        }

        .status-value {
            font-weight: 600;
            color: var(--text-primary);
            font-size: 0.875rem;
            text-align: right;
            max-width: 60%;
            word-break: break-all;
        }

        .status-badge {
            display: inline-flex;
            align-items: center;
            gap: 0.5rem;
            padding: 0.375rem 0.75rem;
            border-radius: var(--radius-sm);
            font-weight: 500;
            font-size: 0.75rem;
            text-transform: uppercase;
            letter-spacing: 0.05em;
        }

        .status-badge.running {
            background: rgba(16, 185, 129, 0.1);
            color: var(--success-color);
            border: 1px solid rgba(16, 185, 129, 0.2);
        }

        .status-badge.stopped {
            background: rgba(239, 68, 68, 0.1);
            color: var(--danger-color);
            border: 1px solid rgba(239, 68, 68, 0.2);
        }

        .status-indicator {
            width: 8px;
            height: 8px;
            border-radius: 50%;
            display: inline-block;
        }

        .status-indicator.running {
            background: var(--success-color);
            box-shadow: 0 0 0 2px rgba(16, 185, 129, 0.2);
        }

        .status-indicator.stopped {
            background: var(--danger-color);
            box-shadow: 0 0 0 2px rgba(239, 68, 68, 0.2);
        }

        .config-grid {
            display: grid;
            gap: 1rem;
        }

        .config-item {
            display: flex;
            justify-content: space-between;
            align-items: flex-start;
            padding: 1rem;
            background: var(--bg-color);
            border-radius: var(--radius-md);
            border: 1px solid var(--border-color);
        }

        .config-key {
            font-weight: 600;
            color: var(--text-primary);
            font-size: 0.875rem;
            min-width: 140px;
        }

        .config-value {
            color: var(--text-secondary);
            font-size: 0.875rem;
            flex: 1;
            text-align: right;
            word-break: break-all;
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

        @media (max-width: 768px) {
            .container {
                padding: 1rem;
            }
            
            .header-content {
                flex-direction: column;
                align-items: flex-start;
            }
            
            .status-grid {
                grid-template-columns: 1fr;
            }
            
            .status-value {
                max-width: 100%;
                margin-top: 0.25rem;
                text-align: left;
            }
            
            .status-grid-item {
                flex-direction: column;
                align-items: flex-start;
            }
            
            .config-item {
                flex-direction: column;
                gap: 0.5rem;
            }
            
            .config-value {
                text-align: left;
            }
        }

        /* Loading animation */
        .skeleton {
            background: linear-gradient(90deg, #f0f0f0 25%, #e0e0e0 50%, #f0f0f0 75%);
            background-size: 200% 100%;
            animation: loading 1.5s infinite;
        }

        @keyframes loading {
            0% { background-position: 200% 0; }
            100% { background-position: -200% 0; }
        }
    </style>
</head>
<body>
    <div class="container">
        <header class="header">
            <div class="header-content">
                <div class="title-section">
                    <div class="logo">🚀</div>
                    <div>
                        <h1>Binary Deploy Monitor</h1>
                        <div class="subtitle">Real-time deployment and process monitoring</div>
                    </div>
                </div>
                <div class="header-actions">
                    <button class="refresh-btn" onclick="loadStatus()" id="refreshBtn">
                        <span class="refresh-icon"></span>
                        <span>Refresh</span>
                    </button>
                    <div class="last-update" id="last-update">Loading...</div>
                </div>
            </div>
        </header>
        
        <div class="status-grid">
            <div class="card">
                <div class="card-header">
                    <h2 class="card-title">
                        <span class="card-icon">📡</span>
                        Server Status
                    </h2>
                </div>
                <div class="card-body">
                    <div class="status-grid-item">
                        <span class="status-label">Port</span>
                        <span class="status-value" id="server-port">-</span>
                    </div>
                    <div class="status-grid-item">
                        <span class="status-label">Target Repository</span>
                        <span class="status-value" id="target-repo">-</span>
                    </div>
                    <div class="status-grid-item">
                        <span class="status-label">Self-Update Repository</span>
                        <span class="status-value" id="self-update-repo">-</span>
                    </div>
                    <div class="status-grid-item">
                        <span class="status-label">Allowed Branches</span>
                        <span class="status-value" id="allowed-branches">-</span>
                    </div>
                </div>
            </div>
            
            <div class="card">
                <div class="card-header">
                    <h2 class="card-title">
                        <span class="card-icon">⚡</span>
                        Process Status
                    </h2>
                </div>
                <div class="card-body">
                    <div class="status-grid-item">
                        <span class="status-label">Status</span>
                        <span class="status-value" id="process-status">
                            <span class="status-badge stopped">
                                <span class="status-indicator stopped"></span>
                                Stopped
                            </span>
                        </span>
                    </div>
                    <div class="status-grid-item">
                        <span class="status-label">Process ID</span>
                        <span class="status-value" id="process-pid">-</span>
                    </div>
                    <div class="status-grid-item">
                        <span class="status-label">Uptime</span>
                        <span class="status-value" id="process-uptime">-</span>
                    </div>
                    <div class="status-grid-item">
                        <span class="status-label">Restart Count</span>
                        <span class="status-value" id="restart-count">-</span>
                    </div>
                    <div class="status-grid-item">
                        <span class="status-label">Command</span>
                        <span class="status-value" id="process-command">-</span>
                    </div>
                    <div class="status-grid-item">
                        <span class="status-label">Working Directory</span>
                        <span class="status-value" id="working-dir">-</span>
                    </div>
                </div>
            </div>
        </div>
        
        <div class="card">
            <div class="card-header">
                <h2 class="card-title">
                    <span class="card-icon">⚙️</span>
                    Process Configuration
                </h2>
            </div>
            <div class="card-body" id="process-config">
                <div class="empty-state">
                    <div class="empty-state-icon">🚫</div>
                    <div class="empty-state-text">No process running</div>
                    <div class="empty-state-subtext">Deploy an application to see configuration details</div>
                </div>
            </div>
        </div>
    </div>

    <script>
        function loadStatus() {
            const refreshBtn = document.getElementById('refreshBtn');
            refreshBtn.classList.add('loading');
            
            fetch('/status')
                .then(response => response.json())
                .then(data => {
                    updateServerInfo(data.server);
                    updateProcessInfo(data.process);
                    document.getElementById('last-update').textContent = 'Last updated: ' + new Date(data.timestamp).toLocaleTimeString();
                })
                .catch(error => {
                    console.error('Error fetching status:', error);
                    document.getElementById('last-update').textContent = 'Error loading data';
                })
                .finally(() => {
                    refreshBtn.classList.remove('loading');
                });
        }
        
        function updateServerInfo(server) {
            document.getElementById('server-port').textContent = server.port;
            document.getElementById('target-repo').textContent = server.target_repo || 'Not configured';
            document.getElementById('self-update-repo').textContent = server.self_update_repo || 'Not configured';
            document.getElementById('allowed-branches').textContent = server.allowed_branches ? server.allowed_branches.join(', ') : 'All branches';
        }
        
        function updateProcessInfo(process) {
            const statusElement = document.getElementById('process-status');
            
            if (process.running) {
                statusElement.innerHTML = '<span class="status-badge running"><span class="status-indicator running"></span>Running</span>';
                document.getElementById('process-pid').textContent = process.pid;
                document.getElementById('process-uptime').textContent = process.uptime;
                document.getElementById('restart-count').textContent = process.restart_count;
                document.getElementById('process-command').textContent = process.command;
                document.getElementById('working-dir').textContent = process.working_dir;
                
                const config = process.config;
                let configHtml = '<div class="config-grid">' +
                    '<div class="config-item">' +
                        '<span class="config-key">Build Command</span>' +
                        '<span class="config-value">' + (config.build_command || 'N/A') + '</span>' +
                    '</div>' +
                    '<div class="config-item">' +
                        '<span class="config-key">Run Command</span>' +
                        '<span class="config-value">' + (config.run_command || 'N/A') + '</span>' +
                    '</div>' +
                    '<div class="config-item">' +
                        '<span class="config-key">Working Directory</span>' +
                        '<span class="config-value">' + (config.working_dir || 'N/A') + '</span>' +
                    '</div>' +
                    '<div class="config-item">' +
                        '<span class="config-key">Environment</span>' +
                        '<span class="config-value">' + (config.environment || 'N/A') + '</span>' +
                    '</div>' +
                    '<div class="config-item">' +
                        '<span class="config-key">Max Restarts</span>' +
                        '<span class="config-value">' + (config.max_restarts || 0) + '</span>' +
                    '</div>' +
                    '<div class="config-item">' +
                        '<span class="config-key">Restart Delay</span>' +
                        '<span class="config-value">' + (config.restart_delay || 0) + 's</span>' +
                    '</div>' +
                '</div>';
                document.getElementById('process-config').innerHTML = configHtml;
            } else {
                statusElement.innerHTML = '<span class="status-badge stopped"><span class="status-indicator stopped"></span>Stopped</span>';
                document.getElementById('process-pid').textContent = '-';
                document.getElementById('process-uptime').textContent = '-';
                document.getElementById('restart-count').textContent = '0';
                document.getElementById('process-command').textContent = '-';
                document.getElementById('working-dir').textContent = '-';
                document.getElementById('process-config').innerHTML = 
                    '<div class="empty-state">' +
                        '<div class="empty-state-icon">🚫</div>' +
                        '<div class="empty-state-text">No process running</div>' +
                        '<div class="empty-state-subtext">Deploy an application to see configuration details</div>' +
                    '</div>';
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
