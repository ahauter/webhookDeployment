package test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"binaryDeploy/updater"
)

// TestSelfUpdater_Direct_HappyPath directly tests the SelfUpdater without webhook integration
func TestSelfUpdater_Direct_HappyPath(t *testing.T) {
	// Create realistic test environment
	testEnv, err := NewRealisticTestEnv("SelfUpdater_Direct_HappyPath")
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

	// Create initial binary
	initialBinaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-current")
	initialBinary, err := helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
	if err != nil {
		t.Fatalf("Failed to create initial binary: %v", err)
	}

	if err := copyFile(initialBinary, initialBinaryPath); err != nil {
		t.Fatalf("Failed to copy initial binary: %v", err)
	}

	// Create self-update repository with v2.0.0
	updatedAppFiles, err := CreateSelfUpdateApp("2.0.0")
	if err != nil {
		t.Fatalf("Failed to create updated app template: %v", err)
	}

	updatedRepoURL, err := helper.CreateTestRepository("selfupdate-v2", updatedAppFiles)
	if err != nil {
		t.Fatalf("Failed to create updated repository: %v", err)
	}
	defer helper.CleanupTestRepository(updatedRepoURL)

	// Test initial binary
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := CreateTestCommand(ctx, initialBinaryPath, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Initial binary failed: %v", err)
	}

	expectedInitial := "binaryDeploy-test version 1.0.0\n"
	if string(output) != expectedInitial {
		t.Fatalf("Expected initial version %q, got %q", expectedInitial, string(output))
	}

	testEnv.Logger.Info("Testing SelfUpdater directly",
		"binary_path", initialBinaryPath,
		"repo_url", updatedRepoURL)

	// Create SelfUpdater instance
	selfUpdateDir := filepath.Join(testEnv.TempDir, "selfupdate")
	selfUpdater := updater.NewSelfUpdater(initialBinaryPath, selfUpdateDir)

	// Perform self-update
	err = selfUpdater.Update(updatedRepoURL, "main")
	if err != nil {
		t.Fatalf("Self-update failed: %v", err)
	}

	// Test updated binary
	cmd = CreateTestCommand(context.Background(), initialBinaryPath, "--version")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Updated binary failed to run: %v", err)
	}

	expectedUpdated := "binaryDeploy-test version 2.0.0\n"
	if string(output) != expectedUpdated {
		t.Fatalf("Expected updated version %q, got %q", expectedUpdated, string(output))
	}

	// Verify backup exists (should be at configured location)
	backupPath := "/tmp/binaryDeploy-test.backup"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatalf("Backup binary was not created at %s", backupPath)
	}

	// Make backup executable
	if err := os.Chmod(backupPath, 0755); err != nil {
		t.Fatalf("Failed to make backup executable: %v", err)
	}

	// Test backup has original version
	cmd = CreateTestCommand(context.Background(), backupPath, "--version")
	output, err = cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Backup binary failed to run: %v", err)
	}

	if string(output) != expectedInitial {
		t.Fatalf("Backup binary has wrong version, expected %q, got %q", expectedInitial, string(output))
	}

	testEnv.Logger.Info("SelfUpdater direct test completed successfully",
		"binary_path", initialBinaryPath,
		"backup_path", backupPath,
		"final_version", string(output))
}

// TestSelfUpdater_Direct_Rollback tests rollback functionality
func TestSelfUpdater_Direct_Rollback(t *testing.T) {
	// Create realistic test environment
	testEnv, err := NewRealisticTestEnv("SelfUpdater_Direct_Rollback")
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

	// Create initial binary
	initialBinaryPath := filepath.Join(testEnv.TempDir, "binaryDeploy-current")
	initialBinary, err := helper.CreateSelfUpdateBinary(testEnv.TempDir, testEnv.TempDir, "1.0.0")
	if err != nil {
		t.Fatalf("Failed to create initial binary: %v", err)
	}

	if err := copyFile(initialBinary, initialBinaryPath); err != nil {
		t.Fatalf("Failed to copy initial binary: %v", err)
	}

	// Store original version
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := CreateTestCommand(ctx, initialBinaryPath, "--version")
	originalOutput, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Initial binary failed: %v", err)
	}

	expectedOriginal := "binaryDeploy-test version 1.0.0\n"
	if string(originalOutput) != expectedOriginal {
		t.Fatalf("Expected original version %q, got %q", expectedOriginal, string(originalOutput))
	}

	// Create failing repository
	failingAppFiles, err := CreateFailingApp("build")
	if err != nil {
		t.Fatalf("Failed to create failing app template: %v", err)
	}

	failingRepoURL, err := helper.CreateTestRepository("selfupdate-failing", failingAppFiles)
	if err != nil {
		t.Fatalf("Failed to create failing repository: %v", err)
	}
	defer helper.CleanupTestRepository(failingRepoURL)

	// Create SelfUpdater instance
	selfUpdateDir := filepath.Join(testEnv.TempDir, "selfupdate")
	selfUpdater := updater.NewSelfUpdater(initialBinaryPath, selfUpdateDir)

	// Attempt self-update with failing repository (should fail)
	err = selfUpdater.Update(failingRepoURL, "main")
	if err == nil {
		t.Fatalf("Expected self-update to fail, but it succeeded")
	}

	testEnv.Logger.Info("Self-update failed as expected", "error", err)

	// Verify original binary is still intact (no rollback needed since update failed)
	cmd = CreateTestCommand(context.Background(), initialBinaryPath, "--version")
	currentOutput, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Binary failed after failed update: %v", err)
	}

	if string(currentOutput) != string(originalOutput) {
		t.Fatalf("Binary was corrupted after failed update! Expected %q, got %q", string(originalOutput), string(currentOutput))
	}

	// Verify backup exists (it should have been created before attempting update)
	backupPath := "/tmp/binaryDeploy-test.backup"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatalf("Backup was not created before failed update")
	}

	// Make backup executable for testing
	if err := os.Chmod(backupPath, 0755); err != nil {
		t.Fatalf("Failed to make backup executable: %v", err)
	}

	// Test rollback functionality manually
	if !selfUpdater.HasBackup() {
		t.Fatalf("Updater reports no backup exists")
	}

	// Corrupt current binary
	if err := os.WriteFile(initialBinaryPath, []byte("corrupted"), 0644); err != nil {
		t.Fatalf("Failed to corrupt binary: %v", err)
	}

	// Perform rollback
	err = selfUpdater.Rollback()
	if err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Verify rollback worked
	cmd = CreateTestCommand(context.Background(), initialBinaryPath, "--version")
	rollbackOutput, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Rolled back binary failed to run: %v", err)
	}

	if string(rollbackOutput) != string(originalOutput) {
		t.Fatalf("Rollback failed! Expected %q, got %q", string(originalOutput), string(rollbackOutput))
	}

	testEnv.Logger.Info("SelfUpdater rollback test completed successfully",
		"binary_path", initialBinaryPath,
		"backup_path", backupPath)
}
