package processmanager

import (
	"testing"
	"time"

	"binaryDeploy/config"
)

func TestProcessManager_NewProcessManager(t *testing.T) {
	pm := NewProcessManager()
	if pm == nil {
		t.Fatal("NewProcessManager returned nil")
	}
	if pm.currentProcess != nil {
		t.Error("Expected no current process initially")
	}
	if pm.logger == nil {
		t.Error("Expected logger to be set")
	}
}

func TestProcessManager_StartAndStopProcess(t *testing.T) {
	pm := NewProcessManager()

	// Create a simple test config with a process that stays running
	deployConfig := &config.DeployConfig{
		RunCommand:   "sleep 5",
		WorkingDir:   "./",
		RestartDelay: 1,
		MaxRestarts:  0, // No restarts for this test
	}

	// Start process
	err := pm.StartProcess(deployConfig, "./")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Verify process is running
	if !pm.IsRunning() {
		t.Error("Expected process to be running")
	}

	pid := pm.GetCurrentPID()
	if pid == 0 {
		t.Error("Expected non-zero PID")
	}

	// Wait a bit for the process to actually start
	time.Sleep(100 * time.Millisecond)

	// Stop process
	err = pm.StopCurrentProcess()
	if err != nil {
		t.Fatalf("Failed to stop process: %v", err)
	}

	// Verify process is stopped
	if pm.IsRunning() {
		t.Error("Expected process to be stopped")
	}

	if pm.GetCurrentPID() != 0 {
		t.Error("Expected PID to be 0 after stopping")
	}
}

func TestProcessManager_ProcessReplacement(t *testing.T) {
	pm := NewProcessManager()

	config1 := &config.DeployConfig{
		RunCommand:  "exit 0",
		WorkingDir:  "./",
		MaxRestarts: 0,
	}

	// Start first process
	err := pm.StartProcess(config1, "./")
	if err != nil {
		t.Fatalf("Failed to start first process: %v", err)
	}

	// Wait for it to exit
	time.Sleep(100 * time.Millisecond)

	// Start second process (should replace the first)
	err = pm.StartProcess(config1, "./")
	if err != nil {
		t.Fatalf("Failed to start second process: %v", err)
	}

	// Wait for second process to exit
	time.Sleep(100 * time.Millisecond)

	// Both should have succeeded
	// Cleanup if needed
	pm.StopCurrentProcess()
}

func TestProcessManager_RestartOnExit(t *testing.T) {
	pm := NewProcessManager()

	// Create a process that exits quickly but should restart
	deployConfig := &config.DeployConfig{
		RunCommand:   "echo 'Quick exit'; exit 1",
		WorkingDir:   "./",
		RestartDelay: 0, // Immediate restart for testing
		MaxRestarts:  2, // Allow restarts
	}

	err := pm.StartProcess(deployConfig, "./")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for all restarts to complete (process will exit and restart 3 times total)
	time.Sleep(3 * time.Second)

	// After max restarts, process should not be running
	if pm.IsRunning() {
		t.Error("Expected process to be stopped after max restarts")
	}

	// Cleanup just in case
	pm.StopCurrentProcess()
}

func TestProcessManager_NoRestartConfigured(t *testing.T) {
	pm := NewProcessManager()

	// Create a process that exits but shouldn't restart
	deployConfig := &config.DeployConfig{
		RunCommand:  "echo 'Exit without restart'; exit 0",
		WorkingDir:  "./",
		MaxRestarts: 0, // No restarts
	}

	err := pm.StartProcess(deployConfig, "./")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Give it time to exit
	time.Sleep(500 * time.Millisecond)

	// Process should not be running
	if pm.IsRunning() {
		t.Error("Expected process to be stopped when restarts are disabled")
	}
}

