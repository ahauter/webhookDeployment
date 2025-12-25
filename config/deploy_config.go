package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DeployConfig represents the parsed deploy.config file
type DeployConfig struct {
	// BinaryDeploy Configuration (optional - all have defaults)
	Port              string
	LogFile           string
	LogBufferSize     int
	DeployDir         string
	SelfUpdateDir     string
	SelfUpdateRepoURL string

	// Application Configuration (required)
	TargetRepoURL   string
	AllowedBranches string // Comma-separated list
	Secret          string

	// Application Deployment Settings
	BuildCommand    string
	RunCommand      string
	WorkingDir      string
	Environment     string
	ApplicationPort int // Application port, separate from binary port
	RestartDelay    int
	MaxRestarts     int
	BackupBinary    string
	RestartCommand  string
}

// DefaultDeployConfig returns a config with sensible defaults
func DefaultDeployConfig() *DeployConfig {
	return &DeployConfig{
		// BinaryDeploy Configuration defaults
		Port:              "8080",
		LogFile:           "./binaryDeploy.log",
		LogBufferSize:     1000,
		DeployDir:         "./deployments",
		SelfUpdateDir:     "./self-update",
		SelfUpdateRepoURL: "https://github.com/ahauter/binaryDeploy-updater.git",

		// Application Configuration defaults
		AllowedBranches: "main",

		// Application Deployment Settings defaults
		WorkingDir:      "./",
		ApplicationPort: 8080,
		RestartDelay:    5,
		MaxRestarts:     3,
	}
}

// LoadDeployConfig parses a key=value deploy.config file
func LoadDeployConfig(path string) (*DeployConfig, error) {
	values, err := readConfigFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading deploy config: %w", err)
	}

	config := DefaultDeployConfig()

	// Parse required fields
	if buildCmd, ok := values["build_command"]; ok {
		config.BuildCommand = buildCmd
	} else {
		return nil, fmt.Errorf("missing required field: build_command")
	}

	if runCmd, ok := values["run_command"]; ok {
		config.RunCommand = runCmd
	} else {
		return nil, fmt.Errorf("missing required field: run_command")
	}

	// Parse optional fields
	if workDir, ok := values["working_dir"]; ok {
		config.WorkingDir = workDir
	}

	if env, ok := values["environment"]; ok {
		config.Environment = env
	}

	if port, ok := values["port"]; ok {
		if p, err := strconv.Atoi(port); err == nil {
			config.ApplicationPort = p
		}
	}

	// Handle binary port separately if specified
	if binaryPort, ok := values["binary_port"]; ok {
		config.Port = binaryPort
	}

	if restartDelay, ok := values["restart_delay"]; ok {
		if r, err := strconv.Atoi(restartDelay); err == nil {
			config.RestartDelay = r
		}
	}

	if maxRestarts, ok := values["max_restarts"]; ok {
		if r, err := strconv.Atoi(maxRestarts); err == nil {
			config.MaxRestarts = r
		}
	}

	// Self-update specific fields
	if backupBinary, ok := values["backup_binary"]; ok {
		config.BackupBinary = backupBinary
	}

	if restartCmd, ok := values["restart_command"]; ok {
		config.RestartCommand = restartCmd
	}

	// Parse binary configuration fields
	if logFile, ok := values["log_file"]; ok {
		config.LogFile = logFile
	}

	if logBufferSize, ok := values["log_buffer_size"]; ok {
		if size, err := strconv.Atoi(logBufferSize); err == nil && size > 0 {
			config.LogBufferSize = size
		}
	}

	if deployDir, ok := values["deploy_dir"]; ok {
		config.DeployDir = deployDir
	}

	if selfUpdateDir, ok := values["self_update_dir"]; ok {
		config.SelfUpdateDir = selfUpdateDir
	}

	if selfUpdateRepoURL, ok := values["self_update_repo_url"]; ok {
		config.SelfUpdateRepoURL = selfUpdateRepoURL
	}

	// Parse application configuration fields (required)
	if targetRepoURL, ok := values["target_repo_url"]; ok {
		config.TargetRepoURL = targetRepoURL
	} else {
		return nil, fmt.Errorf("missing required field: target_repo_url")
	}

	if allowedBranches, ok := values["allowed_branches"]; ok {
		config.AllowedBranches = allowedBranches
	} else {
		return nil, fmt.Errorf("missing required field: allowed_branches")
	}

	if secret, ok := values["secret"]; ok {
		config.Secret = secret
	} else {
		return nil, fmt.Errorf("missing required field: secret")
	}

	return config, nil
}

// ValidateConfig validates the configuration and returns warnings for used defaults
func ValidateConfig(config *DeployConfig) error {
	// Check all required fields
	if config.TargetRepoURL == "" {
		return fmt.Errorf("missing required field: target_repo_url")
	}
	if config.AllowedBranches == "" {
		return fmt.Errorf("missing required field: allowed_branches")
	}
	if config.Secret == "" {
		return fmt.Errorf("missing required field: secret")
	}
	if config.BuildCommand == "" {
		return fmt.Errorf("missing required field: build_command")
	}
	if config.RunCommand == "" {
		return fmt.Errorf("missing required field: run_command")
	}

	return nil
}

// GetDefaultWarnings returns warnings for any default values being used
func GetDefaultWarnings(config *DeployConfig) []string {
	var warnings []string

	defaults := DefaultDeployConfig()

	if config.Port == defaults.Port {
		warnings = append(warnings, "Using default binary port 8080 (add 'port=8080' to deploy.config to customize)")
	}
	if config.LogFile == defaults.LogFile {
		warnings = append(warnings, "Using default log file ./binaryDeploy.log (add 'log_file=...' to deploy.config to customize)")
	}
	if config.DeployDir == defaults.DeployDir {
		warnings = append(warnings, "Using default deploy directory ./deployments (add 'deploy_dir=...' to deploy.config to customize)")
	}
	if config.SelfUpdateDir == defaults.SelfUpdateDir {
		warnings = append(warnings, "Using default self-update directory ./self-update (add 'self_update_dir=...' to deploy.config to customize)")
	}
	if config.SelfUpdateRepoURL == defaults.SelfUpdateRepoURL {
		warnings = append(warnings, "Using default self-update repository (add 'self_update_repo_url=...' to deploy.config to customize)")
	}
	if config.AllowedBranches == defaults.AllowedBranches {
		warnings = append(warnings, "Using default allowed branches 'main' (add 'allowed_branches=...' to deploy.config to customize)")
	}

	return warnings
}

// readConfigFile reads and parses a key=value config file
func readConfigFile(filename string) (map[string]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	values := make(map[string]string)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Remove comments and trim whitespace
		if commentIndex := strings.Index(line, "#"); commentIndex >= 0 {
			line = line[:commentIndex]
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue // Skip empty lines
		}

		// Parse key=value
		if !strings.Contains(line, "=") {
			return nil, fmt.Errorf("line %d: missing '=' separator in '%s'", lineNum, line)
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("line %d: invalid format in '%s'", lineNum, line)
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		if key == "" {
			return nil, fmt.Errorf("line %d: empty key in '%s'", lineNum, line)
		}

		// Remove quotes if present
		if (strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`)) ||
			(strings.HasPrefix(value, `'`) && strings.HasSuffix(value, `'`)) {
			value = value[1 : len(value)-1]
		}

		values[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning config file: %w", err)
	}

	return values, nil
}
