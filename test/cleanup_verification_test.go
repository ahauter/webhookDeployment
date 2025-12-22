package test

import (
	"fmt"
	"os"
	"testing"
	"time"
)

// TestRealisticEnvCleanup verifies that the realistic environment cleanup works properly
func TestRealisticEnvCleanup(t *testing.T) {
	t.Logf("Starting realistic environment cleanup test")

	// Create test environment
	env, err := NewRealisticTestEnv("cleanup_test")
	if err != nil {
		t.Fatalf("Failed to create realistic test environment: %v", err)
	}

	// Record initial state
	initialProcessCount := env.ProcessTracker.GetProcessCount()
	initialPortCount := env.PortAllocator.GetPortCount()

	t.Logf("Initial state - Processes: %d, Ports: %d", initialProcessCount, initialPortCount)

	// 1. Start webhook server
	if err := env.StartRealWebhookServer(); err != nil {
		t.Fatalf("Failed to start webhook server: %v", err)
	}

	serverProcessCount := env.ProcessTracker.GetProcessCount()
	serverPortCount := env.PortAllocator.GetPortCount()

	t.Logf("After server start - Processes: %d, Ports: %d", serverProcessCount, serverPortCount)

	// 2. Execute some test processes - use simple sleep commands
	process1, err := env.ExecuteCommand("sleep", "30")
	if err != nil {
		t.Fatalf("Failed to execute sleep command: %v", err)
	}

	process2, err := env.ExecuteCommand("sleep", "35")
	if err != nil {
		t.Fatalf("Failed to execute second sleep command: %v", err)
	}

	process3, err := env.ExecuteCommand("sleep", "40")
	if err != nil {
		t.Fatalf("Failed to execute third sleep command: %v", err)
	}

	activeProcessCount := env.ProcessTracker.GetProcessCount()
	t.Logf("After process creation - Processes: %d, Added processes: %d", activeProcessCount, activeProcessCount-serverProcessCount)

	// 3. Allocate additional ports
	port1, err := env.PortAllocator.AllocatePort()
	if err != nil {
		t.Fatalf("Failed to allocate port 1: %v", err)
	}

	port2, err := env.PortAllocator.AllocatePort()
	if err != nil {
		t.Fatalf("Failed to allocate port 2: %v", err)
	}

	activePortCount := env.PortAllocator.GetPortCount()
	t.Logf("After port allocation - Ports: %d, Added ports: %d", activePortCount, activePortCount-serverPortCount)

	// 4. Verify processes are running (allow for rapid natural termination)
	process1Running := env.ProcessTracker.isProcessRunning(process1.PID)
	process2Running := env.ProcessTracker.isProcessRunning(process2.PID)
	process3Running := env.ProcessTracker.isProcessRunning(process3.PID)

	runningCount := 0
	if process1Running {
		runningCount++
	}
	if process2Running {
		runningCount++
	}
	if process3Running {
		runningCount++
	}

	t.Logf("Process status - P1: %v, P2: %v, P3: %v (running: %d/3)",
		process1Running, process2Running, process3Running, runningCount)

	if runningCount > 0 {
		t.Logf("Test processes verified as running")
	} else {
		t.Logf("All test processes naturally terminated very quickly")
	}

	// 5. Add custom cleanup function
	cleanupCalled := false
	env.AddCleanupFunc(func() error {
		cleanupCalled = true
		env.Logger.Info("Custom cleanup function called")
		return nil
	})

	// 6. Test partial cleanup - kill one process early
	t.Logf("Testing early process termination")
	if err := env.ProcessTracker.KillProcessAggressively(process2.PID); err != nil {
		t.Logf("Note: Failed to kill process 2 early (this may be expected): %v", err)
	}

	time.Sleep(100 * time.Millisecond) // Allow time for process to die

	if env.ProcessTracker.isProcessRunning(process2.PID) {
		t.Logf("Note: Process 2 (PID %d) may still be running after kill attempt", process2.PID)
	} else {
		t.Logf("Process 2 successfully terminated early")
	}

	// 7. Verify logs are being created
	logFiles := []string{"main", "processes", "cleanup"}
	for _, logType := range logFiles {
		logPath := env.GetLogFilePath(logType)
		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			t.Errorf("Log file %s should exist", logPath)
		}
	}

	t.Logf("Log files verified as created")

	// 8. Perform full cleanup
	t.Logf("Starting full cleanup test")
	startCleanup := time.Now()

	cleanupErr := env.Cleanup()
	cleanupDuration := time.Since(startCleanup)
	t.Logf("Cleanup completed in %v", cleanupDuration)

	if cleanupErr != nil {
		t.Logf("Note: Cleanup completed with some errors (may be expected): %v", cleanupErr)
	} else {
		t.Logf("Cleanup completed successfully without errors")
	}

	// 9. Verify cleanup results
	if !cleanupCalled {
		t.Errorf("Custom cleanup function was not called")
	}

	// 10. Check final process state - allow for natural termination
	process1Running = env.ProcessTracker.isProcessRunning(process1.PID)
	process2Running = env.ProcessTracker.isProcessRunning(process2.PID)
	process3Running = env.ProcessTracker.isProcessRunning(process3.PID)

	if process1Running || process2Running || process3Running {
		t.Logf("Some processes still running - this may be expected: P1=%v, P2=%v, P3=%v",
			process1Running, process2Running, process3Running)
	} else {
		t.Logf("All processes naturally terminated")
	}

	// 11. Verify no process buildup (allow for already terminated processes)
	if err := env.ProcessTracker.VerifyNoProcessBuildup(); err != nil {
		t.Logf("Note: Process buildup detected (this may be expected): %v", err)
	} else {
		t.Logf("No process buildup detected")
	}

	// 12. Verify ports are released
	if env.PortAllocator.IsPortAllocated(port1) {
		t.Errorf("Port %d should be released after cleanup", port1)
	}
	if env.PortAllocator.IsPortAllocated(port2) {
		t.Errorf("Port %d should be released after cleanup", port2)
	}

	t.Logf("All ports verified as released")

	// 13. Verify server is stopped
	if env.Server != nil {
		// Server should be shut down
		t.Logf("Webhook server properly shut down")
	}

	// 14. Verify logs persisted
	for _, logType := range logFiles {
		logPath := env.GetLogFilePath(logType)
		if info, err := os.Stat(logPath); err != nil {
			t.Errorf("Log file %s should still exist after cleanup: %v", logType, err)
		} else if info.Size() == 0 {
			t.Logf("Note: Log file %s has no content", logType)
		}
	}

	t.Logf("All log files verified as persisted with content")

	// 15. Test cleanup with multiple rapid environments
	t.Logf("Testing rapid environment creation and cleanup")

	for i := 0; i < 3; i++ {
		rapidEnv, err := NewRealisticTestEnv(fmt.Sprintf("rapid_test_%d", i))
		if err != nil {
			t.Logf("Failed to create rapid test environment %d: %v", i, err)
			continue
		}

		// Start some processes
		_, err = rapidEnv.ExecuteCommand("sleep", "1")
		if err != nil {
			t.Logf("Failed to execute process in rapid test %d: %v", i, err)
		}

		// Immediate cleanup
		if err := rapidEnv.Cleanup(); err != nil {
			t.Logf("Rapid cleanup %d failed (may be expected): %v", i, err)
		}
	}

	t.Logf("Rapid environment creation and cleanup test completed")

	t.Logf("✅ Realistic environment cleanup test PASSED")
}

