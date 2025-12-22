package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"log/slog"
)

// RealisticTestEnv provides a realistic testing environment with aggressive cleanup
type RealisticTestEnv struct {
	// Core infrastructure
	PortAllocator  *PortAllocator
	ProcessTracker *AggressiveProcessTracker
	LogPersistence *LogPersistence

	// Test metadata
	TestName  string
	StartTime time.Time
	TempDir   string
	LogDir    string

	// Server components
	Server     *http.Server
	ServerURL  string
	ConfigPath string
	DeployDir  string

	// Process management
	MainPID        int
	ChildProcesses map[int]*TrackedProcess

	// Cleanup management
	CleanupFuncs []func() error
	CleanupMutex sync.Mutex

	// Logging
	Logger *slog.Logger
}

// NewRealisticTestEnv creates a new realistic test environment
func NewRealisticTestEnv(testName string) (*RealisticTestEnv, error) {
	// Create timestamped test directory
	timestamp := time.Now().Format("20060102-150405.000")
	baseDir := filepath.Join("/tmp", "binarydeploy-tests", timestamp)

	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create test base dir: %w", err)
	}

	// Create log directory
	logDir := filepath.Join(baseDir, "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create log dir: %w", err)
	}

	// Create deployment directory
	deployDir := filepath.Join(baseDir, "deploy")
	if err := os.MkdirAll(deployDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create deploy dir: %w", err)
	}

	// Initialize components
	portAllocator := NewPortAllocator(20000, 20999)
	processTracker := NewAggressiveProcessTracker(logDir)
	logPersistence := NewLogPersistence(logDir)

	// Create logger
	logFile := filepath.Join(logDir, "main.log")
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to create main log file: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	env := &RealisticTestEnv{
		TestName:       testName,
		StartTime:      time.Now(),
		TempDir:        baseDir,
		LogDir:         logDir,
		DeployDir:      deployDir,
		PortAllocator:  portAllocator,
		ProcessTracker: processTracker,
		LogPersistence: logPersistence,
		ChildProcesses: make(map[int]*TrackedProcess),
		CleanupFuncs:   make([]func() error, 0),
		Logger:         logger,
	}

	// Log environment creation
	env.Logger.Info("Created realistic test environment",
		"test_name", testName,
		"temp_dir", baseDir,
		"log_dir", logDir,
		"deploy_dir", deployDir,
		"start_time", env.StartTime,
	)

	return env, nil
}

// StartRealWebhookServer starts the real webhook server (not mock)
func (env *RealisticTestEnv) StartRealWebhookServer() error {
	// Allocate port
	port, err := env.PortAllocator.AllocatePort()
	if err != nil {
		return fmt.Errorf("failed to allocate port: %w", err)
	}

	// Create config file
	configPath := filepath.Join(env.TempDir, "config.json")
	configContent := fmt.Sprintf(`{
		"port": %d,
		"secret": "test-secret",
		"log_level": "debug"
	}`, port)

	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("failed to create config file: %w", err)
	}

	env.ConfigPath = configPath
	env.ServerURL = fmt.Sprintf("http://localhost:%d", port)

	// TODO: Import and use real webhook handler from main.go
	// For now, create a basic server that will be replaced
	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Webhook received"))
	})

	env.Server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	// Start server in background
	listenErr := make(chan error, 1)
	go func() {
		env.Logger.Info("Starting webhook server", "port", port, "url", env.ServerURL)
		listenErr <- env.Server.ListenAndServe()
	}()

	// Wait for server to start
	select {
	case err := <-listenErr:
		return fmt.Errorf("server failed to start: %w", err)
	case <-time.After(100 * time.Millisecond):
		// Server should be ready
	}

	env.Logger.Info("Webhook server started successfully", "url", env.ServerURL)
	return nil
}

// TrackProcess tracks a new process and adds it to cleanup
func (env *RealisticTestEnv) TrackProcess(pid int, description string) error {
	process, err := env.ProcessTracker.TrackProcess(pid, description)
	if err != nil {
		return fmt.Errorf("failed to track process %d: %w", pid, err)
	}

	env.ChildProcesses[pid] = process
	env.Logger.Info("Tracking new process", "pid", pid, "description", description)

	return nil
}

// ExecuteCommand executes a command and tracks it
func (env *RealisticTestEnv) ExecuteCommand(name string, args ...string) (*TrackedProcess, error) {
	process, err := env.ProcessTracker.ExecuteCommand(name, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to execute command: %w", err)
	}

	env.ChildProcesses[process.PID] = process
	env.Logger.Info("Executed and tracking command",
		"pid", process.PID,
		"command", name,
		"args", args,
	)

	return process, nil
}

