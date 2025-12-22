package test

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"log/slog"
)

// LogPersistence manages persistent logging for realistic tests
type LogPersistence struct {
	LogDir     string
	LogFiles   map[string]*os.File
	Loggers    map[string]*slog.Logger
	mutex      sync.RWMutex
	FlushFuncs []func() error
}

// NewLogPersistence creates a new log persistence manager
func NewLogPersistence(logDir string) *LogPersistence {
	lp := &LogPersistence{
		LogDir:   logDir,
		LogFiles: make(map[string]*os.File),
		Loggers:  make(map[string]*slog.Logger),
	}

	// Initialize default log files
	logTypes := []string{
		"main",
		"processes",
		"webhooks",
		"deployments",
		"chaos",
		"errors",
		"cleanup",
		"network",
	}

	for _, logType := range logTypes {
		if err := lp.InitializeLogger(logType); err != nil {
			// Continue even if some log files fail to initialize
			fmt.Printf("Warning: Failed to initialize %s logger: %v\n", logType, err)
		}
	}

	return lp
}

// InitializeLogger creates a logger for a specific log type
func (lp *LogPersistence) InitializeLogger(logType string) error {
	lp.mutex.Lock()

	// Check if already initialized
	if _, exists := lp.Loggers[logType]; exists {
		lp.mutex.Unlock()
		return nil
	}

	// Create log file path
	logFile := filepath.Join(lp.LogDir, logType+".log")

	// Open log file
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		lp.mutex.Unlock()
		return fmt.Errorf("failed to open log file %s: %w", logFile, err)
	}

	// Create logger
	logger := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	lp.LogFiles[logType] = file
	lp.Loggers[logType] = logger

	// Add flush function manually to avoid deadlock
	lp.FlushFuncs = append(lp.FlushFuncs, func() error {
		return file.Sync()
	})

	lp.mutex.Unlock()
	return nil
}

// GetLogger returns a logger for the specified type
func (lp *LogPersistence) GetLogger(logType string) *slog.Logger {
	lp.mutex.RLock()
	defer lp.mutex.RUnlock()

	logger, exists := lp.Loggers[logType]
	if !exists {
		// Fallback to creating the logger on-demand
		lp.mutex.RUnlock()
		if err := lp.InitializeLogger(logType); err != nil {
			// Return a no-op logger if initialization fails
			return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
		}
		lp.mutex.RLock()
		logger = lp.Loggers[logType]
	}

	return logger
}

// Log logs a message to the specified log type
func (lp *LogPersistence) Log(logType string, level slog.Level, message string, args ...any) {
	logger := lp.GetLogger(logType)
	logger.Log(nil, level, message, args...)
}

// LogInfo logs an info message
func (lp *LogPersistence) LogInfo(logType, message string, args ...any) {
	lp.Log(logType, slog.LevelInfo, message, args...)
}

// LogError logs an error message
func (lp *LogPersistence) LogError(logType, message string, args ...any) {
	lp.Log(logType, slog.LevelError, message, args...)
}

// LogDebug logs a debug message
func (lp *LogPersistence) LogDebug(logType, message string, args ...any) {
	lp.Log(logType, slog.LevelDebug, message, args...)
}

// LogWarn logs a warning message
func (lp *LogPersistence) LogWarn(logType, message string, args ...any) {
	lp.Log(logType, slog.LevelWarn, message, args...)
}

// LogWithTimestamp logs a message with an explicit timestamp
func (lp *LogPersistence) LogWithTimestamp(logType string, timestamp time.Time, level slog.Level, message string, args ...any) {
	logger := lp.GetLogger(logType)

	// Add timestamp to args
	allArgs := append([]any{"log_timestamp", timestamp}, args...)
	logger.Log(nil, level, message, allArgs...)
}

// LogEvent logs a structured event
func (lp *LogPersistence) LogEvent(logType, eventType string, data map[string]any) {
	logger := lp.GetLogger(logType)

	args := make([]any, 0, len(data)*2+2)
	args = append(args, "event_type", eventType)

	for key, value := range data {
		args = append(args, key, value)
	}

	logger.Info("Event: "+eventType, args...)
}

// LogErrorEvent logs an error event with stack trace
func (lp *LogPersistence) LogErrorEvent(logType string, err error, context map[string]any) {
	logger := lp.GetLogger(logType)

	args := make([]any, 0, len(context)*2+4)
	args = append(args, "error", err.Error(), "error_type", fmt.Sprintf("%T", err))

	for key, value := range context {
		args = append(args, key, value)
	}

	logger.Error("Error occurred", args...)
}

// LogProcess logs a process lifecycle event
func (lp *LogPersistence) LogProcess(pid int, event string, description string, additionalData map[string]any) {
	data := map[string]any{
		"pid":         pid,
		"event":       event,
		"description": description,
		"timestamp":   time.Now(),
	}

	// Merge additional data
	for k, v := range additionalData {
		data[k] = v
	}

	lp.LogEvent("processes", "process_lifecycle", data)
}

