package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anyproto/goru/pkg/model"
)

func TestParseSimple(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "simple.txt"))
	if err != nil {
		t.Fatal(err)
	}

	p := New()
	snapshot, err := p.ParseBytes(data, "test-host")
	if err != nil {
		t.Fatal(err)
	}

	// Verify basic counts
	if total := snapshot.TotalGoroutines(); total != 4 {
		t.Errorf("Expected 4 goroutines, got %d", total)
	}

	if len(snapshot.Groups) != 3 {
		t.Errorf("Expected 3 groups, got %d", len(snapshot.Groups))
	}

	// Check for specific groups
	var hasRunning, hasWorkers, hasIOWait bool
	var workerGroup *model.Group

	for _, g := range snapshot.Groups {
		funcs := []string{}
		for _, f := range g.Trace {
			funcs = append(funcs, f.Func)
		}
		t.Logf("Group: state=%s, count=%d, funcs=%v", g.State, g.Count, funcs)

		if g.State == model.StateRunning && g.Trace[0].Func == "main.main" {
			hasRunning = true
			if g.Count != 1 {
				t.Errorf("Running group should have count 1, got %d", g.Count)
			}
		}

		if g.State == model.StateBlocked && g.Trace[0].Func == "main.worker" {
			hasWorkers = true
			workerGroup = g
		}

		if g.State == model.StateWaiting && len(g.Trace) > 0 && g.Trace[0].Func == "net.(*netFD).Read" {
			hasIOWait = true
		}
	}

	if !hasRunning {
		t.Error("Missing running goroutine group")
	}

	if !hasWorkers {
		t.Error("Missing worker goroutine group")
	}

	if !hasIOWait {
		t.Error("Missing IO wait goroutine group")
	}

	// Check worker group details
	if workerGroup != nil {
		if workerGroup.Count != 2 {
			t.Errorf("Worker group should have count 2, got %d", workerGroup.Count)
		}

		if len(workerGroup.WaitDurations) != 2 {
			t.Errorf("Worker group should have 2 wait durations, got %d", len(workerGroup.WaitDurations))
		}

		for _, dur := range workerGroup.WaitDurations {
			if dur != "5 minutes" {
				t.Errorf("Expected wait duration '5 minutes', got %q", dur)
			}
		}
	}
}

func TestExtractFunctionName(t *testing.T) {
	p := New()

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "main.main()",
			expected: "main.main",
		},
		{
			input:    "net.(*netFD).Read(0xc0000a0000, 0xc0000b0000, 0x1000, 0x1000, 0x0, 0x0, 0x0)",
			expected: "net.(*netFD).Read",
		},
		{
			input:    "runtime.gopark(0x123456, 0x0, 0x13, 0x14, 0x1)",
			expected: "runtime.gopark",
		},
		{
			input:    "net.(*conn).Read(0xc0000a8000, 0xc0000b0000, 0x1000, 0x1000, 0x0, 0x0, 0x0)",
			expected: "net.(*conn).Read",
		},
		{
			input:    "net.(*netFD).Read(0xc0000a0000",
			expected: "net.(*netFD).Read",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := p.extractFunctionName(tt.input)
			if got != tt.expected {
				t.Errorf("extractFunctionName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestParseState(t *testing.T) {
	p := New()

	tests := []struct {
		input    string
		expected model.GoroutineState
	}{
		{"running", model.StateRunning},
		{"runnable", model.StateRunnable},
		{"syscall", model.StateSyscall},
		{"chan receive", model.StateBlocked},
		{"chan send", model.StateBlocked},
		{"select", model.StateBlocked},
		{"IO wait", model.StateWaiting},
		{"semacquire", model.StateWaiting},
		{"sleep", model.StateWaiting},
		{"finalizer wait", model.StateWaiting},
		{"chan receive, 5 minutes", model.StateBlocked},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := p.parseState(tt.input)
			if got != tt.expected {
				t.Errorf("parseState(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestStripMemoryAddresses(t *testing.T) {
	p := New()

	tests := []struct {
		input    string
		expected string
	}{
		{
			input:    "function(0x123abc, 0xdeadbeef)",
			expected: "function(...)",
		},
		{
			input:    "field: 0x123456",
			expected: "field: 0x?",
		},
		{
			input:    "no addresses here",
			expected: "no addresses here",
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := p.stripMemoryAddresses(tt.input)
			if got != tt.expected {
				t.Errorf("stripMemoryAddresses(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func BenchmarkParse(b *testing.B) {
	data, err := os.ReadFile(filepath.Join("testdata", "simple.txt"))
	if err != nil {
		b.Fatal(err)
	}

	p := New()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := p.ParseBytes(data, "bench-host")
		if err != nil {
			b.Fatal(err)
		}
	}
}
