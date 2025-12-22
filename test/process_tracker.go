package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"log/slog"
)

// TrackedProcess represents a process being tracked by the test environment
type TrackedProcess struct {
	PID         int
	PPID        int
	CmdLine     string
	Description string
	StartTime   time.Time
	Killed      bool
	KillTime    *time.Time
	KillMethod  string
}

// AggressiveProcessTracker provides aggressive process tracking and cleanup
type AggressiveProcessTracker struct {
	Processes    map[int]*TrackedProcess
	CleanupFuncs []func() error
	mutex        sync.RWMutex
	LogDir       string
	Logger       *slog.Logger
	ProcessLog   *os.File
}

// NewAggressiveProcessTracker creates a new aggressive process tracker
func NewAggressiveProcessTracker(logDir string) *AggressiveProcessTracker {
	// Create process log file
	processLogFile := filepath.Join(logDir, "processes.log")
	file, err := os.OpenFile(processLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Fallback to stdout if file creation fails
		file = os.Stdout
	}

	logger := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	tracker := &AggressiveProcessTracker{
		Processes:  make(map[int]*TrackedProcess),
		LogDir:     logDir,
		Logger:     logger,
		ProcessLog: file,
	}

	// Start process monitoring goroutine
	go tracker.monitorProcesses()

	return tracker
}

// TrackProcess starts tracking a process by PID
func (apt *AggressiveProcessTracker) TrackProcess(pid int, description string) (*TrackedProcess, error) {
	apt.mutex.Lock()
	defer apt.mutex.Unlock()

	// Get process info
	cmdLine, ppid, err := apt.getProcessInfo(pid)
	if err != nil {
		return nil, fmt.Errorf("failed to get process info for PID %d: %w", pid, err)
	}

	process := &TrackedProcess{
		PID:         pid,
		PPID:        ppid,
		CmdLine:     cmdLine,
		Description: description,
		StartTime:   time.Now(),
		Killed:      false,
	}

	apt.Processes[pid] = process

	apt.Logger.Info("Started tracking process",
		"pid", pid,
		"ppid", ppid,
		"cmdline", cmdLine,
		"description", description,
		"start_time", process.StartTime,
	)

	return process, nil
}

// ExecuteCommand executes a command and automatically tracks it
func (apt *AggressiveProcessTracker) ExecuteCommand(name string, args ...string) (*TrackedProcess, error) {
	cmd := exec.Command(name, args...)

	// Start the command
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command %s: %w", name, err)
	}

	pid := cmd.Process.Pid
	description := fmt.Sprintf("exec: %s %s", name, strings.Join(args, " "))

	// Track the process
	process, err := apt.TrackProcess(pid, description)
	if err != nil {
		// Kill the process if tracking failed
		cmd.Process.Kill()
		return nil, err
	}

	// Add cleanup function to wait for process completion
	apt.AddCleanupFunc(func() error {
		if !process.Killed {
			// Check if process is still running
			if apt.isProcessRunning(pid) {
				apt.Logger.Info("Waiting for process to complete", "pid", pid)
				cmd.Wait() // nolint: errcheck // Ignore wait error during cleanup
			}
		}
		return nil
	})

	return process, nil
}

