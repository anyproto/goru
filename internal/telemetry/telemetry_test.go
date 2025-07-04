package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLoggerLevels(t *testing.T) {
	tests := []struct {
		level    string
		logDebug bool
		logInfo  bool
		logWarn  bool
		logError bool
	}{
		{"debug", true, true, true, true},
		{"info", false, true, true, true},
		{"warn", false, false, true, true},
		{"error", false, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			// Redirect stderr to capture output
			r, w, _ := os.Pipe()
			oldStderr := os.Stderr
			os.Stderr = w

			logger := NewLogger(tt.level, false)

			logger.Debug("debug message")
			logger.Info("info message")
			logger.Warn("warn message")
			logger.Error("error message")

			w.Close()
			os.Stderr = oldStderr

			buf := make([]byte, 4096)
			n, _ := r.Read(buf)
			output := string(buf[:n])

			hasDebug := strings.Contains(output, "debug message")
			hasInfo := strings.Contains(output, "info message")
			hasWarn := strings.Contains(output, "warn message")
			hasError := strings.Contains(output, "error message")

			if hasDebug != tt.logDebug {
				t.Errorf("Debug log: got %v, want %v", hasDebug, tt.logDebug)
			}
			if hasInfo != tt.logInfo {
				t.Errorf("Info log: got %v, want %v", hasInfo, tt.logInfo)
			}
			if hasWarn != tt.logWarn {
				t.Errorf("Warn log: got %v, want %v", hasWarn, tt.logWarn)
			}
			if hasError != tt.logError {
				t.Errorf("Error log: got %v, want %v", hasError, tt.logError)
			}
		})
	}
}

func TestLoggerFields(t *testing.T) {
	// Redirect stderr to capture output
	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()

	logger := NewLogger("info", false)
	logger.Info("test message",
		String("key1", "value1"),
		Int("key2", 42),
		Error(fmt.Errorf("test error")),
	)

	w.Close()

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "test message") {
		t.Error("Missing message in output")
	}
	if !strings.Contains(output, "key1=value1") {
		t.Error("Missing string field in output")
	}
	if !strings.Contains(output, "key2=42") {
		t.Error("Missing int field in output")
	}
	if !strings.Contains(output, "error=test error") {
		t.Error("Missing error field in output")
	}
}

func TestLoggerJSON(t *testing.T) {
	// Redirect stderr to capture output
	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()

	logger := NewLogger("info", true)
	logger.Info("test message", String("key", "value"))

	w.Close()

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.HasPrefix(output, "{") || !strings.HasSuffix(strings.TrimSpace(output), "}") {
		t.Error("Output is not JSON format")
	}

	if !strings.Contains(output, `"level":"INFO"`) {
		t.Error("Missing level in JSON output")
	}
	if !strings.Contains(output, `"msg":"test message"`) {
		t.Error("Missing message in JSON output")
	}
	if !strings.Contains(output, `"key":"value"`) {
		t.Error("Missing field in JSON output")
	}
}

func TestLoggerWith(t *testing.T) {
	// Redirect stderr to capture output
	r, w, _ := os.Pipe()
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
	}()

	logger := NewLogger("info", false)
	childLogger := logger.With(String("component", "test"))

	childLogger.Info("child message", String("extra", "field"))

	w.Close()

	buf := make([]byte, 1024)
	n, _ := r.Read(buf)
	output := string(buf[:n])

	if !strings.Contains(output, "component=test") {
		t.Error("Missing inherited field in output")
	}
	if !strings.Contains(output, "extra=field") {
		t.Error("Missing additional field in output")
	}
}

func TestStartPProf(t *testing.T) {
	logger := NewLogger("info", false)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start pprof server
	err := StartPProf(ctx, "localhost:0", logger)
	if err != nil {
		t.Fatal(err)
	}

	// Give server time to start
	time.Sleep(50 * time.Millisecond)

	// Try to access pprof endpoint
	resp, err := http.Get("http://localhost:0/debug/pprof/")
	if err == nil {
		defer resp.Body.Close()
		// We expect an error since we're using port 0
	}

	// Test with empty address (should not start)
	err = StartPProf(ctx, "", logger)
	if err != nil {
		t.Error("StartPProf with empty address should return nil")
	}
}

func TestFieldHelpers(t *testing.T) {
	// Test String field
	f := String("key", "value")
	if f.Key != "key" || f.Value != "value" {
		t.Error("String field incorrect")
	}

	// Test Int field
	f = Int("count", 42)
	if f.Key != "count" || f.Value != 42 {
		t.Error("Int field incorrect")
	}

	// Test Error field
	err := fmt.Errorf("test error")
	f = Error(err)
	if f.Key != "error" || f.Value != err {
		t.Error("Error field incorrect")
	}

	// Test Duration field
	d := 5 * time.Second
	f = Duration("elapsed", d)
	if f.Key != "elapsed" || f.Value != d {
		t.Error("Duration field incorrect")
	}
}
