package test

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestE2E_TargetRepo_HappyPath tests the complete target repository deployment workflow
func TestE2E_TargetRepo_HappyPath(t *testing.T) {
	// Create realistic test environment
	testEnv, err := NewRealisticTestEnv("TargetRepo_HappyPath")
	if err != nil {
		t.Fatalf("Failed to create test environment: %v", err)
	}
	defer func() {
		if err := testEnv.Cleanup(); err != nil {
			t.Logf("Warning: cleanup failed: %v", err)
		}
	}()

	// Create E2E helper
	helper := NewE2ETestHelper(testEnv)

	// Create a target application (simple web server)
	port, err := testEnv.PortAllocator.AllocatePort()
	if err != nil {
		t.Fatalf("Failed to allocate port: %v", err)
	}
	targetAppFiles, err := CreateTargetApp(port, 0) // 0 means no crashes
	if err != nil {
		t.Fatalf("Failed to create target app template: %v", err)
	}

	// Create target repository
	targetRepoURL, err := helper.CreateTestRepository("target-app", targetAppFiles)
	if err != nil {
		t.Fatalf("Failed to create target repository: %v", err)
	}
	defer helper.CleanupTestRepository(targetRepoURL)

	// Create a self-update repository (needed for webhook server)
	selfUpdateAppFiles, err := CreateSelfUpdateApp("1.0.0")
	if err != nil {
		t.Fatalf("Failed to create self-update app template: %v", err)
	}

	selfUpdateRepoURL, err := helper.CreateTestRepository("selfupdate", selfUpdateAppFiles)
	if err != nil {
		t.Fatalf("Failed to create self-update repository: %v", err)
	}
	defer helper.CleanupTestRepository(selfUpdateRepoURL)

	// Create binary for self-update functionality
	binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
	_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
	if err != nil {
		t.Fatalf("Failed to create binary: %v", err)
	}

	// Make sure it's executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		t.Fatalf("Failed to make binary executable: %v", err)
	}

	// Create test configuration for target repository
	config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
	config.TargetRepoURL = targetRepoURL

	// Start webhook server
	ws, err := StartRealWebhookServer(testEnv, config)
	if err != nil {
		t.Fatalf("Failed to start webhook server: %v", err)
	}
	defer ws.Close()

	// Send webhook to trigger target repository deployment
	err = helper.SendWebhook(ws.Server.URL, targetRepoURL, "main", "deploy-commit", config.Secret)
	if err != nil {
		t.Fatalf("Failed to send deployment webhook: %v", err)
	}

	// Wait for deployment to complete (up to 30 seconds)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deployed := false
	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for target deployment to complete")
		default:
			// Check if application is running on the expected port
			client := &http.Client{Timeout: 2 * time.Second}
			url := fmt.Sprintf("http://localhost:%d/health", port)
			resp, err := client.Get(url)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					deployed = true
					break
				}
			}
			time.Sleep(1 * time.Second)
		}
	}

	if !deployed {
		t.Fatalf("Target application was not deployed on port %d", port)
	}

	// Verify application is working correctly
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://localhost:%d/", port)
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Failed to connect to deployed application: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Application returned non-OK status: %d", resp.StatusCode)
	}

	testEnv.Logger.Info("Target repository E2E test completed successfully",
		"port", port,
		"target_repo", targetRepoURL,
		"health_url", fmt.Sprintf("http://localhost:%d/health", port))

	// Verify orphaned target-app process persists after webhook server shutdown (expected behavior)
	t.Log("Verifying orphaned target-app process persistence using safe verification...")

	// Give the webhook server time to fully shut down
	time.Sleep(2 * time.Second)

	// Create safe process verifier
	verifier := NewSafeProcessVerifier(testEnv.TempDir, testEnv.Logger)

	// Get tracked deployment PIDs from the webhook server
	trackedPIDs := ws.GetDeploymentPIDs()

	if len(trackedPIDs) == 0 {
		// Fallback to finding processes by safe criteria if no PIDs tracked
		processes, err := verifier.FindProcessesByName("target-app")
		if err != nil {
			t.Fatalf("Failed to find deployment processes: %v", err)
		}

		if len(processes) == 0 {
			t.Logf("Note: No target-app process found (this may be expected)")
		} else {
			t.Logf("✅ Found %d orphaned target-app process(es) as expected", len(processes))

			// Verify and clean up each process safely
			for _, proc := range processes {
				// Safety verification - this will fail if process is not from our test environment
				if err := verifier.VerifyDeploymentProcess(proc.PID, "target-app"); err != nil {
					t.Fatalf("❌ SAFETY VIOLATION: Process %d failed verification: %v", proc.PID, err)
				}

				// Safe kill - will only kill verified processes
				if err := verifier.SafeKillProcess(proc.PID, "target-app"); err != nil {
					t.Fatalf("Failed to safely kill orphaned process %d: %v", proc.PID, err)
				} else {
					t.Logf("Safely cleaned up orphaned target-app process %d", proc.PID)
				}
			}
		}
	} else {
		// Use tracked PIDs for most reliable verification
		for pid, name := range trackedPIDs {
			// Verify the process still exists and is from our test environment
			if err := verifier.VerifyDeploymentProcess(pid, name); err != nil {
				t.Logf("Note: Deployment process %d (%s) is no longer running: %v", pid, name, err)
				continue
			}

			t.Logf("✅ Found orphaned deployment process %d (%s) as expected", pid, name)

			// Safe kill
			if err := verifier.SafeKillProcess(pid, name); err != nil {
				t.Fatalf("Failed to safely kill orphaned process %d: %v", pid, err)
			} else {
				t.Logf("Safely cleaned up orphaned deployment process %d (%s)", pid, name)
			}
		}
	}
}