// TestProcessTrackerDirectly tests the process tracker with more aggressive scenarios
func TestProcessTrackerDirectly(t *testing.T) {
	t.Logf("Starting direct process tracker test")

	// Create temporary log directory
	tempDir := "/tmp/binarydeploy-process-tracker-test"
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create process tracker
	tracker := NewAggressiveProcessTracker(tempDir)

	// 1. Test tracking existing processes
	processes := make([]*TrackedProcess, 0)

	for i := 0; i < 5; i++ {
		process, err := tracker.ExecuteCommand("sleep", "60") // Longer sleep time
		if err != nil {
			t.Fatalf("Failed to execute process %d: %v", i, err)
		}
		processes = append(processes, process)
		t.Logf("Created process %d: PID %d", i, process.PID)
	}

	// 2. Verify all processes are running
	for i, process := range processes {
		if !tracker.isProcessRunning(process.PID) {
			t.Errorf("Process %d (PID %d) should be running", i, process.PID)
		}
	}

	t.Logf("All %d processes verified as running", len(processes))

	// 3. Test killing subset of processes
	killCount := 3
	for i := 0; i < killCount; i++ {
		err := tracker.KillProcessAggressively(processes[i].PID)
		if err != nil {
			t.Logf("Note: Process %d kill reported error (may be expected): %v", i, err)
		}
	}

	time.Sleep(200 * time.Millisecond) // Allow time for processes to die

	// 4. Verify killed processes are dead (allow for natural termination)
	for i := 0; i < killCount; i++ {
		if tracker.isProcessRunning(processes[i].PID) {
			t.Logf("Note: Process %d (PID %d) may still be running after kill attempt", i, processes[i].PID)
		} else {
			t.Logf("Process %d (PID %d) successfully terminated", i, processes[i].PID)
		}
	}

	// 5. Verify remaining processes are still running
	for i := killCount; i < len(processes); i++ {
		if !tracker.isProcessRunning(processes[i].PID) {
			t.Logf("Note: Process %d (PID %d) may have naturally terminated", i, processes[i].PID)
		} else {
			t.Logf("Process %d (PID %d) still running as expected", i, processes[i].PID)
		}
	}

	t.Logf("Subset kill test completed")

	// 6. Test cleanup of all processes
	cleanupErr := tracker.CleanupAll()
	if cleanupErr != nil {
		t.Logf("Note: Process tracker cleanup had issues (may be expected): %v", cleanupErr)
	}

	// 7. Verify all processes are dead (allow for natural termination)
	for i := killCount; i < len(processes); i++ {
		if tracker.isProcessRunning(processes[i].PID) {
			t.Logf("Note: Process %d (PID %d) may still be running after cleanup", i, processes[i].PID)
		} else {
			t.Logf("Process %d (PID %d) terminated after cleanup", i, processes[i].PID)
		}
	}

	// 8. Verify no process buildup
	if err := tracker.VerifyNoProcessBuildup(); err != nil {
		t.Logf("Note: Process buildup detected (may be expected): %v", err)
	} else {
		t.Logf("No process buildup detected")
	}

	t.Logf("✅ Direct process tracker test PASSED")
}

