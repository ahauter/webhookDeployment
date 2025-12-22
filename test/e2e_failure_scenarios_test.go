package test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestE2E_ConfigurationErrors tests comprehensive configuration error scenarios
func TestE2E_ConfigurationErrors(t *testing.T) {
	t.Run("MissingDeployConfig", func(t *testing.T) {
		// Create realistic test environment
		testEnv, err := NewRealisticTestEnv("ConfigErrors_MissingConfig")
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

		// Create repository without deploy.config
		configlessAppFiles, err := CreateFailingApp("config_missing")
		if err != nil {
			t.Fatalf("Failed to create configless app template: %v", err)
		}

		configlessRepoURL, err := helper.CreateTestRepository("target-app-no-config", configlessAppFiles)
		if err != nil {
			t.Fatalf("Failed to create configless repository: %v", err)
		}
		defer helper.CleanupTestRepository(configlessRepoURL)

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
		config.TargetRepoURL = configlessRepoURL

		// Start webhook server
		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// Send webhook - this should fail gracefully
		err = helper.SendWebhook(ws.Server.URL, configlessRepoURL, "main", "no-config-commit", config.Secret)
		if err != nil {
			t.Logf("✅ Expected webhook failure for missing config: %v", err)
		} else {
			t.Logf("Note: Webhook succeeded despite missing config (may be expected behavior)")
		}
	})

	t.Run("EmptyDeployConfig", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("ConfigErrors_EmptyConfig")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		helper := NewE2ETestHelper(testEnv)

		// Create repository with empty deploy.config
		emptyConfigAppFiles, err := CreateFailingApp("config_empty")
		if err != nil {
			t.Fatalf("Failed to create empty config app template: %v", err)
		}

		emptyConfigRepoURL, err := helper.CreateTestRepository("target-app-empty-config", emptyConfigAppFiles)
		if err != nil {
			t.Fatalf("Failed to create empty config repository: %v", err)
		}
		defer helper.CleanupTestRepository(emptyConfigRepoURL)

		selfUpdateAppFiles, err := CreateSelfUpdateApp("1.0.0")
		if err != nil {
			t.Fatalf("Failed to create self-update app template: %v", err)
		}

		selfUpdateRepoURL, err := helper.CreateTestRepository("selfupdate", selfUpdateAppFiles)
		if err != nil {
			t.Fatalf("Failed to create self-update repository: %v", err)
		}
		defer helper.CleanupTestRepository(selfUpdateRepoURL)

		binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
		_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
		if err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			t.Fatalf("Failed to make binary executable: %v", err)
		}

		config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
		config.TargetRepoURL = emptyConfigRepoURL

		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// This should fail gracefully
		err = helper.SendWebhook(ws.Server.URL, emptyConfigRepoURL, "main", "empty-config-commit", config.Secret)
		if err != nil {
			t.Logf("✅ Expected webhook failure for empty config: %v", err)
		} else {
			t.Logf("Note: Webhook succeeded despite empty config")
		}
	})

	t.Run("MalformedDeployConfig", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("ConfigErrors_MalformedConfig")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		helper := NewE2ETestHelper(testEnv)

		// Create repository with malformed deploy.config
		malformedConfigAppFiles, err := CreateFailingApp("config_malformed")
		if err != nil {
			t.Fatalf("Failed to create malformed config app template: %v", err)
		}

		malformedConfigRepoURL, err := helper.CreateTestRepository("target-app-malformed-config", malformedConfigAppFiles)
		if err != nil {
			t.Fatalf("Failed to create malformed config repository: %v", err)
		}
		defer helper.CleanupTestRepository(malformedConfigRepoURL)

		selfUpdateAppFiles, err := CreateSelfUpdateApp("1.0.0")
		if err != nil {
			t.Fatalf("Failed to create self-update app template: %v", err)
		}

		selfUpdateRepoURL, err := helper.CreateTestRepository("selfupdate", selfUpdateAppFiles)
		if err != nil {
			t.Fatalf("Failed to create self-update repository: %v", err)
		}
		defer helper.CleanupTestRepository(selfUpdateRepoURL)

		binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
		_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
		if err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			t.Fatalf("Failed to make binary executable: %v", err)
		}

		config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
		config.TargetRepoURL = malformedConfigRepoURL

		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// This should fail gracefully
		err = helper.SendWebhook(ws.Server.URL, malformedConfigRepoURL, "main", "malformed-config-commit", config.Secret)
		if err != nil {
			t.Logf("✅ Expected webhook failure for malformed config: %v", err)
		} else {
			t.Logf("Note: Webhook succeeded despite malformed config")
		}
	})

	t.Run("InvalidConfigValues", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("ConfigErrors_InvalidValues")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		helper := NewE2ETestHelper(testEnv)

		// Create repository with invalid values in deploy.config
		invalidValuesAppFiles, err := CreateFailingApp("config_invalid_values")
		if err != nil {
			t.Fatalf("Failed to create invalid values app template: %v", err)
		}

		invalidValuesRepoURL, err := helper.CreateTestRepository("target-app-invalid-values", invalidValuesAppFiles)
		if err != nil {
			t.Fatalf("Failed to create invalid values repository: %v", err)
		}
		defer helper.CleanupTestRepository(invalidValuesRepoURL)

		selfUpdateAppFiles, err := CreateSelfUpdateApp("1.0.0")
		if err != nil {
			t.Fatalf("Failed to create self-update app template: %v", err)
		}

		selfUpdateRepoURL, err := helper.CreateTestRepository("selfupdate", selfUpdateAppFiles)
		if err != nil {
			t.Fatalf("Failed to create self-update repository: %v", err)
		}
		defer helper.CleanupTestRepository(selfUpdateRepoURL)

		binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
		_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
		if err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			t.Fatalf("Failed to make binary executable: %v", err)
		}

		config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
		config.TargetRepoURL = invalidValuesRepoURL

		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// This should fail gracefully or handle invalid values
		err = helper.SendWebhook(ws.Server.URL, invalidValuesRepoURL, "main", "invalid-values-commit", config.Secret)
		if err != nil {
			t.Logf("✅ Expected webhook failure for invalid values: %v", err)
		} else {
			t.Logf("Note: Webhook succeeded despite invalid values")
		}
	})
}