// LogWebhook logs a webhook event
func (lp *LogPersistence) LogWebhook(eventType, repo, commit string, statusCode int, responseTime time.Duration) {
	data := map[string]any{
		"event_type":    eventType,
		"repository":    repo,
		"commit":        commit,
		"status_code":   statusCode,
		"response_time": responseTime.String(),
		"timestamp":     time.Now(),
	}

	lp.LogEvent("webhooks", "webhook_received", data)
}

// LogDeployment logs a deployment event
func (lp *LogPersistence) LogDeployment(deploymentID, repo, commit, status string, duration time.Duration, error error) {
	data := map[string]any{
		"deployment_id": deploymentID,
		"repository":    repo,
		"commit":        commit,
		"status":        status,
		"duration":      duration.String(),
		"timestamp":     time.Now(),
	}

	if error != nil {
		data["error"] = error.Error()
		data["error_type"] = fmt.Sprintf("%T", error)
	}

	lp.LogEvent("deployments", "deployment_lifecycle", data)
}

// LogChaos logs a chaos injection event
func (lp *LogPersistence) LogChaos(chaosType, target string, severity string, result string, details map[string]any) {
	data := map[string]any{
		"chaos_type": chaosType,
		"target":     target,
		"severity":   severity,
		"result":     result,
		"timestamp":  time.Now(),
	}

	// Merge additional details
	for k, v := range details {
		data[k] = v
	}

	lp.LogEvent("chaos", "chaos_injection", data)
}

// LogCleanup logs a cleanup operation
func (lp *LogPersistence) LogCleanup(operation string, target string, result string, details map[string]any) {
	data := map[string]any{
		"operation": operation,
		"target":    target,
		"result":    result,
		"timestamp": time.Now(),
	}

	// Merge additional details
	for k, v := range details {
		data[k] = v
	}

	lp.LogEvent("cleanup", "cleanup_operation", data)
}

// AddFlushFunc adds a function to be called during FlushAll
func (lp *LogPersistence) AddFlushFunc(flushFunc func() error) {
	lp.mutex.Lock()
	defer lp.mutex.Unlock()
	lp.FlushFuncs = append(lp.FlushFuncs, flushFunc)
}

// Flush flushes all log files to disk
func (lp *LogPersistence) Flush() error {
	lp.mutex.RLock()
	logFiles := make(map[string]*os.File)
	for k, v := range lp.LogFiles {
		logFiles[k] = v
	}
	lp.mutex.RUnlock()

	var errors []error
	for logType, file := range logFiles {
		if err := file.Sync(); err != nil {
			errors = append(errors, fmt.Errorf("failed to flush %s log: %w", logType, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("flush failed for %d log files: %v", len(errors), errors)
	}

	return nil
}

// FlushAll flushes all logs and runs all flush functions
func (lp *LogPersistence) FlushAll() error {
	var errors []error

	// Flush all log files
	if err := lp.Flush(); err != nil {
		errors = append(errors, err)
	}

	// Run all flush functions
	lp.mutex.RLock()
	flushFuncs := lp.FlushFuncs
	lp.mutex.RUnlock()

	for i, flushFunc := range flushFuncs {
		if err := flushFunc(); err != nil {
			errors = append(errors, fmt.Errorf("flush function %d failed: %w", i, err))
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("flush all completed with %d errors: %v", len(errors), errors)
	}

	return nil
}

// Close closes all log files and cleans up resources
func (lp *LogPersistence) Close() error {
	lp.mutex.Lock()
	defer lp.mutex.Unlock()

	var errors []error

	// Close all log files
	for logType, file := range lp.LogFiles {
		if err := file.Close(); err != nil {
			errors = append(errors, fmt.Errorf("failed to close %s log file: %w", logType, err))
		}
	}

	// Clear all maps
	lp.LogFiles = make(map[string]*os.File)
	lp.Loggers = make(map[string]*slog.Logger)
	lp.FlushFuncs = nil

	if len(errors) > 0 {
		return fmt.Errorf("close completed with %d errors: %v", len(errors), errors)
	}

	return nil
}

// GetLogFilePath returns the path to a specific log file
func (lp *LogPersistence) GetLogFilePath(logType string) string {
	return filepath.Join(lp.LogDir, logType+".log")
}

// GetAllLogFilePaths returns paths to all log files
func (lp *LogPersistence) GetAllLogFilePaths() []string {
	lp.mutex.RLock()
	defer lp.mutex.RUnlock()

	paths := make([]string, 0, len(lp.LogFiles))
	for logType := range lp.LogFiles {
		paths = append(paths, filepath.Join(lp.LogDir, logType+".log"))
	}

	return paths
}

// GetLogStats returns statistics about log files
func (lp *LogPersistence) GetLogStats() map[string]interface{} {
	lp.mutex.RLock()
	defer lp.mutex.RUnlock()

	stats := make(map[string]interface{})
	stats["log_dir"] = lp.LogDir
	stats["log_files_count"] = len(lp.LogFiles)
	stats["flush_funcs_count"] = len(lp.FlushFuncs)

	fileStats := make(map[string]interface{})
	for logType, file := range lp.LogFiles {
		if info, err := file.Stat(); err == nil {
			fileStats[logType] = map[string]interface{}{
				"size":     info.Size(),
				"mod_time": info.ModTime(),
				"name":     info.Name(),
			}
		}
	}
	stats["files"] = fileStats

	return stats
}
