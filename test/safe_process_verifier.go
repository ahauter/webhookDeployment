package test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"log/slog"
)

// SafeProcessVerifier provides path-based process verification for testing
type SafeProcessVerifier struct {
	testEnvPath string
	logger      *slog.Logger
}

// NewSafeProcessVerifier creates a new safe process verifier
func NewSafeProcessVerifier(testEnvPath string, logger *slog.Logger) *SafeProcessVerifier {
	return &SafeProcessVerifier{
		testEnvPath: testEnvPath,
		logger:      logger,
	}
}

// VerifyDeploymentProcess safely verifies a deployment process is running
func (spv *SafeProcessVerifier) VerifyDeploymentProcess(pid int, expectedName string) error {
	if pid == 0 {
		return fmt.Errorf("invalid PID: 0")
	}

	// Check if process exists
	if !spv.isProcessRunning(pid) {
		return fmt.Errorf("process %d is not running", pid)
	}

	// Get process command line for verification
	cmdLine, err := spv.getProcessCommandLine(pid)
	if err != nil {
		return fmt.Errorf("failed to get process command line: %w", err)
	}

	// Verify process is running from our test environment
	if !strings.Contains(cmdLine, spv.testEnvPath) && !strings.Contains(cmdLine, "/tmp/binarydeploy-tests") {
		return fmt.Errorf("process %d is not running from test environment. Command: %s", pid, cmdLine)
	}

	// Verify process name matches expected
	if !strings.Contains(cmdLine, expectedName) {
		return fmt.Errorf("process %d command does not contain expected name %s. Command: %s", pid, expectedName, cmdLine)
	}

	spv.logger.Info("Process verification successful",
		"pid", pid,
		"command", cmdLine,
		"expected_name", expectedName)

	return nil
}

// SafeKillProcess safely kills a process after thorough verification
func (spv *SafeProcessVerifier) SafeKillProcess(pid int, expectedName string) error {
	if pid == 0 {
		return fmt.Errorf("invalid PID: 0")
	}

	// Final verification before kill
	if err := spv.VerifyDeploymentProcess(pid, expectedName); err != nil {
		return fmt.Errorf("safety verification failed before kill: %w", err)
	}

	// Attempt graceful termination first
	if err := exec.Command("kill", "-TERM", strconv.Itoa(pid)).Run(); err != nil {
		spv.logger.Warn("Graceful termination failed, using SIGKILL", "pid", pid, "error", err)
		// Force kill if graceful termination fails
		if err := exec.Command("kill", "-KILL", strconv.Itoa(pid)).Run(); err != nil {
			return fmt.Errorf("failed to kill process %d with both SIGTERM and SIGKILL: %w", pid, err)
		}
	} else {
		spv.logger.Info("Process terminated gracefully", "pid", pid)
	}

	return nil
}

// isProcessRunning checks if a process with given PID exists
func (spv *SafeProcessVerifier) isProcessRunning(pid int) bool {
	// Check /proc/<pid> exists (Linux specific but good for our test environment)
	if _, err := os.Stat(filepath.Join("/proc", strconv.Itoa(pid))); err != nil {
		return false
	}

	// Double-check with ps command
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid))
	err := cmd.Run()
	return err == nil
}

// getProcessCommandLine returns the command line for a process
func (spv *SafeProcessVerifier) getProcessCommandLine(pid int) (string, error) {
	// Try reading from /proc/<pid>/cmdline first (most reliable)
	cmdlineFile := filepath.Join("/proc", strconv.Itoa(pid), "cmdline")
	if cmdlineData, err := os.ReadFile(cmdlineFile); err == nil {
		// Convert null-separated args to space-separated string
		cmdline := strings.ReplaceAll(string(cmdlineData), "\x00", " ")
		cmdline = strings.TrimSpace(cmdline)
		if cmdline != "" {
			return cmdline, nil
		}
	}

	// Fallback to ps command
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ps command failed: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// FindProcessesByName finds all processes matching expected name and path criteria
func (spv *SafeProcessVerifier) FindProcessesByName(expectedName string) ([]ProcessInfo, error) {
	var processes []ProcessInfo

	// Use ps to find all processes, then filter safely
	cmd := exec.Command("ps", "ax", "-o", "pid,command=")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list processes: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Split into PID and command
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		pidStr := parts[0]
		command := strings.Join(parts[1:], " ")

		// Check if this is our process
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}

		// Verify it's our process
		if strings.Contains(command, expectedName) &&
			(strings.Contains(command, spv.testEnvPath) || strings.Contains(command, "/tmp/binarydeploy-tests")) {

			processes = append(processes, ProcessInfo{
				PID:     pid,
				Command: command,
				Path:    spv.extractWorkingDirectory(pid),
			})
		}
	}

	return processes, nil
}

// ProcessInfo contains information about a found process
type ProcessInfo struct {
	PID     int
	Command string
	Path    string
}

// extractWorkingDirectory attempts to extract the working directory of a process
func (spv *SafeProcessVerifier) extractWorkingDirectory(pid int) string {
	// Try to read from /proc/<pid>/cwd symlink
	cwdPath := filepath.Join("/proc", strconv.Itoa(pid), "cwd")
	if path, err := os.Readlink(cwdPath); err == nil {
		return path
	}
	return ""
}