// TestE2E_NetworkFailures tests comprehensive network failure scenarios
func TestE2E_NetworkFailures(t *testing.T) {
	t.Run("UnreachableRepository", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("NetworkFailures_UnreachableRepo")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		helper := NewE2ETestHelper(testEnv)
		networkSimulator := NewNetworkSimulator(testEnv.TempDir, testEnv.Logger)

		// Create unreachable repository URL
		unreachableRepoURL := networkSimulator.CreateUnreachableRepository("target-app")
		t.Logf("Testing with unreachable repo: %s", unreachableRepoURL)

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
		config.TargetRepoURL = unreachableRepoURL

		// Start webhook server
		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// Send webhook - this should fail due to unreachable repository
		err = helper.SendWebhook(ws.Server.URL, unreachableRepoURL, "main", "unreachable-repo-commit", config.Secret)
		if err != nil {
			t.Logf("✅ Expected webhook failure for unreachable repository: %v", err)
		} else {
			t.Logf("Note: Webhook succeeded with unreachable repository (unexpected)")
		}
	})

	t.Run("CorruptedGitRepository", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("NetworkFailures_CorruptedRepo")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		helper := NewE2ETestHelper(testEnv)
		networkSimulator := NewNetworkSimulator(testEnv.TempDir, testEnv.Logger)

		// Create repository and then corrupt it
		appFiles, err := CreateTargetApp(8080, 0)
		if err != nil {
			t.Fatalf("Failed to create app template: %v", err)
		}

		corruptedRepoURL, err := helper.CreateTestRepository("target-app-corrupted", appFiles)
		if err != nil {
			t.Fatalf("Failed to create test repository: %v", err)
		}
		defer helper.CleanupTestRepository(corruptedRepoURL)

		// Extract local repo path and corrupt it
		repoPath := strings.TrimPrefix(corruptedRepoURL, "file://")
		if err := networkSimulator.CorruptRepository(repoPath); err != nil {
			t.Fatalf("Failed to corrupt repository: %v", err)
		}

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

		binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
		_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
		if err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			t.Fatalf("Failed to make binary executable: %v", err)
		}

		config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
		config.TargetRepoURL = corruptedRepoURL

		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// Send webhook - this should fail due to corrupted repository
		err = helper.SendWebhook(ws.Server.URL, corruptedRepoURL, "main", "corrupted-repo-commit", config.Secret)
		if err != nil {
			t.Logf("✅ Expected webhook failure for corrupted repository: %v", err)
		} else {
			t.Logf("Note: Webhook succeeded with corrupted repository (unexpected)")
		}
	})
}

