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
	logChan       chan string   // Async logging channel
	done          chan struct{} // Signal when logging is done
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

	// Write only to file, not to stderr (to avoid interfering with TUI)
	logWriter = logFile
	enabled = true
	sessionID = time.Now().Format("20060102-150405")

	// Create async logging channel
	logChan = make(chan string, 1000) // Buffer up to 1000 messages
	done = make(chan struct{})

	// Start async writer goroutine
	go func() {
		for msg := range logChan {
			fmt.Fprintln(logWriter, msg)
		}
		close(done)
	}()

	Log("=== Llemecode Session Started ===")
	Log("Session ID: %s", sessionID)
	Log("Log file: %s", filePath)
	Log("================================")

	return nil
}

// Close closes the log file
func Close() {
	mu.Lock()
	wasEnabled := enabled
	enabled = false
	mu.Unlock()

	if wasEnabled {
		Log("=== Session Ended ===")
		close(logChan)
		<-done // Wait for all messages to be written

		mu.Lock()
		if logFile != nil {
			logFile.Close()
			logFile = nil
		}
		mu.Unlock()
	}
}

// Log writes a log message
func Log(format string, args ...interface{}) {
	mu.Lock()
	isEnabled := enabled
	mu.Unlock()

	if !isEnabled {
		// Still print to stderr for debugging even without file logging
		fmt.Fprintf(os.Stderr, "[DEBUG] "+format+"\n", args...)
		return
	}

	timestamp := time.Now().Format("15:04:05.000")
	message := fmt.Sprintf(format, args...)
	logLine := fmt.Sprintf("[%s] %s", timestamp, message)

	// Non-blocking send to channel
	select {
	case logChan <- logLine:
	default:
		// Channel full, log dropped (shouldn't happen with 1000 buffer)
		fmt.Fprintf(os.Stderr, "[WARN] Log buffer full, message dropped\n")
	}
}

// LogConversation logs a conversation message
func LogConversation(role, content string) {
	mu.Lock()
	isEnabled := enabled
	mu.Unlock()

	if !isEnabled {
		return
	}

	timestamp := time.Now().Format("15:04:05.000")
	logLine := fmt.Sprintf("\n[%s] === %s ===\n%s", timestamp, role, content)

	select {
	case logChan <- logLine:
	default:
		fmt.Fprintf(os.Stderr, "[WARN] Log buffer full, conversation dropped\n")
	}
}

// LogToolCall logs a tool invocation
func LogToolCall(name string, args map[string]interface{}, result string, err error) {
	mu.Lock()
	isEnabled := enabled
	mu.Unlock()

	if !isEnabled {
		return
	}

	timestamp := time.Now().Format("15:04:05.000")
	var logLine string
	if err != nil {
		logLine = fmt.Sprintf("\n[%s] === TOOL CALL: %s ===\nArguments: %v\nError: %v\n=========================",
			timestamp, name, args, err)
	} else {
		logLine = fmt.Sprintf("\n[%s] === TOOL CALL: %s ===\nArguments: %v\nResult: %s\n=========================",
			timestamp, name, args, result)
	}

	select {
	case logChan <- logLine:
	default:
		fmt.Fprintf(os.Stderr, "[WARN] Log buffer full, tool call dropped\n")
	}
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
	updater := statusUpdater
	isEnabled := enabled
	mu.Unlock()

	// Update status bar if callback is set
	if updater != nil {
		updater(message)
	}

	// Also log to file if enabled
	if isEnabled {
		timestamp := time.Now().Format("15:04:05.000")
		logLine := fmt.Sprintf("[%s] STATUS: %s", timestamp, message)
		select {
		case logChan <- logLine:
		default:
			// Buffer full, skip logging this status message
		}
	}
}