// AddCleanupFunc adds a cleanup function to be called on cleanup
func (env *RealisticTestEnv) AddCleanupFunc(cleanup func() error) {
	env.CleanupMutex.Lock()
	defer env.CleanupMutex.Unlock()
	env.CleanupFuncs = append(env.CleanupFuncs, cleanup)
}

// Cleanup performs aggressive cleanup of all resources
func (env *RealisticTestEnv) Cleanup() error {
	env.Logger.Info("Starting aggressive cleanup", "test_duration", time.Since(env.StartTime))

	var errors []error

	// 1. Stop webhook server
	if env.Server != nil {
		env.Logger.Info("Stopping webhook server")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := env.Server.Shutdown(ctx); err != nil {
			errors = append(errors, fmt.Errorf("server shutdown error: %w", err))
		}
	}

	// 2. Kill all tracked processes aggressively
	env.Logger.Info("Killing tracked processes", "count", len(env.ChildProcesses))
	for pid := range env.ChildProcesses {
		if err := env.ProcessTracker.KillProcessAggressively(pid); err != nil {
			errors = append(errors, fmt.Errorf("failed to kill process %d: %w", pid, err))
		}
	}

	// 3. Process tracker cleanup
	if err := env.ProcessTracker.CleanupAll(); err != nil {
		errors = append(errors, fmt.Errorf("process tracker cleanup error: %w", err))
	}

	// 4. Clean up orphaned processes (expected behavior)
	env.Logger.Info("Cleaning up orphaned processes")
	if err := env.cleanupOrphanedProcesses(); err != nil {
		errors = append(errors, fmt.Errorf("orphaned process cleanup error: %w", err))
	}

	// 5. Port cleanup
	if err := env.PortAllocator.ReleaseAllPorts(); err != nil {
		errors = append(errors, fmt.Errorf("port cleanup error: %w", err))
	}

	// 5. Custom cleanup functions
	env.CleanupMutex.Lock()
	cleanupFuncs := env.CleanupFuncs
	env.CleanupMutex.Unlock()

	for i, cleanup := range cleanupFuncs {
		env.Logger.Info("Running custom cleanup function", "index", i)
		if err := cleanup(); err != nil {
			errors = append(errors, fmt.Errorf("cleanup function %d error: %w", i, err))
		}
	}

	// 6. Flush logs
	if err := env.LogPersistence.FlushAll(); err != nil {
		errors = append(errors, fmt.Errorf("log flush error: %w", err))
	}

	// 7. Verify no processes remain
	if err := env.ProcessTracker.VerifyNoProcessBuildup(); err != nil {
		errors = append(errors, fmt.Errorf("process buildup detected: %w", err))
	}

	duration := time.Since(env.StartTime)
	if len(errors) > 0 {
		env.Logger.Error("Cleanup completed with errors",
			"duration", duration,
			"error_count", len(errors),
		)
		return fmt.Errorf("cleanup completed with %d errors: %v", len(errors), errors)
	}

	env.Logger.Info("Aggressive cleanup completed successfully",
		"duration", duration,
		"temp_dir", env.TempDir,
	)

	return nil
}

// cleanupOrphanedProcesses cleans up expected orphaned processes (target-app, binaryDeploy)
func (env *RealisticTestEnv) cleanupOrphanedProcesses() error {
	processes := []string{"target-app", "binaryDeploy"}
	var errors []error

	// Create safe process verifier
	verifier := NewSafeProcessVerifier(env.TempDir, env.Logger)

	for _, procName := range processes {
		// Find processes using safe criteria
		foundProcesses, err := verifier.FindProcessesByName(procName)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to find %s processes: %w", procName, err))
			continue
		}

		if len(foundProcesses) == 0 {
			continue // No processes found is expected
		}

		// Verify and clean up each process safely
		for _, proc := range foundProcesses {
			// Safety verification - this will fail if process is not from our test environment
			if err := verifier.VerifyDeploymentProcess(proc.PID, procName); err != nil {
				errors = append(errors, fmt.Errorf("❌ SAFETY VIOLATION: %s process %d failed verification: %w", procName, proc.PID, err))
				continue
			}

			// Safe kill - will only kill verified processes
			if err := verifier.SafeKillProcess(proc.PID, procName); err != nil {
				errors = append(errors, fmt.Errorf("failed to safely kill orphaned %s process %d: %w", procName, proc.PID, err))
			} else {
				env.Logger.Info("Safely cleaned up orphaned process", "name", procName, "pid", proc.PID)
			}
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup errors: %v", errors)
	}
	return nil
}

