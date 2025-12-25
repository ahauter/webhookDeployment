package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"log/slog"
)

// LogStreamer handles real-time log streaming with circular buffer
type LogStreamer struct {
	handler    slog.Handler
	logChan    chan []byte
	clients    map[chan []byte]bool
	clientsMux sync.RWMutex
	buffer     [][]byte
	bufferMux  sync.RWMutex
	maxBuffer  int
	startTime  time.Time
}

// StreamingLogEntry represents a formatted log entry for frontend
type StreamingLogEntry struct {
	Timestamp time.Time              `json:"timestamp"`
	Level     string                 `json:"level"`
	Message   string                 `json:"message"`
	Fields    map[string]interface{} `json:"fields,omitempty"`
	Color     string                 `json:"color"`
}

// Global log streamer instance
var globalLogStreamer *LogStreamer

// NewLogStreamer creates a new log streaming handler
func NewLogStreamer(baseHandler slog.Handler, maxBuffer int) *LogStreamer {
	if maxBuffer <= 0 {
		maxBuffer = 1000 // default
	}

	ls := &LogStreamer{
		handler:   baseHandler,
		logChan:   make(chan []byte, 1000), // buffered channel
		clients:   make(map[chan []byte]bool),
		buffer:    make([][]byte, 0, maxBuffer),
		maxBuffer: maxBuffer,
		startTime: time.Now(),
	}

	// Start log distribution goroutine
	go ls.distributeLogs()

	return ls
}

// Handle implements slog.Handler interface
func (ls *LogStreamer) Handle(ctx context.Context, r slog.Record) error {
	// First, write to the original handler (file)
	err := ls.handler.Handle(ctx, r)

	// Create streaming log entry
	entry := StreamingLogEntry{
		Timestamp: r.Time,
		Level:     r.Level.String(),
		Message:   r.Message,
		Fields:    make(map[string]interface{}),
		Color:     ls.getLevelColor(r.Level),
	}

	// Extract attributes
	r.Attrs(func(a slog.Attr) bool {
		entry.Fields[a.Key] = a.Value.Any()
		return true
	})

	// Marshal to JSON
	if data, marshalErr := json.Marshal(entry); marshalErr == nil {
		select {
		case ls.logChan <- data:
			// Log sent to channel
		default:
			// Channel full, skip this log to avoid blocking
		}
	}

	return err
}

// Enabled implements slog.Handler interface
func (ls *LogStreamer) Enabled(ctx context.Context, level slog.Level) bool {
	return ls.handler.Enabled(ctx, level)
}

// WithAttrs implements slog.Handler interface
func (ls *LogStreamer) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogStreamer{
		handler:   ls.handler.WithAttrs(attrs),
		logChan:   ls.logChan,
		clients:   ls.clients,
		buffer:    ls.buffer,
		bufferMux: ls.bufferMux,
		maxBuffer: ls.maxBuffer,
		startTime: ls.startTime,
	}
}

// WithGroup implements slog.Handler interface
func (ls *LogStreamer) WithGroup(name string) slog.Handler {
	return &LogStreamer{
		handler:   ls.handler.WithGroup(name),
		logChan:   ls.logChan,
		clients:   ls.clients,
		buffer:    ls.buffer,
		bufferMux: ls.bufferMux,
		maxBuffer: ls.maxBuffer,
		startTime: ls.startTime,
	}
}

// AddClient adds a new SSE client
func (ls *LogStreamer) AddClient(clientChan chan []byte) {
	ls.clientsMux.Lock()
	defer ls.clientsMux.Unlock()
	ls.clients[clientChan] = true
}

// RemoveClient removes an SSE client
func (ls *LogStreamer) RemoveClient(clientChan chan []byte) {
	ls.clientsMux.Lock()
	defer ls.clientsMux.Unlock()
	delete(ls.clients, clientChan)
	close(clientChan)
}

// GetBufferedLogs returns the current buffer contents
func (ls *LogStreamer) GetBufferedLogs() [][]byte {
	ls.bufferMux.RLock()
	defer ls.bufferMux.RUnlock()

	// Return a copy to avoid race conditions
	result := make([][]byte, len(ls.buffer))
	copy(result, ls.buffer)
	return result
}

// GetStats returns streaming statistics
func (ls *LogStreamer) GetStats() map[string]interface{} {
	ls.bufferMux.RLock()
	ls.clientsMux.RLock()
	defer ls.bufferMux.RUnlock()
	defer ls.clientsMux.RUnlock()

	return map[string]interface{}{
		"clients_count":   len(ls.clients),
		"buffer_size":     len(ls.buffer),
		"max_buffer":      ls.maxBuffer,
		"uptime":          time.Since(ls.startTime).String(),
		"total_log_count": ls.getTotalLogCount(),
	}
}

// distributeLogs sends incoming logs to all connected clients
func (ls *LogStreamer) distributeLogs() {
	for logData := range ls.logChan {
		// Add to circular buffer
		ls.addToBuffer(logData)

		// Send to all clients
		ls.clientsMux.RLock()
		for clientChan := range ls.clients {
			select {
			case clientChan <- logData:
				// Sent successfully
			default:
				// Client channel full, skip
			}
		}
		ls.clientsMux.RUnlock()
	}
}

// addToBuffer adds log entry to circular buffer
func (ls *LogStreamer) addToBuffer(logData []byte) {
	ls.bufferMux.Lock()
	defer ls.bufferMux.Unlock()

	ls.buffer = append(ls.buffer, logData)

	// Maintain circular buffer size
	if len(ls.buffer) > ls.maxBuffer {
		// Remove oldest entry (slide window)
		ls.buffer = ls.buffer[1:]
	}
}

// getLevelColor returns CSS color class for log level
func (ls *LogStreamer) getLevelColor(level slog.Level) string {
	switch level {
	case slog.LevelError:
		return "#ef4444" // red
	case slog.LevelWarn:
		return "#f59e0b" // amber
	case slog.LevelInfo:
		return "#3b82f6" // blue
	case slog.LevelDebug:
		return "#8b5cf6" // violet
	default:
		return "#6b7280" // gray
	}
}

// getTotalLogCount estimates total logs processed
func (ls *LogStreamer) getTotalLogCount() int64 {
	// This is a rough estimate since we don't track every log individually
	// Could be enhanced with a counter if needed
	return int64(time.Since(ls.startTime).Milliseconds() / 100) // rough estimate
}

// Close shuts down the log streamer
func (ls *LogStreamer) Close() {
	close(ls.logChan)

	// Close all client channels
	ls.clientsMux.Lock()
	for clientChan := range ls.clients {
		close(clientChan)
		delete(ls.clients, clientChan)
	}
	ls.clientsMux.Unlock()
}
