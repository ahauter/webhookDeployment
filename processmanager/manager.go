package processmanager

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"binaryDeploy/config"
)

// Process represents a running application process
type Process struct {
	PID          int
	Cmd          *exec.Cmd
	StartTime    time.Time
	RestartCount int
	Config       *config.DeployConfig
	WorkingDir   string
	cancel       context.CancelFunc
}

// ProcessManager manages the lifecycle of a single application process
type ProcessManager struct {
	currentProcess *Process
	mutex          sync.RWMutex
	logger         *slog.Logger
}

// NewProcessManager creates a new ProcessManager instance
func NewProcessManager() *ProcessManager {
	return &ProcessManager{
		logger: slog.Default(),
	}
}

// GetCurrentPID safely returns the current process PID, or 0 if no process is running
func (pm *ProcessManager) GetCurrentPID() int {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	if pm.currentProcess != nil {
		return pm.currentProcess.PID
	}
	return 0
}

// GetCurrentWorkingDir returns the working directory of the current process
func (pm *ProcessManager) GetCurrentWorkingDir() string {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	if pm.currentProcess != nil {
		return pm.currentProcess.WorkingDir
	}
	return ""
}

// StartProcess stops any existing process and starts a new one
func (pm *ProcessManager) StartProcess(deployConfig *config.DeployConfig, workingDir string) error {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	// Stop any existing process first
	if pm.currentProcess != nil {
		if err := pm.stopProcessInternal(pm.currentProcess); err != nil {
			pm.logger.Warn("Failed to stop existing process", "error", err)
		}
	}

	// Create and start new process
	process, err := pm.createProcess(deployConfig, workingDir)
	if err != nil {
		return fmt.Errorf("failed to create process: %w", err)
	}

	if err := pm.startProcessInternal(process); err != nil {
		return fmt.Errorf("failed to start process: %w", err)
	}

	pm.currentProcess = process
	pm.logger.Info("Process started successfully",
		"pid", process.PID,
		"command", deployConfig.RunCommand,
		"working_dir", workingDir)

	// Start monitoring the process in a goroutine
	go pm.monitorProcess(process)

	return nil
}

// StopCurrentProcess stops the currently running process
func (pm *ProcessManager) StopCurrentProcess() error {
	pm.mutex.Lock()

	if pm.currentProcess == nil {
		pm.mutex.Unlock()
		return nil // No process to stop
	}

	// Get reference to current process and clear it to avoid race
	process := pm.currentProcess
	pm.currentProcess = nil
	pm.mutex.Unlock()

	// Stop the process outside of lock
	err := pm.stopProcessInternal(process)
	return err
}

// IsRunning returns true if a process is currently running
func (pm *ProcessManager) IsRunning() bool {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	return pm.currentProcess != nil
}

// createProcess creates a new Process instance without starting it
func (pm *ProcessManager) createProcess(deployConfig *config.DeployConfig, workingDir string) (*Process, error) {
	ctx, cancel := context.WithCancel(context.Background())

	cmd := exec.CommandContext(ctx, "sh", "-c", deployConfig.RunCommand)
	cmd.Dir = workingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return &Process{
		Config:     deployConfig,
		WorkingDir: workingDir,
		Cmd:        cmd,
		cancel:     cancel,
	}, nil
}

// startProcessInternal starts a process and sets its PID
func (pm *ProcessManager) startProcessInternal(process *Process) error {
	if err := process.Cmd.Start(); err != nil {
		return err
	}

	process.PID = process.Cmd.Process.Pid
	process.StartTime = time.Now()

	return nil
}