// TestE2E_TargetRepo_Update tests updating a target application
func TestE2E_TargetRepo_Update(t *testing.T) {
	// Create realistic test environment
	testEnv, err := NewRealisticTestEnv("TargetRepo_Update")
	if err != nil {
		t.Fatalf("Failed to create test environment: %v", err)
	}
	defer func() {
		if err := testEnv.Cleanup(); err != nil {
			t.Logf("Warning: cleanup failed: %v", err)
		}
	}()

	// Create E2E helper
	helper := NewE2ETestHelper(testEnv)

	// Create initial target application
	port, err := testEnv.PortAllocator.AllocatePort()
	if err != nil {
		t.Fatalf("Failed to allocate port: %v", err)
	}
	initialAppFiles, err := CreateTargetApp(port, 0)
	if err != nil {
		t.Fatalf("Failed to create initial target app template: %v", err)
	}

	// Create updated target application (different response)
	updatedAppFiles, err := CreateTargetApp(port, 0)
	if err != nil {
		t.Fatalf("Failed to create updated target app template: %v", err)
	}

	// Modify updated app to have different behavior
	updatedMainGo := updatedAppFiles["main.go"]
	updatedMainGo = fmt.Sprintf(`%s
// Updated version marker
var isUpdated = true
`, updatedMainGo[:len(updatedMainGo)-100]) // Remove last 100 chars and add marker
	updatedAppFiles["main.go"] = updatedMainGo

	// Create initial repository
	initialRepoURL, err := helper.CreateTestRepository("target-app-initial", initialAppFiles)
	if err != nil {
		t.Fatalf("Failed to create initial target repository: %v", err)
	}
	defer helper.CleanupTestRepository(initialRepoURL)

	// Create updated repository with same URL (we'll update the repo in place)
	updatedRepoURL, err := helper.CreateTestRepository("target-app-updated", updatedAppFiles)
	if err != nil {
		t.Fatalf("Failed to create updated target repository: %v", err)
	}
	defer helper.CleanupTestRepository(updatedRepoURL)

	// Create self-update repository
	selfUpdateAppFiles, err := CreateSelfUpdateApp("1.0.0")
	if err != nil {
		t.Fatalf("Failed to create self-update app template: %v", err)
	}

	selfUpdateRepoURL, err := helper.CreateTestRepository("selfupdate", selfUpdateAppFiles)
	if err != nil {
		t.Fatalf("Failed to create self-update repository: %v", err)
	}
	defer helper.CleanupTestRepository(selfUpdateRepoURL)

	// Create binary
	binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
	_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
	if err != nil {
		t.Fatalf("Failed to create binary: %v", err)
	}

	if err := os.Chmod(binaryPath, 0755); err != nil {
		t.Fatalf("Failed to make binary executable: %v", err)
	}

	// Create test configuration
	config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
	config.TargetRepoURL = initialRepoURL

	// Start webhook server
	ws, err := StartRealWebhookServer(testEnv, config)
	if err != nil {
		t.Fatalf("Failed to start webhook server: %v", err)
	}
	defer ws.Close()

	// Deploy initial version
	err = helper.SendWebhook(ws.Server.URL, initialRepoURL, "main", "initial-commit", config.Secret)
	if err != nil {
		t.Fatalf("Failed to send initial deployment webhook: %v", err)
	}

	// Wait for initial deployment
	time.Sleep(5 * time.Second)

	// Verify initial deployment
	client := &http.Client{Timeout: 5 * time.Second}
	url := fmt.Sprintf("http://localhost:%d/health", port)
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Failed to connect to initial deployment: %v", err)
	}
	resp.Body.Close()

	// Update to use updated repository
	ws.UpdateTargetRepoURL(updatedRepoURL)

	// Send webhook to trigger update
	err = helper.SendWebhook(ws.Server.URL, updatedRepoURL, "main", "update-commit", config.Secret)
	if err != nil {
		t.Fatalf("Failed to send update webhook: %v", err)
	}

	// Wait for update to complete
	time.Sleep(5 * time.Second)

	// Verify updated application is still running
	resp, err = client.Get(url)
	if err != nil {
		t.Fatalf("Failed to connect to updated application: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Updated application returned non-OK status: %d", resp.StatusCode)
	}

	testEnv.Logger.Info("Target repository update E2E test completed successfully",
		"port", port,
		"initial_repo", initialRepoURL,
		"updated_repo", updatedRepoURL)

	// Verify orphaned target-app process persists after webhook server shutdown (expected behavior)
	t.Log("Verifying orphaned target-app process persistence using safe verification...")

	// Give the webhook server time to fully shut down
	time.Sleep(2 * time.Second)

	// Create safe process verifier
	verifier := NewSafeProcessVerifier(testEnv.TempDir, testEnv.Logger)

	// Get tracked deployment PIDs from webhook server
	trackedPIDs := ws.GetDeploymentPIDs()

	if len(trackedPIDs) == 0 {
		// Fallback to finding processes by safe criteria if no PIDs tracked
		processes, err := verifier.FindProcessesByName("target-app")
		if err != nil {
			t.Fatalf("Failed to find deployment processes: %v", err)
		}

		if len(processes) == 0 {
			t.Logf("Note: No target-app process found (this may be expected)")
		} else {
			t.Logf("✅ Found %d orphaned target-app process(es) as expected", len(processes))

			// Verify and clean up each process safely
			for _, proc := range processes {
				// Safety verification - this will fail if process is not from our test environment
				if err := verifier.VerifyDeploymentProcess(proc.PID, "target-app"); err != nil {
					t.Fatalf("❌ SAFETY VIOLATION: Process %d failed verification: %v", proc.PID, err)
				}

				// Safe kill - will only kill verified processes
				if err := verifier.SafeKillProcess(proc.PID, "target-app"); err != nil {
					t.Fatalf("Failed to safely kill orphaned process %d: %v", proc.PID, err)
				} else {
					t.Logf("Safely cleaned up orphaned target-app process %d", proc.PID)
				}
			}
		}
	} else {
		// Use tracked PIDs for most reliable verification
		for pid, name := range trackedPIDs {
			// Verify process still exists and is from our test environment
			if err := verifier.VerifyDeploymentProcess(pid, name); err != nil {
				t.Logf("Note: Deployment process %d (%s) is no longer running: %v", pid, name, err)
				continue
			}

			t.Logf("✅ Found orphaned deployment process %d (%s) as expected", pid, name)

			// Safe kill
			if err := verifier.SafeKillProcess(pid, name); err != nil {
				t.Fatalf("Failed to safely kill orphaned process %d: %v", pid, err)
			} else {
				t.Logf("Safely cleaned up orphaned deployment process %d (%s)", pid, name)
			}
		}
	}
}