// KillProcessAggressively kills a process using increasingly aggressive methods
func (apt *AggressiveProcessTracker) KillProcessAggressively(pid int) error {
	apt.mutex.Lock()
	process, exists := apt.Processes[pid]
	if !exists {
		apt.mutex.Unlock()
		return fmt.Errorf("process %d not found in tracker", pid)
	}

	if process.Killed {
		apt.mutex.Unlock()
		return nil // Already killed
	}

	apt.mutex.Unlock()

	apt.Logger.Info("Starting aggressive kill sequence", "pid", pid, "description", process.Description)

	// First check if process is already dead
	if !apt.isProcessRunning(pid) {
		apt.Logger.Info("Process already dead", "pid", pid)
		return apt.markProcessKilled(pid, "already_dead")
	}

	// Method 1: Graceful SIGTERM
	if err := apt.killProcess(pid, syscall.SIGTERM, "SIGTERM"); err == nil {
		time.Sleep(100 * time.Millisecond)
		if !apt.isProcessRunning(pid) {
			return apt.markProcessKilled(pid, "SIGTERM")
		}
	}

	// Method 2: Forceful SIGKILL
	if err := apt.killProcess(pid, syscall.SIGKILL, "SIGKILL"); err == nil {
		time.Sleep(50 * time.Millisecond)
		if !apt.isProcessRunning(pid) {
			return apt.markProcessKilled(pid, "SIGKILL")
		}
	}

	// Method 3: Kill entire process group
	if err := apt.killProcessGroup(pid); err == nil {
		time.Sleep(50 * time.Millisecond)
		if !apt.isProcessRunning(pid) {
			return apt.markProcessKilled(pid, "process_group")
		}
	}

	// Method 4: Kill all children and try again
	if err := apt.killProcessChildren(pid); err == nil {
		time.Sleep(50 * time.Millisecond)
		if err := apt.killProcess(pid, syscall.SIGKILL, "SIGKILL_after_children"); err == nil {
			time.Sleep(50 * time.Millisecond)
			if !apt.isProcessRunning(pid) {
				return apt.markProcessKilled(pid, "SIGKILL_after_children")
			}
		}
	}

	// Final check - maybe it died during our attempts
	if !apt.isProcessRunning(pid) {
		return apt.markProcessKilled(pid, "died_during_attempts")
	}

	return fmt.Errorf("failed to kill process %d with all methods", pid)
}

// KillAllProcesses kills all tracked processes aggressively
func (apt *AggressiveProcessTracker) KillAllProcesses() error {
	apt.mutex.RLock()
	pids := make([]int, 0, len(apt.Processes))
	for pid := range apt.Processes {
		pids = append(pids, pid)
	}
	apt.mutex.RUnlock()

	var errors []error
	for _, pid := range pids {
		if err := apt.KillProcessAggressively(pid); err != nil {
			errors = append(errors, err)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to kill %d processes: %v", len(errors), errors)
	}

	return nil
}

// CleanupAll performs complete cleanup of all processes and resources
func (apt *AggressiveProcessTracker) CleanupAll() error {
	apt.Logger.Info("Starting complete process cleanup")

	// Kill all processes
	if err := apt.KillAllProcesses(); err != nil {
		apt.Logger.Error("Failed to kill all processes", "error", err)
	}

	// Run cleanup functions
	apt.mutex.Lock()
	cleanupFuncs := apt.CleanupFuncs
	apt.mutex.Unlock()

	var errors []error
	for i, cleanup := range cleanupFuncs {
		if err := cleanup(); err != nil {
			errors = append(errors, fmt.Errorf("cleanup function %d: %w", i, err))
		}
	}

	// Clear all tracking data
	apt.mutex.Lock()
	apt.Processes = make(map[int]*TrackedProcess)
	apt.CleanupFuncs = nil
	apt.mutex.Unlock()

	// Close log file
	if apt.ProcessLog != nil && apt.ProcessLog != os.Stdout {
		apt.ProcessLog.Close()
	}

	if len(errors) > 0 {
		return fmt.Errorf("cleanup completed with %d errors: %v", len(errors), errors)
	}

	apt.Logger.Info("Complete process cleanup finished")
	return nil
}

// VerifyNoProcessBuildup ensures no tracked processes remain
func (apt *AggressiveProcessTracker) VerifyNoProcessBuildup() error {
	apt.mutex.RLock()
	defer apt.mutex.RUnlock()

	var runningPids []int
	for pid, process := range apt.Processes {
		if apt.isProcessRunning(pid) {
			runningPids = append(runningPids, pid)
			apt.Logger.Error("Process still running after cleanup",
				"pid", pid,
				"description", process.Description,
				"start_time", process.StartTime,
			)
		}
	}

	if len(runningPids) > 0 {
		return fmt.Errorf("found %d processes still running: %v", len(runningPids), runningPids)
	}

	return nil
}

// AddCleanupFunc adds a cleanup function to be called during cleanup
func (apt *AggressiveProcessTracker) AddCleanupFunc(cleanup func() error) {
	apt.mutex.Lock()
	defer apt.mutex.Unlock()
	apt.CleanupFuncs = append(apt.CleanupFuncs, cleanup)
}

// GetProcessCount returns the number of tracked processes
func (apt *AggressiveProcessTracker) GetProcessCount() int {
	apt.mutex.RLock()
	defer apt.mutex.RUnlock()
	return len(apt.Processes)
}

// getProcessInfo retrieves process information from /proc
func (apt *AggressiveProcessTracker) getProcessInfo(pid int) (string, int, error) {
	// Get command line
	cmdLineBytes, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline"))
	if err != nil {
		return "", 0, err
	}
	cmdLine := strings.ReplaceAll(string(cmdLineBytes), "\x00", " ")

	// Get PPID
	statBytes, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return "", 0, err
	}
	statParts := strings.Fields(string(statBytes))
	if len(statParts) < 4 {
		return "", 0, fmt.Errorf("invalid stat format for PID %d", pid)
	}

	ppid, err := strconv.Atoi(statParts[3])
	if err != nil {
		return "", 0, fmt.Errorf("failed to parse PPID for PID %d: %w", pid, err)
	}

	return cmdLine, ppid, nil
}