func TestProcessManager_GracefulShutdown(t *testing.T) {
	pm := NewProcessManager()

	deployConfig := &config.DeployConfig{
		RunCommand:  "exit 0",
		WorkingDir:  "./",
		MaxRestarts: 0,
	}

	err := pm.StartProcess(deployConfig, "./")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Give it time to exit
	time.Sleep(100 * time.Millisecond)

	// Should be stopped automatically since process exits
	if pm.IsRunning() {
		t.Error("Expected process to be stopped")
	}

	// Stop should work even if already stopped
	err = pm.StopCurrentProcess()
	if err != nil {
		t.Fatalf("Failed to stop process: %v", err)
	}
}

func TestProcessManager_StopWhenNoProcess(t *testing.T) {
	pm := NewProcessManager()

	// Should not error when stopping with no process running
	err := pm.StopCurrentProcess()
	if err != nil {
		t.Errorf("Expected no error when stopping with no process, got: %v", err)
	}

	if pm.IsRunning() {
		t.Error("Expected no process to be running")
	}

	if pm.GetCurrentPID() != 0 {
		t.Error("Expected PID to be 0")
	}
}

func TestProcessManager_InvalidCommand(t *testing.T) {
	pm := NewProcessManager()

	// Create config with invalid command
	deployConfig := &config.DeployConfig{
		RunCommand:  "nonexistent_command_12345",
		WorkingDir:  "./",
		MaxRestarts: 0,
	}

	err := pm.StartProcess(deployConfig, "./")
	// The process will start but fail immediately, which is expected behavior
	// The error handling is done through the monitoring goroutine
	if err != nil {
		t.Fatalf("Expected start to succeed (monitoring handles failure), got: %v", err)
	}

	// Wait for the process to fail and exit
	time.Sleep(500 * time.Millisecond)

	// Process should not be running after the invalid command fails
	if pm.IsRunning() {
		t.Error("Expected no process to be running after invalid command failure")
	}
}

func TestProcessManager_MultipleConcurrentStarts(t *testing.T) {
	pm := NewProcessManager()

	deployConfig := &config.DeployConfig{
		RunCommand:  "echo 'Concurrent test' && exit 0",
		WorkingDir:  "./",
		MaxRestarts: 0,
	}

	// Start multiple processes sequentially to test proper replacement
	err1 := pm.StartProcess(deployConfig, "./")
	if err1 != nil {
		t.Errorf("First start failed: %v", err1)
	}

	// Wait for first process to exit
	time.Sleep(100 * time.Millisecond)

	err2 := pm.StartProcess(deployConfig, "./")
	if err2 != nil {
		t.Errorf("Second start failed: %v", err2)
	}

	// Both should succeed since processes exit quickly, and the manager handles replacement
	// Wait for processes to exit
	time.Sleep(200 * time.Millisecond)

	// Cleanup if needed
	pm.StopCurrentProcess()
}

func TestProcessManager_Shutdown(t *testing.T) {
	pm := NewProcessManager()

	deployConfig := &config.DeployConfig{
		RunCommand:  "echo 'Shutdown test' && exit 0",
		WorkingDir:  "./",
		MaxRestarts: 0,
	}

	err := pm.StartProcess(deployConfig, "./")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for process to exit (it exits immediately)
	time.Sleep(100 * time.Millisecond)

	// Shutdown should work even if process already exited
	err = pm.Shutdown()
	if err != nil {
		t.Fatalf("Failed to shutdown: %v", err)
	}

	if pm.IsRunning() {
		t.Error("Expected process to be stopped after shutdown")
	}
}

func TestProcessManager_ProcessExitLogging(t *testing.T) {
	// This test focuses on the logging behavior when processes exit
	pm := NewProcessManager()

	deployConfig := &config.DeployConfig{
		RunCommand:  "echo 'Test process exiting'; exit 42",
		WorkingDir:  "./",
		MaxRestarts: 0, // No restarts to simplify test
	}

	err := pm.StartProcess(deployConfig, "./")
	if err != nil {
		t.Fatalf("Failed to start process: %v", err)
	}

	// Wait for process to exit
	time.Sleep(500 * time.Millisecond)

	// Process should be stopped
	if pm.IsRunning() {
		t.Error("Expected process to be stopped")
	}

	// The actual logging verification would require capturing log output,
	// which is complex for this test. We'll just verify the process stopped.
}