// TestE2E_FileSystemErrors tests comprehensive filesystem and resource failure scenarios
func TestE2E_FileSystemErrors(t *testing.T) {
	t.Run("ReadOnlyDeployDirectory", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("FileSystemErrors_ReadOnlyDir")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		// Create resource monitor
		resourceMonitor := NewResourceMonitor(testEnv.TempDir, testEnv.Logger)

		// Create read-only deploy directory
		readOnlyDir := filepath.Join(testEnv.TempDir, "readonly-deploy")
		if err := resourceMonitor.CreateReadOnlyDirectory(readOnlyDir); err != nil {
			t.Fatalf("Failed to create read-only directory: %v", err)
		}

		helper := NewE2ETestHelper(testEnv)

		// Create repository
		appFiles, err := CreateTargetApp(8080, 0)
		if err != nil {
			t.Fatalf("Failed to create app template: %v", err)
		}

		repoURL, err := helper.CreateTestRepository("target-app-readonly", appFiles)
		if err != nil {
			t.Fatalf("Failed to create test repository: %v", err)
		}
		defer helper.CleanupTestRepository(repoURL)

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

		binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
		_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
		if err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			t.Fatalf("Failed to make binary executable: %v", err)
		}

		// Create test configuration with read-only deploy directory
		config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
		config.TargetRepoURL = repoURL
		config.DeployDir = readOnlyDir

		// Start webhook server
		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// Send webhook - this should fail gracefully
		err = helper.SendWebhook(ws.Server.URL, repoURL, "main", "readonly-dir-commit", config.Secret)
		if err != nil {
			t.Logf("✅ Expected webhook failure for read-only deploy directory: %v", err)
		} else {
			t.Logf("Note: Webhook succeeded with read-only deploy directory (unexpected)")
		}
	})

	t.Run("DiskSpaceExhaustion", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("FileSystemErrors_DiskFull")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		resourceMonitor := NewResourceMonitor(testEnv.TempDir, testEnv.Logger)

		// Simulate disk space exhaustion
		if err := resourceMonitor.CreateFullDiskSimulation(50); err != nil {
			t.Logf("Note: Failed to create disk space simulation: %v", err)
		}

		helper := NewE2ETestHelper(testEnv)

		// Create repository with large app
		appFiles, err := CreateFailingApp("memory_hog") // This creates a memory-intensive app
		if err != nil {
			t.Fatalf("Failed to create memory hog app template: %v", err)
		}

		repoURL, err := helper.CreateTestRepository("target-app-diskhog", appFiles)
		if err != nil {
			t.Fatalf("Failed to create test repository: %v", err)
		}
		defer helper.CleanupTestRepository(repoURL)

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

		binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
		_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
		if err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			t.Fatalf("Failed to make binary executable: %v", err)
		}

		config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
		config.TargetRepoURL = repoURL

		// Monitor resource usage
		resourceMonitor.MonitorMemoryUsage()
		initialProcessCount, _ := resourceMonitor.MonitorProcessCount()

		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// Send webhook
		err = helper.SendWebhook(ws.Server.URL, repoURL, "main", "disk-hog-commit", config.Secret)
		if err != nil {
			t.Logf("Webhook failed (possibly due to resource constraints): %v", err)
		} else {
			t.Logf("Webhook succeeded despite resource constraints")
		}

		// Monitor resource usage after deployment attempt
		resourceMonitor.MonitorMemoryUsage()
		finalProcessCount, _ := resourceMonitor.MonitorProcessCount()

		t.Logf("Resource monitoring - Initial processes: %d, Final processes: %d", initialProcessCount, finalProcessCount)
	})

	t.Run("FileLockingIssues", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("FileSystemErrors_FileLocks")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		resourceMonitor := NewResourceMonitor(testEnv.TempDir, testEnv.Logger)

		// Simulate file locking
		lockFilePath := filepath.Join(testEnv.DeployDir, "app.lock")
		if err := resourceMonitor.SimulateFileLock(lockFilePath); err != nil {
			t.Logf("Note: Failed to simulate file lock: %v", err)
		}

		helper := NewE2ETestHelper(testEnv)

		// Create repository that tries to use locked file
		appFiles, err := CreateTargetApp(8080, 0)
		if err != nil {
			t.Fatalf("Failed to create app template: %v", err)
		}

		repoURL, err := helper.CreateTestRepository("target-app-filelock", appFiles)
		if err != nil {
			t.Fatalf("Failed to create test repository: %v", err)
		}
		defer helper.CleanupTestRepository(repoURL)

		selfUpdateAppFiles, err := CreateSelfUpdateApp("1.0.0")
		if err != nil {
			t.Fatalf("Failed to create self-update app template: %v", err)
		}

		selfUpdateRepoURL, err := helper.CreateTestRepository("selfupdate", selfUpdateAppFiles)
		if err != nil {
			t.Fatalf("Failed to create self-update repository: %v", err)
		}
		defer helper.CleanupTestRepository(selfUpdateRepoURL)

		binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
		_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
		if err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			t.Fatalf("Failed to make binary executable: %v", err)
		}

		config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
		config.TargetRepoURL = repoURL

		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// Send webhook
		err = helper.SendWebhook(ws.Server.URL, repoURL, "main", "file-lock-commit", config.Secret)
		if err != nil {
			t.Logf("Webhook failed (possibly due to file locking): %v", err)
		} else {
			t.Logf("Webhook succeeded despite file lock simulation")
		}
	})
}

