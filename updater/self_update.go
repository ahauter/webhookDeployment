package updater

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SelfUpdater handles updating the webhook server binary
type SelfUpdater struct {
	CurrentBinaryPath string
	SelfUpdateDir     string
	TempDir           string
	BackupPath        string
}

// NewSelfUpdater creates a new SelfUpdater instance
func NewSelfUpdater(currentBinaryPath, selfUpdateDir string) *SelfUpdater {
	return &SelfUpdater{
		CurrentBinaryPath: currentBinaryPath,
		SelfUpdateDir:     selfUpdateDir,
		TempDir:           filepath.Join(selfUpdateDir, "temp"),
		BackupPath:        currentBinaryPath + ".backup",
	}
}

// Update performs the self-update process with automatic rollback on failure
func (su *SelfUpdater) Update(repoURL, branch string) error {
	slog.Info("Starting self-update", "repo_url", repoURL, "branch", branch)

	// Create temporary directory for update
	if err := os.MkdirAll(su.TempDir, 0755); err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}

	repoDir := filepath.Join(su.TempDir, "repo")

	// Clone or update the repository
	if err := su.cloneOrUpdateRepo(repoURL, repoDir); err != nil {
		su.cleanup()
		return fmt.Errorf("cloning/updating repo: %w", err)
	}

	// Read deploy config from the cloned repository
	configPath := filepath.Join(repoDir, "deploy.config")
	deployConfig, err := su.readDeployConfig(configPath)
	if err != nil {
		su.cleanup()
		return fmt.Errorf("reading deploy config: %w", err)
	}

	// Backup current binary
	if err := su.backupCurrentBinary(deployConfig); err != nil {
		su.cleanup()
		return fmt.Errorf("backing up current binary: %w", err)
	}

	// Build new binary
	newBinaryPath, err := su.buildNewBinary(repoDir, deployConfig)
	if err != nil {
		su.cleanup()
		return fmt.Errorf("building new binary: %w", err)
	}

	// Verify new binary
	if err := su.verifyNewBinary(newBinaryPath); err != nil {
		su.cleanup()
		return fmt.Errorf("verifying new binary: %w", err)
	}

	// Replace current binary atomically
	if err := su.replaceBinaryAtomically(newBinaryPath); err != nil {
		// Try to rollback on failure
		if rollbackErr := su.Rollback(); rollbackErr != nil {
			slog.Error("Failed to rollback after binary replacement failure", "error", rollbackErr)
		} else {
			slog.Info("Successfully rolled back after binary replacement failure")
		}
		su.cleanup()
		return fmt.Errorf("replacing binary (rollback attempted): %w", err)
	}

	// Test the new binary by running it with --help
	if err := su.testNewBinary(); err != nil {
		slog.Error("New binary test failed", "error", err)
		if rollbackErr := su.Rollback(); rollbackErr != nil {
			slog.Error("Failed to rollback after binary test failure", "error", rollbackErr)
		} else {
			slog.Info("Successfully rolled back after binary test failure")
		}
		su.cleanup()
		return fmt.Errorf("new binary test failed (rollback attempted): %w", err)
	}

	// Clean up temporary files on success
	su.cleanup()

	slog.Info("Self-update completed successfully")
	return nil
}

// testNewBinary runs the new binary to ensure it works
func (su *SelfUpdater) testNewBinary() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, su.CurrentBinaryPath, "--version")
	if err := cmd.Run(); err != nil {
		// Try --help if --version fails
		cmd = exec.CommandContext(ctx, su.CurrentBinaryPath, "--help")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("new binary failed to run with --version or --help: %w", err)
		}
	}

	slog.Info("New binary test passed")
	return nil
}

// cloneOrUpdateRepo clones the repository or updates an existing one
func (su *SelfUpdater) cloneOrUpdateRepo(repoURL, repoDir string) error {
	if _, err := os.Stat(repoDir); os.IsNotExist(err) {
		slog.Info("Cloning repository", "path", repoDir)
		if err := su.runCommand("git", "clone", repoURL, repoDir); err != nil {
			return err
		}
	} else {
		slog.Info("Updating repository", "path", repoDir)
		if err := su.runCommandInDir(repoDir, "git", "fetch", "origin"); err != nil {
			return err
		}
		if err := su.runCommandInDir(repoDir, "git", "reset", "--hard", "origin/HEAD"); err != nil {
			return err
		}
	}
	return nil
}

// readDeployConfig reads the deploy.config file
func (su *SelfUpdater) readDeployConfig(configPath string) (interface{}, error) {
	// For now, we'll read it as a simple map until we integrate the config package
	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	config := make(map[string]string)
	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.Contains(line, "=") {
			parts := strings.SplitN(line, "=", 2)
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			config[key] = value
		}
	}

	return config, nil
}

