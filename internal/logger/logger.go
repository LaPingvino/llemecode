package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

var (
	logFile       *os.File
	logWriter     io.Writer
	mu            sync.Mutex
	enabled       bool
	statusUpdater func(string) // Callback to update status bar in TUI
	sessionID     string
)

// Init initializes the logger with a file path
func Init(filePath string) error {
	if filePath == "" {
		enabled = false
		return nil
	}

	mu.Lock()
	defer mu.Unlock()

	var err error
	logFile, err = os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	// Write to both file and stderr
	logWriter = io.MultiWriter(logFile, os.Stderr)
	enabled = true
	sessionID = time.Now().Format("20060102-150405")

	Log("=== Llemecode Session Started ===")
	Log("Session ID: %s", sessionID)
	Log("Log file: %s", filePath)
	Log("================================")

	return nil
}

// Close closes the log file
func Close() {
	mu.Lock()
	defer mu.Unlock()

	if logFile != nil {
		Log("=== Session Ended ===")
		logFile.Close()
		logFile = nil
	}
	enabled = false
}

// Log writes a log message
func Log(format string, args ...interface{}) {
	if !enabled {
		// Still print to stderr for debugging even without file logging
		fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
		return
	}

	mu.Lock()
	defer mu.Unlock()

	timestamp := time.Now().Format("15:04:05.000")
	message := fmt.Sprintf(format, args...)
	fmt.Fprintf(logWriter, "[%s] %s\n", timestamp, message)
}

// LogConversation logs a conversation message
func LogConversation(role, content string) {
	if !enabled {
		return
	}

	mu.Lock()
	defer mu.Unlock()

	timestamp := time.Now().Format("15:04:05.000")
	fmt.Fprintf(logWriter, "\n[%s] === %s ===\n%s\n", timestamp, role, content)
}

// LogToolCall logs a tool invocation
func LogToolCall(name string, args map[string]interface{}, result string, err error) {
	if !enabled {
		return
	}

	mu.Lock()
	defer mu.Unlock()

	timestamp := time.Now().Format("15:04:05.000")
	fmt.Fprintf(logWriter, "\n[%s] === TOOL CALL: %s ===\n", timestamp, name)
	fmt.Fprintf(logWriter, "Arguments: %v\n", args)
	if err != nil {
		fmt.Fprintf(logWriter, "Error: %v\n", err)
	} else {
		fmt.Fprintf(logWriter, "Result: %s\n", result)
	}
	fmt.Fprintf(logWriter, "=========================\n")
}

// IsEnabled returns whether logging is enabled
func IsEnabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return enabled
}

// SetStatusUpdater sets a callback function to update the status bar
func SetStatusUpdater(updater func(string)) {
	mu.Lock()
	defer mu.Unlock()
	statusUpdater = updater
}

// Status logs a status message and updates the status bar if available
func Status(format string, args ...interface{}) {
	message := fmt.Sprintf(format, args...)

	mu.Lock()
	defer mu.Unlock()

	// Update status bar if callback is set
	if statusUpdater != nil {
		statusUpdater(message)
	}

	// Also log to file if enabled
	if enabled {
		timestamp := time.Now().Format("15:04:05.000")
		fmt.Fprintf(logWriter, "[%s] STATUS: %s\n", timestamp, message)
	}
}
