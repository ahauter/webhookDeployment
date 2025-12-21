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
	BuildCommand   string
	RunCommand     string
	WorkingDir     string
	Environment    string
	Port           int
	RestartDelay   int
	MaxRestarts    int
	BackupBinary   string
	RestartCommand string
}

// DefaultDeployConfig returns a config with sensible defaults
func DefaultDeployConfig() *DeployConfig {
	return &DeployConfig{
		WorkingDir:   "./",
		Port:         8080,
		RestartDelay: 5,
		MaxRestarts:  3,
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
			config.Port = p
		}
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

	return config, nil
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
