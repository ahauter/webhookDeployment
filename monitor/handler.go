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

        .action-btn {
            background: var(--card-bg);
            color: var(--text-primary);
            border: 1px solid var(--border-color);
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

        .action-btn:hover {
            background: var(--bg-color);
            transform: translateY(-1px);
            box-shadow: var(--shadow-md);
        }

        .action-btn:active {
            transform: translateY(0);
        }

        .action-btn.loading {
            opacity: 0.6;
            cursor: not-allowed;
        }

        .update-target-btn:hover {
            border-color: var(--success-color);
            color: var(--success-color);
        }

        .update-self-btn:hover {
            border-color: var(--warning-color);
            color: var(--warning-color);
        }

        .btn-icon {
            font-size: 1rem;
        }

        .action-btn.loading .btn-icon {
            animation: spin 1s linear infinite;
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

        .update-status-container {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(500px, 1fr));
            gap: 1.5rem;
            margin-bottom: 1.5rem;
        }

        .update-status-item {
            background: var(--card-bg);
            border-radius: var(--radius-md);
            padding: 1rem;
            border: 1px solid var(--border-color);
            box-shadow: var(--shadow-sm);
        }

        .update-status-label {
            font-weight: 600;
            color: var(--text-primary);
            margin-right: 0.5rem;
        }

        .update-message {
            margin-top: 0.5rem;
            font-size: 0.875rem;
            padding: 0.5rem;
            border-radius: var(--radius-sm);
        }

        .update-message.idle {
            color: var(--text-muted);
            background: var(--bg-color);
        }

        .update-message.updating {
            color: var(--warning-color);
            background: rgba(245, 158, 11, 0.1);
        }

        .update-message.success {
            color: var(--success-color);
            background: rgba(16, 185, 129, 0.1);
        }

        .update-message.error {
            color: var(--danger-color);
            background: rgba(239, 68, 68, 0.1);
        }

        .status-badge.updating {
            background: var(--warning-color);
            color: white;
        }

        .status-indicator.updating {
            background: white;
        }

        .status-badge.idle {
            background: var(--text-muted);
            color: white;
        }

        .status-indicator.idle {
            background: white;
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

        .status-badge.error {
            background: rgba(239, 68, 68, 0.1);
            color: var(--danger-color);
            border: 1px solid rgba(239, 68, 68, 0.2);
        }

        .status-badge.success {
            background: rgba(16, 185, 129, 0.1);
            color: var(--success-color);
            border: 1px solid rgba(16, 185, 129, 0.2);
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

        .status-indicator.error {
            background: var(--danger-color);
            box-shadow: 0 0 0 2px rgba(239, 68, 68, 0.2);
        }

        .status-indicator.success {
            background: var(--success-color);
            box-shadow: 0 0 0 2px rgba(16, 185, 129, 0.2);
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

        /* Notification styles */
        .notification {
            position: fixed;
            top: 20px;
            right: 20px;
            background: var(--card-bg);
            border: 1px solid var(--border-color);
            border-radius: var(--radius-md);
            box-shadow: var(--shadow-lg);
            z-index: 1000;
            transform: translateX(100%);
            transition: transform 0.3s ease;
            max-width: 400px;
            min-width: 300px;
        }

        .notification.show {
            transform: translateX(0);
        }

        .notification-content {
            padding: 1rem 1.5rem;
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }

        .notification-icon {
            font-size: 1.25rem;
            flex-shrink: 0;
        }

        .notification-message {
            font-weight: 500;
            font-size: 0.875rem;
            color: var(--text-primary);
        }

        .notification-success {
            border-left: 4px solid var(--success-color);
        }

        .notification-error {
            border-left: 4px solid var(--danger-color);
        }

        .notification-warning {
            border-left: 4px solid var(--warning-color);
        }

        .notification-info {
            border-left: 4px solid var(--primary-color);
        }

        /* Log Panel Styles */
        .log-header-content {
            display: flex;
            justify-content: space-between;
            align-items: center;
            width: 100%;
        }

        .log-controls {
            display: flex;
            gap: 0.5rem;
            align-items: center;
        }

        .log-status {
            font-size: 0.875rem;
            font-weight: 500;
            margin-left: 0.5rem;
        }

        .resize-handle {
            position: absolute;
            bottom: -8px;
            left: 50%;
            transform: translateX(-50%);
            width: 60px;
            height: 16px;
            background: var(--bg-color);
            border: 1px solid var(--border-color);
            border-radius: var(--radius-md);
            cursor: ns-resize;
            display: flex;
            align-items: center;
            justify-content: center;
            transition: all 0.2s ease;
        }

        .resize-handle:hover {
            background: var(--border-color);
            transform: translateX(-50%) scale(1.05);
        }

        .resize-dots {
            font-size: 0.75rem;
            color: var(--text-muted);
            letter-spacing: 2px;
        }

        .log-card-body {
            padding: 0;
            position: relative;
        }

        .log-container {
            background: #0d1117;
            color: #e6edf3;
            font-family: 'JetBrains Mono', 'Fira Code', 'Consolas', 'Monaco', 'Courier New', monospace;
            font-size: 0.8rem;
            height: 400px;
            overflow-y: auto;
            padding: 1rem;
            border-radius: var(--radius-md);
            position: relative;
            line-height: 1.6;
            resize: vertical;
            min-height: 200px;
            max-height: 80vh;
        }

        .log-entry {
            margin-bottom: 0.5rem;
            padding: 0.5rem;
            border-radius: var(--radius-sm);
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

        /* Beautiful log level colors */
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
            border-radius: var(--radius-sm);
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

        /* Custom scrollbar */
        .log-container::-webkit-scrollbar {
            width: 8px;
        }

        .log-container::-webkit-scrollbar-track {
            background: #21262d;
            border-radius: var(--radius-md);
        }

        .log-container::-webkit-scrollbar-thumb {
            background: #30363d;
            border-radius: var(--radius-md);
            border: 1px solid #21262d;
        }

        .log-container::-webkit-scrollbar-thumb:hover {
            background: #484f58;
        }

        /* Log container resizing */
        .log-container.resizing {
            outline: 2px solid var(--primary-color);
            outline-offset: 2px;
        }

        /* Pinned log entry (important messages) */
        .log-entry.pinned {
            background: linear-gradient(135deg, rgba(34, 197, 94, 0.15), rgba(34, 197, 94, 0.05));
            border-left-color: #22c55e;
            border-width: 4px;
        }

        /* Animated connection status */
        .log-status.connecting {
            animation: pulse 1.5s infinite;
        }

        @keyframes pulse {
            0%, 100% { opacity: 1; }
            50% { opacity: 0.5; }
        }

        .log-status.error {
            animation: blink 2s infinite;
        }

        @keyframes blink {
            0%, 50%, 100% { opacity: 1; }
            25%, 75% { opacity: 0.3; }
        }

        /* Mobile responsive */
        @media (max-width: 768px) {
            .log-header-content {
                flex-direction: column;
                align-items: flex-start;
                gap: 1rem;
            }

            .log-controls {
                width: 100%;
                justify-content: flex-start;
            }

            .log-container {
                height: 300px;
                font-size: 0.75rem;
            }

            .log-entry {
                margin-bottom: 0.25rem;
                padding: 0.375rem;
            }
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
                    <button class="action-btn update-target-btn" onclick="updateTargetApp()" id="updateTargetBtn">
                        <span class="btn-icon">🎯</span>
                        <span>Update Target App</span>
                    </button>
                    <button class="action-btn update-self-btn" onclick="updateSelf()" id="updateSelfBtn">
                        <span class="btn-icon">🔄</span>
                        <span>Update Self</span>
                    </button>
                    <button class="refresh-btn" onclick="loadStatus()" id="refreshBtn">
                        <span class="refresh-icon"></span>
                        <span>Refresh</span>
                    </button>
                    <div class="last-update" id="last-update">Loading...</div>
                </div>
            </div>
        </header>
        
        <!-- Update Status Displays -->
        <div class="update-status-container">
            <div class="update-status-item">
                <span class="update-status-label">Target App Update:</span>
                <span id="target-update-status">
                    <span class="status-badge idle">
                        <span class="status-indicator idle"></span>
                        Idle
                    </span>
                </span>
                <div id="target-update-message" class="update-message idle">No recent updates</div>
            </div>
            <div class="update-status-item">
                <span class="update-status-label">Self Update:</span>
                <span id="self-update-status">
                    <span class="status-badge idle">
                        <span class="status-indicator idle"></span>
                        Idle
                    </span>
                </span>
                <div id="self-update-message" class="update-message idle">No recent updates</div>
            </div>
        </div>
        
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
        
        <!-- Live Logs Panel -->
        <div class="card">
            <div class="card-header">
                <div class="log-header-content">
                    <h2 class="card-title">
                        <span class="card-icon">📋</span>
                        Live Logs
                        <span class="log-status" id="log-status">🟢 Connecting...</span>
                    </h2>
                    <div class="log-controls">
                        <button class="action-btn" onclick="toggleLogStream()" id="logToggleBtn">
                            <span class="btn-icon">⏸️</span>
                            <span>Pause</span>
                        </button>
                        <button class="action-btn" onclick="clearLogs()" id="logClearBtn">
                            <span class="btn-icon">🗑️</span>
                            <span>Clear</span>
                        </button>
                        <a href="/logs-only" class="action-btn" target="_blank">
                            <span class="btn-icon">🔗</span>
                            <span>Full Screen</span>
                        </a>
                    </div>
                </div>
                <div class="resize-handle" id="logResizeHandle">
                    <div class="resize-dots">⋮</div>
                </div>
            </div>
            <div class="card-body log-card-body">
                <div class="log-container" id="log-container">
                    <div class="empty-state">
                        <div class="empty-state-icon">⏳</div>
                        <div class="empty-state-text">Connecting to log stream...</div>
                        <div class="empty-state-subtext">Real-time logs will appear here</div>
                    </div>
                </div>
            </div>
        </div>
    </div>

    <script>
        // Log streaming variables
        let eventSource;
        let isLogStreamActive = true;
        let logEntryCount = 0;
        let maxLogEntries = 1000;

        function initializeLogStreaming() {
            connectLogStream();
            setupLogResizing();
        }

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
                
                // Auto-reconnect after 5 seconds
                setTimeout(() => {
                    connectLogStream();
                }, 5000);
            };
        }

        function appendLogEntry(logEntry) {
            const container = document.getElementById('log-container');
            
            // Remove empty state if this is the first log
            if (logEntryCount === 0) {
                container.innerHTML = '';
            }

            const entry = document.createElement('div');
            entry.className = 'log-entry ' + logEntry.level.toLowerCase();
            
            // Format timestamp
            const timestamp = new Date(logEntry.timestamp).toLocaleTimeString();
            
            // Build readable log entry
            let logHTML = '<span class="log-timestamp">' + timestamp + '</span>' +
                '<span class="log-level" style="background-color: ' + logEntry.color + '20; color: ' + logEntry.color + '; border: 1px solid ' + logEntry.color + '40;">' + logEntry.level + '</span>' +
                '<span class="log-message">' + logEntry.message + '</span>';

            // Add fields if present
            if (logEntry.fields && Object.keys(logEntry.fields).length > 0) {
                const fieldParts = [];
                for (const [key, value] of Object.entries(logEntry.fields)) {
                    fieldParts.push('<span class="log-field"><span class="log-field-key">' + key + '</span>=<span class="log-field-value">' + value + '</span></span>');
                }
                logHTML += '<div class="log-fields">' + fieldParts.join(' ') + '</div>';
            }

            entry.innerHTML = logHTML;
            
            // Add to container
            container.appendChild(entry);
            logEntryCount++;

            // Maintain max log entries
            while (container.children.length > maxLogEntries) {
                container.removeChild(container.firstChild);
            }

            // Auto-scroll to bottom
            container.scrollTop = container.scrollHeight;

            // Special handling for certain log levels
            if (logEntry.level === 'ERROR') {
                entry.classList.add('pinned');
                // Add visual emphasis
                setTimeout(() => {
                    entry.style.animation = 'pulse 2s';
                }, 100);
            }
        }

        function toggleLogStream() {
            isLogStreamActive = !isLogStreamActive;
            const btn = document.getElementById('logToggleBtn');
            
            if (isLogStreamActive) {
                btn.innerHTML = '<span class="btn-icon">⏸️</span><span>Pause</span>';
            } else {
                btn.innerHTML = '<span class="btn-icon">▶️</span><span>Resume</span>';
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

        function setupLogResizing() {
            const logContainer = document.getElementById('log-container');
            const resizeHandle = document.getElementById('logResizeHandle');
            let isResizing = false;
            let startY = 0;
            let startHeight = 0;

            resizeHandle.addEventListener('mousedown', (e) => {
                isResizing = true;
                startY = e.clientY;
                startHeight = logContainer.offsetHeight;
                logContainer.classList.add('resizing');
                document.body.style.cursor = 'ns-resize';
                e.preventDefault();
            });

            document.addEventListener('mousemove', (e) => {
                if (!isResizing) return;
                
                const deltaY = e.clientY - startY;
                const newHeight = Math.max(200, Math.min(window.innerHeight * 0.8, startHeight + deltaY));
                logContainer.style.height = newHeight + 'px';
            });

            document.addEventListener('mouseup', () => {
                if (isResizing) {
                    isResizing = false;
                    logContainer.classList.remove('resizing');
                    document.body.style.cursor = 'default';
                }
            });

            // Touch support for mobile
            resizeHandle.addEventListener('touchstart', (e) => {
                isResizing = true;
                startY = e.touches[0].clientY;
                startHeight = logContainer.offsetHeight;
                logContainer.classList.add('resizing');
                e.preventDefault();
            });

            document.addEventListener('touchmove', (e) => {
                if (!isResizing) return;
                
                const deltaY = e.touches[0].clientY - startY;
                const newHeight = Math.max(200, Math.min(window.innerHeight * 0.8, startHeight + deltaY));
                logContainer.style.height = newHeight + 'px';
            });

            document.addEventListener('touchend', () => {
                if (isResizing) {
                    isResizing = false;
                    logContainer.classList.remove('resizing');
                }
            });

            // Keyboard shortcut for pause/resume (spacebar)
            document.addEventListener('keydown', (e) => {
                if (e.code === 'Space' && e.target.tagName !== 'INPUT') {
                    e.preventDefault();
                    toggleLogStream();
                }
            });
        }

        function loadStatus() {
            const refreshBtn = document.getElementById('refreshBtn');
            refreshBtn.classList.add('loading');
            
            Promise.all([
                fetch('/status').then(response => response.json()),
                fetch('/update-status').then(response => response.json())
            ])
                .then(([statusData, updateData]) => {
                    updateServerInfo(statusData.server);
                    updateProcessInfo(statusData.process);
                    updateStatusInfo(updateData);
                    document.getElementById('last-update').textContent = 'Last updated: ' + new Date(statusData.timestamp).toLocaleTimeString();
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
        
        function updateStatusInfo(updateData) {
            // Update target app status
            const targetStatus = updateData.target;
            updateUpdateStatusDisplay('target', targetStatus);
            
            // Update self-update status  
            const selfStatus = updateData.self;
            updateUpdateStatusDisplay('self', selfStatus);
        }
        
        function updateUpdateStatusDisplay(type, status) {
            const statusElement = document.getElementById(type + '-update-status');
            const statusMessage = document.getElementById(type + '-update-message');
            
            if (statusElement && statusMessage) {
                if (status.is_running) {
                    statusElement.innerHTML = '<span class="status-badge updating"><span class="status-indicator updating"></span>Updating</span>';
                    statusMessage.textContent = status.message || 'Update in progress...';
                    statusMessage.className = 'update-message updating';
                } else if (status.error) {
                    statusElement.innerHTML = '<span class="status-badge error"><span class="status-indicator error"></span>Failed</span>';
                    statusMessage.textContent = status.error;
                    statusMessage.className = 'update-message error';
                } else if (status.completed_at) {
                    statusElement.innerHTML = '<span class="status-badge success"><span class="status-indicator success"></span>Completed</span>';
                    statusMessage.textContent = status.message || 'Update completed';
                    statusMessage.className = 'update-message success';
                } else {
                    statusElement.innerHTML = '<span class="status-badge idle"><span class="status-indicator idle"></span>Idle</span>';
                    statusMessage.textContent = 'No recent updates';
                    statusMessage.className = 'update-message idle';
                }
                
                // Add timestamp if available
                if (status.completed_at) {
                    const timeStr = new Date(status.completed_at).toLocaleString();
                    statusMessage.textContent += ' (' + timeStr + ')';
                } else if (status.start_time) {
                    const timeStr = new Date(status.start_time).toLocaleString();
                    statusMessage.textContent += ' (started ' + timeStr + ')';
                }
            }
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
        
        function updateTargetApp() {
            const btn = document.getElementById('updateTargetBtn');
            const originalContent = btn.innerHTML;
            
            btn.classList.add('loading');
            btn.disabled = true;
            btn.innerHTML = '<span class="btn-icon">⏳</span><span>Updating...</span>';
            
            fetch('/update-target', { method: 'POST' })
                .then(response => response.json())
                .then(data => {
                    showNotification('Target app update triggered successfully!', 'success');
                    // Refresh status after a short delay to show progress
                    setTimeout(() => {
                        loadStatus();
                        showNotification('Checking update status...', 'info');
                    }, 2000);
                })
                .catch(error => {
                    console.error('Update target error:', error);
                    showNotification('Failed to trigger target app update', 'error');
                })
                .finally(() => {
                    btn.classList.remove('loading');
                    btn.disabled = false;
                    btn.innerHTML = originalContent;
                });
        }

        function updateSelf() {
            const btn = document.getElementById('updateSelfBtn');
            const originalContent = btn.innerHTML;
            
            btn.classList.add('loading');
            btn.disabled = true;
            btn.innerHTML = '<span class="btn-icon">⏳</span><span>Updating...</span>';
            
            fetch('/update-self', { method: 'POST' })
                .then(response => response.json())
                .then(data => {
                    showNotification('Self update triggered successfully!', 'warning');
                    // Refresh status after a short delay to show progress
                    setTimeout(() => {
                        loadStatus();
                        showNotification('Checking update status...', 'info');
                    }, 2000);
                })
                .catch(error => {
                    console.error('Update self error:', error);
                    showNotification('Failed to trigger self update', 'error');
                })
                .finally(() => {
                    btn.classList.remove('loading');
                    btn.disabled = false;
                    btn.innerHTML = originalContent;
                });
        }

        function showNotification(message, type) {
            type = type || 'info';
            // Create notification element
            const notification = document.createElement('div');
            notification.className = 'notification notification-' + type;
            notification.innerHTML = '<div class="notification-content"><span class="notification-icon">' + getNotificationIcon(type) + '</span><span class="notification-message">' + message + '</span></div>';
            
            // Add to page
            document.body.appendChild(notification);
            
            // Animate in
            setTimeout(() => {
                notification.classList.add('show');
            }, 10);
            
            // Remove after 4 seconds
            setTimeout(() => {
                notification.classList.remove('show');
                setTimeout(() => {
                    if (notification.parentNode) {
                        document.body.removeChild(notification);
                    }
                }, 300);
            }, 4000);
        }

        function getNotificationIcon(type) {
            switch(type) {
                case 'success': return '✅';
                case 'error': return '❌';
                case 'warning': return '⚠️';
                case 'info': return 'ℹ️';
                default: return 'ℹ️';
            }
        }

        function updateSelf() {
            const btn = document.getElementById('updateSelfBtn');
            const originalContent = btn.innerHTML;
            
            btn.classList.add('loading');
            btn.disabled = true;
            btn.innerHTML = '<span class="btn-icon">⏳</span><span>Updating...</span>';
            
            fetch('/update-self', { method: 'POST' })
                .then(response => response.json())
                .then(data => {
                    showNotification('Self update triggered successfully!', 'warning');
                    // Refresh status after a short delay to show progress
                    setTimeout(() => {
                        loadStatus();
                        showNotification('Checking update status...', 'info');
                    }, 2000);
                })
                .catch(error => {
                    console.error('Update self error:', error);
                    showNotification('Failed to trigger self update', 'error');
                })
                .finally(() => {
                    btn.classList.remove('loading');
                    btn.disabled = false;
                    btn.innerHTML = originalContent;
                });
        }

        function showNotification(message, type) {
            type = type || 'info';
            // Create notification element
            const notification = document.createElement('div');
            notification.className = 'notification notification-' + type;
            notification.innerHTML = '<div class="notification-content"><span class="notification-icon">' + getNotificationIcon(type) + '</span><span class="notification-message">' + message + '</span></div>';
            
            // Add to page
            document.body.appendChild(notification);
            
            // Animate in
            setTimeout(() => {
                notification.classList.add('show');
            }, 10);
            
            // Remove after 4 seconds
            setTimeout(() => {
                notification.classList.remove('show');
                setTimeout(() => {
                    if (notification.parentNode) {
                        document.body.removeChild(notification);
                    }
                }, 300);
            }, 4000);
        }

        function getNotificationIcon(type) {
            switch(type) {
                case 'success': return '✅';
                case 'error': return '❌';
                case 'warning': return '⚠️';
                case 'info': return 'ℹ️';
                default: return 'ℹ️';
            }
        }

        // Auto-refresh every 5 seconds
        setInterval(loadStatus, 5000);
        
        // Initialize log streaming
        initializeLogStreaming();
        
        // Initial load
        loadStatus();
    </script>
</body>
</html>`

	fmt.Fprintf(w, html)
}