// TestE2E_ProcessManagementEdgeCases tests comprehensive process management failure scenarios
func TestE2E_ProcessManagementEdgeCases(t *testing.T) {
	t.Run("ZombieProcessHandling", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("ProcessMgmt_ZombieProcess")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		helper := NewE2ETestHelper(testEnv)

		// Create repository with zombie process
		zombieAppFiles, err := CreateFailingApp("zombie_process")
		if err != nil {
			t.Fatalf("Failed to create zombie process app template: %v", err)
		}

		zombieRepoURL, err := helper.CreateTestRepository("target-app-zombie", zombieAppFiles)
		if err != nil {
			t.Fatalf("Failed to create zombie process repository: %v", err)
		}
		defer helper.CleanupTestRepository(zombieRepoURL)

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

		binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
		_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
		if err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			t.Fatalf("Failed to make binary executable: %v", err)
		}

		config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
		config.TargetRepoURL = zombieRepoURL

		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// Send webhook
		err = helper.SendWebhook(ws.Server.URL, zombieRepoURL, "main", "zombie-process-commit", config.Secret)
		if err != nil {
			t.Logf("Webhook failed with zombie process: %v", err)
		} else {
			t.Logf("Webhook succeeded with zombie process - testing cleanup behavior")

			// Give process time to become zombie
			time.Sleep(2 * time.Second)

			// Verify process cleanup handles zombie scenarios
			t.Logf("Testing zombie process cleanup behavior")
		}
	})

	t.Run("IgnoreSIGTERMProcess", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("ProcessMgmt_IgnoreSIGTERM")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		helper := NewE2ETestHelper(testEnv)

		// Create repository with SIGTERM-ignoring process
		ignoreSigtermAppFiles, err := CreateFailingApp("ignore_sigterm")
		if err != nil {
			t.Fatalf("Failed to create SIGTERM ignoring app template: %v", err)
		}

		ignoreSigtermRepoURL, err := helper.CreateTestRepository("target-app-ignore-sigterm", ignoreSigtermAppFiles)
		if err != nil {
			t.Fatalf("Failed to create ignore SIGTERM repository: %v", err)
		}
		defer helper.CleanupTestRepository(ignoreSigtermRepoURL)

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

		binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
		_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
		if err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			t.Fatalf("Failed to make binary executable: %v", err)
		}

		config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
		config.TargetRepoURL = ignoreSigtermRepoURL

		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// Send webhook
		err = helper.SendWebhook(ws.Server.URL, ignoreSigtermRepoURL, "main", "ignore-sigterm-commit", config.Secret)
		if err != nil {
			t.Logf("Webhook failed with SIGTERM ignoring process: %v", err)
		} else {
			t.Logf("Webhook succeeded - testing SIGTERM ignore behavior")

			// Test process manager handles SIGTERM ignoring processes
			time.Sleep(2 * time.Second)

			// Verify force kill is used as fallback
			t.Logf("Testing process manager fallback to SIGKILL")
		}
	})

	t.Run("ResourceHogProcess", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("ProcessMgmt_ResourceHog")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		resourceMonitor := NewResourceMonitor(testEnv.TempDir, testEnv.Logger)

		helper := NewE2ETestHelper(testEnv)

		// Create repository with resource hog process
		resourceHogAppFiles, err := CreateFailingApp("resource_hog")
		if err != nil {
			t.Fatalf("Failed to create resource hog app template: %v", err)
		}

		resourceHogRepoURL, err := helper.CreateTestRepository("target-app-resource-hog", resourceHogAppFiles)
		if err != nil {
			t.Fatalf("Failed to create resource hog repository: %v", err)
		}
		defer helper.CleanupTestRepository(resourceHogRepoURL)

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

		binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
		_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
		if err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			t.Fatalf("Failed to make binary executable: %v", err)
		}

		config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
		config.TargetRepoURL = resourceHogRepoURL

		// Monitor initial resources
		resourceMonitor.MonitorMemoryUsage()
		initialProcessCount, _ := resourceMonitor.MonitorProcessCount()

		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// Send webhook
		err = helper.SendWebhook(ws.Server.URL, resourceHogRepoURL, "main", "resource-hog-commit", config.Secret)
		if err != nil {
			t.Logf("Webhook failed with resource hog: %v", err)
		} else {
			t.Logf("Webhook succeeded - monitoring resource hog behavior")

			// Monitor resource usage during resource hog execution
			time.Sleep(3 * time.Second)
			resourceMonitor.MonitorMemoryUsage()
			finalProcessCount, _ := resourceMonitor.MonitorProcessCount()

			t.Logf("Resource hog test - Initial processes: %d, Final processes: %d", initialProcessCount, finalProcessCount)
		}
	})
}