// isProcessRunning checks if a process is still running
func (apt *AggressiveProcessTracker) isProcessRunning(pid int) bool {
	_, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	return err == nil
}

// killProcess sends a signal to a process
func (apt *AggressiveProcessTracker) killProcess(pid int, signal syscall.Signal, method string) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	apt.Logger.Debug("Sending signal to process", "pid", pid, "signal", signal, "method", method)
	return process.Signal(signal)
}

// killProcessGroup kills the entire process group
func (apt *AggressiveProcessTracker) killProcessGroup(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}

	// Get process group ID
	pgid := process.Pid // On Unix, negative PID kills process group
	apt.Logger.Debug("Killing process group", "pid", pid, "pgid", pgid)

	grp, err := os.FindProcess(-pgid)
	if err != nil {
		return err
	}

	return grp.Signal(syscall.SIGKILL)
}

// killProcessChildren kills all child processes of a given PID
func (apt *AggressiveProcessTracker) killProcessChildren(pid int) error {
	// Find children by scanning /proc
	procDir, err := os.Open("/proc")
	if err != nil {
		return err
	}
	defer procDir.Close()

	names, err := procDir.Readdirnames(-1)
	if err != nil {
		return err
	}

	var childPids []int
	for _, name := range names {
		if childPid, err := strconv.Atoi(name); err == nil {
			if childPPid, err := apt.getParentPid(childPid); err == nil {
				if childPPid == pid {
					childPids = append(childPids, childPid)
				}
			}
		}
	}

	// Kill all children
	var errors []error
	for _, childPid := range childPids {
		if err := apt.KillProcessAggressively(childPid); err != nil {
			errors = append(errors, fmt.Errorf("failed to kill child %d: %w", childPid, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("failed to kill children: %v", errors)
	}

	return nil
}

// getParentPid gets the parent PID of a process
func (apt *AggressiveProcessTracker) getParentPid(pid int) (int, error) {
	statBytes, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return 0, err
	}
	statParts := strings.Fields(string(statBytes))
	if len(statParts) < 4 {
		return 0, fmt.Errorf("invalid stat format for PID %d", pid)
	}

	return strconv.Atoi(statParts[3])
}

// markProcessKilled marks a process as killed with the specified method
func (apt *AggressiveProcessTracker) markProcessKilled(pid int, method string) error {
	apt.mutex.Lock()
	defer apt.mutex.Unlock()

	process, exists := apt.Processes[pid]
	if !exists {
		return fmt.Errorf("process %d not found", pid)
	}

	process.Killed = true
	killTime := time.Now()
	process.KillTime = &killTime
	process.KillMethod = method

	apt.Logger.Info("Process killed successfully",
		"pid", pid,
		"description", process.Description,
		"method", method,
		"duration", killTime.Sub(process.StartTime),
	)

	return nil
}

// monitorProcesses runs a background monitor to detect zombie processes
func (apt *AggressiveProcessTracker) monitorProcesses() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		apt.mutex.RLock()
		processes := make(map[int]*TrackedProcess)
		for k, v := range apt.Processes {
			processes[k] = v
		}
		apt.mutex.RUnlock()

		for pid, process := range processes {
			if !process.Killed && !apt.isProcessRunning(pid) {
				apt.Logger.Info("Process died naturally",
					"pid", pid,
					"description", process.Description,
					"duration", time.Since(process.StartTime),
				)
			}
		}
	}
}