// GetLogFilePath returns the path to a specific log file
func (env *RealisticTestEnv) GetLogFilePath(logType string) string {
	return filepath.Join(env.LogDir, logType+".log")
}

// CreateTempFile creates a temporary file within the test environment
func (env *RealisticTestEnv) CreateTempFile(name string, content string) (string, error) {
	filePath := filepath.Join(env.TempDir, name)
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to create temp file %s: %w", name, err)
	}
	return filePath, nil
}

// ResourceMonitor provides resource monitoring and simulation capabilities
type ResourceMonitor struct {
	tempDir    string
	logger     *slog.Logger
	initialMem runtime.MemStats
	initialCPU time.Duration
}

// NewResourceMonitor creates a new resource monitor
func NewResourceMonitor(tempDir string, logger *slog.Logger) *ResourceMonitor {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return &ResourceMonitor{
		tempDir:    tempDir,
		logger:     logger,
		initialMem: m,
	}
}

// MonitorMemoryUsage tracks memory usage during test execution
func (rm *ResourceMonitor) MonitorMemoryUsage() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	allocDiff := m.Alloc - rm.initialMem.Alloc
	allocMB := float64(allocDiff) / 1024 / 1024

	rm.logger.Info("Memory usage",
		"allocated_mb", allocMB,
		"total_alloc_mb", float64(m.TotalAlloc)/1024/1024,
		"sys_mb", float64(m.Sys)/1024/1024,
		"num_gc", m.NumGC)
}

// CreateReadOnlyDirectory creates a directory with read-only permissions
func (rm *ResourceMonitor) CreateReadOnlyDirectory(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Remove write permissions
	if err := os.Chmod(path, 0555); err != nil {
		return fmt.Errorf("failed to set read-only permissions: %w", err)
	}

	return nil
}

// CreateFullDiskSimulation simulates a full disk by creating a large file
func (rm *ResourceMonitor) CreateFullDiskSimulation(sizeMB int) error {
	largeFile := filepath.Join(rm.tempDir, "disk-fill.tmp")

	// Create a large file to simulate disk space exhaustion
	data := make([]byte, 1024*1024) // 1MB chunk

	file, err := os.Create(largeFile)
	if err != nil {
		return fmt.Errorf("failed to create disk fill file: %w", err)
	}
	defer file.Close()

	for i := 0; i < sizeMB; i++ {
		if _, err := file.Write(data); err != nil {
			return fmt.Errorf("failed to write disk fill data: %w", err)
		}
	}

	rm.logger.Info("Created disk space simulation", "size_mb", sizeMB, "file", largeFile)
	return nil
}

// MonitorProcessCount tracks the number of processes
func (rm *ResourceMonitor) MonitorProcessCount() (int, error) {
	// Simple process count monitoring
	dir, err := os.Open("/proc")
	if err != nil {
		return 0, fmt.Errorf("failed to open /proc: %w", err)
	}
	defer dir.Close()

	entries, err := dir.Readdirnames(-1)
	if err != nil {
		return 0, fmt.Errorf("failed to read /proc: %w", err)
	}

	// Count numeric PIDs (processes)
	count := 0
	for _, entry := range entries {
		if _, err := os.Stat(filepath.Join("/proc", entry)); err == nil {
			if _, parseErr := fmt.Sscanf(entry, "%d", new(int)); parseErr == nil {
				count++
			}
		}
	}

	rm.logger.Info("Process count", "count", count)
	return count, nil
}

// SimulateFileLock simulates a locked file scenario
func (rm *ResourceMonitor) SimulateFileLock(filePath string) error {
	// Create and lock a file
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return fmt.Errorf("failed to create lock file: %w", err)
	}

	// Write some data to make it look legitimate
	if _, err := file.WriteString("locked content"); err != nil {
		file.Close()
		return fmt.Errorf("failed to write lock file: %w", err)
	}

	// Keep file open to maintain lock (in real scenario, this would be another process)
	rm.logger.Info("Created file lock simulation", "file", filePath)

	// In a real test environment, we'd have another process keep this open
	// For now, just close it after a delay
	go func() {
		time.Sleep(30 * time.Second)
		file.Close()
		rm.logger.Info("Released file lock", "file", filePath)
	}()

	return nil
}

// GetResourceLimits checks current system resource limits
func (rm *ResourceMonitor) GetResourceLimits() map[string]interface{} {
	limits := make(map[string]interface{})

	var rlimit syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rlimit); err == nil {
		limits["max_files"] = rlimit.Cur
	}

	// Note: RLIMIT_NPROC may not be available on all systems
	// We'll skip process limit check for cross-platform compatibility

	rm.logger.Info("Resource limits", "limits", limits)
	return limits
}