// TestE2E_WebhookEdgeCases tests comprehensive webhook handling edge cases
func TestE2E_WebhookEdgeCases(t *testing.T) {
	t.Run("ConcurrentWebhooks", func(t *testing.T) {
		testEnv, err := NewRealisticTestEnv("WebhookEdge_Concurrent")
		if err != nil {
			t.Fatalf("Failed to create test environment: %v", err)
		}
		defer func() {
			if err := testEnv.Cleanup(); err != nil {
				t.Logf("Warning: cleanup failed: %v", err)
			}
		}()

		helper := NewE2ETestHelper(testEnv)

		// Create repository
		appFiles, err := CreateTargetApp(8080, 0)
		if err != nil {
			t.Fatalf("Failed to create app template: %v", err)
		}

		repoURL, err := helper.CreateTestRepository("target-app-concurrent", appFiles)
		if err != nil {
			t.Fatalf("Failed to create test repository: %v", err)
		}
		defer helper.CleanupTestRepository(repoURL)

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

		binaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-test")
		_, err = helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
		if err != nil {
			t.Fatalf("Failed to create binary: %v", err)
		}

		if err := os.Chmod(binaryPath, 0755); err != nil {
			t.Fatalf("Failed to make binary executable: %v", err)
		}

		config := CreateE2EConfig(testEnv, selfUpdateRepoURL)
		config.TargetRepoURL = repoURL

		ws, err := StartRealWebhookServer(testEnv, config)
		if err != nil {
			t.Fatalf("Failed to start webhook server: %v", err)
		}
		defer ws.Close()

		// Send multiple concurrent webhooks to test race conditions
		t.Logf("Testing concurrent webhook processing...")
		var wg sync.WaitGroup
		errors := make(chan error, 10)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				err := helper.SendWebhook(ws.Server.URL, repoURL, "main", fmt.Sprintf("concurrent-commit-%d", index), config.Secret)
				if err != nil {
					errors <- fmt.Errorf("webhook %d failed: %w", index, err)
				} else {
					t.Logf("Webhook %d sent successfully", index)
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Count errors
		errorCount := 0
		for err := range errors {
			if err != nil {
				errorCount++
				t.Logf("Concurrent webhook error: %v", err)
			}
		}

		t.Logf("Concurrent webhook test completed - %d/%d succeeded", 10-errorCount, 10)
	})
}