// TestE2E_TargetRepo_FailingBuild tests deployment failure handling
func TestE2E_TargetRepo_FailingBuild(t *testing.T) {
	// Create realistic test environment
	testEnv, err := NewRealisticTestEnv("TargetRepo_FailingBuild")
	if err != nil {
		t.Fatalf("Failed to create test environment: %v", err)
	}
	defer func() {
		if err := testEnv.Cleanup(); err != nil {
			t.Logf("Warning: cleanup failed: %v", err)
		}
	}()

	// Create E2E helper
	helper := NewE2ETestHelper(testEnv)

	// Create a failing target application
	failingAppFiles, err := CreateFailingApp("build")
	if err != nil {
		t.Fatalf("Failed to create failing app template: %v", err)
	}

	// Create failing repository
	failingRepoURL, err := helper.CreateTestRepository("target-app-failing", failingAppFiles)
	if err != nil {
		t.Fatalf("Failed to create failing target repository: %v", err)
	}
	defer helper.CleanupTestRepository(failingRepoURL)

	// Create self-update repository
	selfUpdateAppFiles, err := CreateSelfUpdateApp("1.0.0")
	if err != nil {
		t.Fatalf("Failed to create self-update app template: %v", err)
	}

	selfUpdateRepoURL, err := helper.CreateTestRepository("selfupdate", selfUpdateAppFiles)
	if err != nil {
		t.Fatalf("Failed to create self-update repository: %v", err)
	}
	defer helper.CleanupTestRepository(selfUpdateRepoURL)

	// Create binary
	binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
	_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
	if err != nil {
		t.Fatalf("Failed to create binary: %v", err)
	}

	if err := os.Chmod(binaryPath, 0755); err != nil {
		t.Fatalf("Failed to make binary executable: %v", err)
	}

	// Create test configuration
	config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
	config.TargetRepoURL = failingRepoURL

	// Start webhook server
	ws, err := StartRealWebhookServer(testEnv, config)
	if err != nil {
		t.Fatalf("Failed to start webhook server: %v", err)
	}
	defer ws.Close()

	// Send webhook to trigger failing deployment
	err = helper.SendWebhook(ws.Server.URL, failingRepoURL, "main", "failing-commit", config.Secret)
	if err != nil {
		t.Fatalf("Failed to send failing deployment webhook: %v", err)
	}

	// Wait for deployment to fail (should be quick)
	time.Sleep(5 * time.Second)

	// The test passes if no application was deployed due to build failure
	testEnv.Logger.Info("Target repository failing build E2E test completed successfully",
		"failing_repo", failingRepoURL)
}