// stopProcessInternal stops a process gracefully with SIGTERM, then SIGKILL if needed
func (pm *ProcessManager) stopProcessInternal(process *Process) error {
	if process.Cmd == nil || process.Cmd.Process == nil {
		return nil
	}

	pid := process.Cmd.Process.Pid
	pm.logger.Info("Stopping process", "pid", pid)

	// Cancel the context first
	if process.cancel != nil {
		process.cancel()
	}

	// Try graceful shutdown with SIGTERM
	if err := process.Cmd.Process.Signal(syscall.SIGTERM); err != nil {
		pm.logger.Warn("Failed to send SIGTERM", "pid", pid, "error", err)
	}

	// Give process time to exit, then force kill if needed
	time.Sleep(100 * time.Millisecond)

	// Force kill to ensure process stops
	if err := process.Cmd.Process.Kill(); err != nil {
		pm.logger.Warn("Failed to kill process", "pid", pid, "error", err)
	}

	// Don't wait to avoid race condition with monitoring goroutine
	return nil
}

// monitorProcess watches a process and handles restarts if it exits unexpectedly
func (pm *ProcessManager) monitorProcess(process *Process) {
	err := process.Cmd.Wait()

	pm.mutex.Lock()

	// Check if this is still the current process (might have been replaced)
	if pm.currentProcess != process {
		pm.mutex.Unlock()
		return
	}

	// Clear current process before potentially starting a new one
	pm.currentProcess = nil

	pm.mutex.Unlock()

	if err != nil {
		pm.logger.Error("Process exited with error",
			"pid", process.PID,
			"error", err,
			"uptime", time.Since(process.StartTime))
	} else {
		pm.logger.Info("Process exited normally",
			"pid", process.PID,
			"uptime", time.Since(process.StartTime))
	}

	// Handle restart logic
	if process.Config.MaxRestarts > 0 && process.RestartCount < process.Config.MaxRestarts {
		process.RestartCount++
		pm.logger.Info("Restarting process",
			"attempt", process.RestartCount,
			"max_restarts", process.Config.MaxRestarts,
			"delay_seconds", process.Config.RestartDelay)

		// Wait before restart
		time.Sleep(time.Duration(process.Config.RestartDelay) * time.Second)

		// Try to restart - this will handle locking properly
		newProcess, err := pm.createProcess(process.Config, process.WorkingDir)
		if err != nil {
			pm.logger.Error("Failed to create restart process", "error", err)
			return
		}

		if err := pm.startProcessInternal(newProcess); err != nil {
			pm.logger.Error("Failed to start restart process", "error", err)
			return
		}

		newProcess.RestartCount = process.RestartCount

		pm.mutex.Lock()
		pm.currentProcess = newProcess
		pm.mutex.Unlock()

		pm.logger.Info("Process restarted successfully", "pid", newProcess.PID)

		// Continue monitoring the new process
		go pm.monitorProcess(newProcess)
	} else {
		pm.logger.Info("Process will not be restarted",
			"restart_count", process.RestartCount,
			"max_restarts", process.Config.MaxRestarts)
	}
}

// GetWebStatus returns a map with process status information for web display
func (pm *ProcessManager) GetWebStatus() map[string]interface{} {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	status := map[string]interface{}{
		"running":       false,
		"pid":           0,
		"uptime":        "",
		"command":       "",
		"working_dir":   "",
		"restart_count": 0,
		"config":        map[string]interface{}{},
	}

	if pm.currentProcess != nil {
		uptime := time.Since(pm.currentProcess.StartTime)

		status["running"] = true
		status["pid"] = pm.currentProcess.PID
		status["uptime"] = uptime.String()
		status["command"] = pm.currentProcess.Config.RunCommand
		status["working_dir"] = pm.currentProcess.WorkingDir
		status["restart_count"] = pm.currentProcess.RestartCount

		if pm.currentProcess.Config != nil {
			status["config"] = map[string]interface{}{
				"build_command": pm.currentProcess.Config.BuildCommand,
				"run_command":   pm.currentProcess.Config.RunCommand,
				"working_dir":   pm.currentProcess.Config.WorkingDir,
				"environment":   pm.currentProcess.Config.Environment,
				"max_restarts":  pm.currentProcess.Config.MaxRestarts,
				"restart_delay": pm.currentProcess.Config.RestartDelay,
			}
		}
	}

	return status
}

// Shutdown stops all processes gracefully
func (pm *ProcessManager) Shutdown() error {
	return pm.StopCurrentProcess()
}