// backupCurrentBinary creates a backup of the current binary
func (su *SelfUpdater) backupCurrentBinary(deployConfig interface{}) error {
	configMap, ok := deployConfig.(map[string]string)
	if !ok {
		return fmt.Errorf("invalid config type")
	}

	backupPath := su.CurrentBinaryPath + ".backup"
	if backupBinary, exists := configMap["backup_binary"]; exists && backupBinary != "" {
		backupPath = backupBinary
	}

	slog.Info("Backing up current binary", "backup_path", backupPath)

	// Remove existing backup
	if _, err := os.Stat(backupPath); err == nil {
		if err := os.Remove(backupPath); err != nil {
			return fmt.Errorf("removing existing backup: %w", err)
		}
	}

	// Create backup
	if err := su.copyFile(su.CurrentBinaryPath, backupPath); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}

	return nil
}

// buildNewBinary builds the new binary from source
func (su *SelfUpdater) buildNewBinary(repoDir string, deployConfig interface{}) (string, error) {
	configMap, ok := deployConfig.(map[string]string)
	if !ok {
		return "", fmt.Errorf("invalid config type")
	}

	buildCommand, exists := configMap["build_command"]
	if !exists {
		return "", fmt.Errorf("build_command not found in deploy.config")
	}

	slog.Info("Building new binary", "command", buildCommand)

	// Parse command and arguments
	parts := strings.Fields(buildCommand)
	if len(parts) == 0 {
		return "", fmt.Errorf("empty build command")
	}

	// Execute build command in repository directory
	if err := su.runCommandInDir(repoDir, parts[0], parts[1:]...); err != nil {
		return "", err
	}

	// Find the built binary (assume it's in the repo directory)
	binaryName := "binaryDeploy" // Default name
	if len(parts) > 2 && strings.Contains(parts[len(parts)-2], "-o") {
		binaryName = filepath.Base(parts[len(parts)-1])
	}

	newBinaryPath := filepath.Join(repoDir, binaryName)
	if _, err := os.Stat(newBinaryPath); err != nil {
		return "", fmt.Errorf("built binary not found at %s", newBinaryPath)
	}

	return newBinaryPath, nil
}

// verifyNewBinary checks if the new binary is valid
func (su *SelfUpdater) verifyNewBinary(binaryPath string) error {
	// Check if file exists and is executable
	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("binary not found: %w", err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("binary is not a regular file")
	}

	// Make it executable
	if err := os.Chmod(binaryPath, 0755); err != nil {
		return fmt.Errorf("making binary executable: %w", err)
	}

	// Try to run binary with --version or --help to verify it works
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "--version")
	if err := cmd.Run(); err != nil {
		// Try with --help if --version fails
		cmd = exec.CommandContext(ctx, binaryPath, "--help")
		if err := cmd.Run(); err != nil {
			slog.Warn("Could not verify binary with --version or --help", "error", err)
		}
	}

	return nil
}

// replaceBinaryAtomically replaces the current binary with the new one
func (su *SelfUpdater) replaceBinaryAtomically(newBinaryPath string) error {
	slog.Info("Replacing binary atomically")

	// Create temporary file path for atomic replacement
	tempPath := su.CurrentBinaryPath + ".new"

	// Copy new binary to temporary location
	if err := su.copyFile(newBinaryPath, tempPath); err != nil {
		return fmt.Errorf("copying new binary to temp location: %w", err)
	}

	// Ensure temp binary has correct permissions
	if err := os.Chmod(tempPath, 0755); err != nil {
		return fmt.Errorf("setting permissions on temp binary: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, su.CurrentBinaryPath); err != nil {
		return fmt.Errorf("atomic rename failed: %w", err)
	}

	return nil
}

// Rollback restores the backup binary if the update failed
func (su *SelfUpdater) Rollback() error {
	if _, err := os.Stat(su.BackupPath); os.IsNotExist(err) {
		return fmt.Errorf("no backup binary found at %s", su.BackupPath)
	}

	slog.Info("Rolling back to backup binary", "backup_path", su.BackupPath)

	// Create temporary file for atomic rollback
	tempPath := su.CurrentBinaryPath + ".rollback"
	if err := su.copyFile(su.BackupPath, tempPath); err != nil {
		return fmt.Errorf("copying backup to temp: %w", err)
	}

	// Ensure correct permissions
	if err := os.Chmod(tempPath, 0755); err != nil {
		return fmt.Errorf("setting permissions on rollback binary: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, su.CurrentBinaryPath); err != nil {
		return fmt.Errorf("atomic rollback failed: %w", err)
	}

	slog.Info("Rollback completed successfully")
	return nil
}

// HasBackup checks if a backup binary exists
func (su *SelfUpdater) HasBackup() bool {
	_, err := os.Stat(su.BackupPath)
	return err == nil
}

// cleanup removes temporary files
func (su *SelfUpdater) cleanup() {
	if err := os.RemoveAll(su.TempDir); err != nil {
		slog.Warn("Failed to clean up temp directory", "error", err)
	}
}

// copyFile copies a file from src to dst
func (su *SelfUpdater) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// runCommand executes a command
func (su *SelfUpdater) runCommand(command string, args ...string) error {
	return su.runCommandInDir("", command, args...)
}

// runCommandInDir executes a command in a specific directory
func (su *SelfUpdater) runCommandInDir(dir, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	if dir != "" {
		cmd.Dir = dir
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}