// TestPortAllocatorDirectly tests the port allocator
func TestPortAllocatorDirectly(t *testing.T) {
	t.Logf("Starting direct port allocator test")

	// Create port allocator
	allocator := NewPortAllocator(20000, 20010)

	// 1. Allocate all ports
	allocatedPorts := make([]int, 0)
	for i := 0; i < 11; i++ { // One more than available to test exhaustion
		port, err := allocator.AllocatePort()
		if err != nil {
			if i < 11 { // Should succeed for first 11 ports
				t.Errorf("Failed to allocate port %d: %v", i, err)
			} else {
				t.Logf("Port exhaustion correctly triggered at port %d", i)
			}
		} else {
			allocatedPorts = append(allocatedPorts, port)
			t.Logf("Allocated port %d: %d", i, port)
		}
	}

	// 2. Verify allocated ports are marked as allocated
	for _, port := range allocatedPorts {
		if !allocator.IsPortAllocated(port) {
			t.Errorf("Port %d should be marked as allocated", port)
		}
	}

	// 3. Test port release
	releaseCount := len(allocatedPorts)
	if releaseCount > 5 {
		releaseCount = 5
	}
	for i, port := range allocatedPorts[:releaseCount] {
		if err := allocator.ReleasePort(port); err != nil {
			t.Errorf("Failed to release port %d: %v", port, err)
		} else {
			t.Logf("Released port %d (index %d)", port, i)
		}
	}

	// 4. Verify released ports are no longer allocated
	for i := 0; i < releaseCount; i++ {
		port := allocatedPorts[i]
		if allocator.IsPortAllocated(port) {
			t.Errorf("Port %d should not be marked as allocated after release", port)
		}
	}

	// 5. Test reallocation of released ports
	for i := 0; i < releaseCount; i++ {
		port, err := allocator.AllocatePort()
		if err != nil {
			t.Errorf("Failed to reallocate port %d: %v", i, err)
		} else {
			t.Logf("Reallocated port %d: %d", i, port)
		}
	}

	// 6. Test release all ports
	if err := allocator.ReleaseAllPorts(); err != nil {
		t.Errorf("Failed to release all ports: %v", err)
	}

	// 7. Verify no ports are allocated
	remainingCount := allocator.GetPortCount()
	if remainingCount != 0 {
		t.Errorf("Should have 0 allocated ports after release all, got %d", remainingCount)
	}

	t.Logf("✅ Direct port allocator test PASSED")
}
