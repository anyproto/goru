package model

import (
	"testing"
	"time"
)

func TestStackTraceString(t *testing.T) {
	trace := StackTrace{
		{Func: "main.main", File: "/app/main.go", Line: 10},
		{Func: "runtime.goexit", File: "/usr/local/go/src/runtime/asm_amd64.s", Line: 1571},
	}

	expected := "main.main /app/main.go:10\nruntime.goexit /usr/local/go/src/runtime/asm_amd64.s:1571"
	if got := trace.String(); got != expected {
		t.Errorf("StackTrace.String() = %q, want %q", got, expected)
	}
}

func TestGroupGenerateID(t *testing.T) {
	g1 := &Group{
		State: StateRunning,
		Trace: StackTrace{
			{Func: "main.worker", File: "main.go", Line: 42},
		},
	}

	g2 := &Group{
		State: StateRunning,
		Trace: StackTrace{
			{Func: "main.worker", File: "main.go", Line: 42},
		},
	}

	g3 := &Group{
		State: StateWaiting,
		Trace: StackTrace{
			{Func: "main.worker", File: "main.go", Line: 42},
		},
	}

	id1 := g1.GenerateID()
	id2 := g2.GenerateID()
	id3 := g3.GenerateID()

	if id1 != id2 {
		t.Errorf("Same groups should generate same ID: %s != %s", id1, id2)
	}

	if id1 == id3 {
		t.Errorf("Different states should generate different IDs: %s == %s", id1, id3)
	}
}

func TestSnapshotAddGoroutine(t *testing.T) {
	s := NewSnapshot("test-host")

	trace1 := StackTrace{{Func: "main.worker"}}
	trace2 := StackTrace{{Func: "main.handler"}}

	s.AddGoroutine(StateRunning, trace1, "")
	s.AddGoroutine(StateRunning, trace1, "")
	s.AddGoroutine(StateWaiting, trace1, "5m")
	s.AddGoroutine(StateWaiting, trace2, "10s")

	if len(s.Groups) != 3 {
		t.Errorf("Expected 3 groups, got %d", len(s.Groups))
	}

	var runningWorker *Group
	for _, g := range s.Groups {
		if g.State == StateRunning && g.Trace[0].Func == "main.worker" {
			runningWorker = g
			break
		}
	}

	if runningWorker == nil {
		t.Fatal("Running worker group not found")
	}

	if runningWorker.Count != 2 {
		t.Errorf("Expected count 2, got %d", runningWorker.Count)
	}

	total := s.TotalGoroutines()
	if total != 4 {
		t.Errorf("Expected total 4 goroutines, got %d", total)
	}
}

func TestSnapshotWaitDurations(t *testing.T) {
	s := NewSnapshot("test-host")
	trace := StackTrace{{Func: "main.waiter"}}

	s.AddGoroutine(StateWaiting, trace, "1m")
	s.AddGoroutine(StateWaiting, trace, "2m")
	s.AddGoroutine(StateWaiting, trace, "")

	var group *Group
	for _, g := range s.Groups {
		if g.Trace[0].Func == "main.waiter" {
			group = g
			break
		}
	}

	if group == nil {
		t.Fatal("Group not found")
	}

	if len(group.WaitDurations) != 2 {
		t.Errorf("Expected 2 wait durations, got %d", len(group.WaitDurations))
	}

	if group.Count != 3 {
		t.Errorf("Expected count 3, got %d", group.Count)
	}
}

func TestChangeSetIsEmpty(t *testing.T) {
	c := NewChangeSet("test-host")

	if !c.IsEmpty() {
		t.Error("New ChangeSet should be empty")
	}

	c.Added = []*Group{{ID: "test"}}
	if c.IsEmpty() {
		t.Error("ChangeSet with additions should not be empty")
	}

	c = NewChangeSet("test-host")
	c.Updated["test"] = 5
	if c.IsEmpty() {
		t.Error("ChangeSet with updates should not be empty")
	}
}

func TestNewSnapshot(t *testing.T) {
	before := time.Now()
	s := NewSnapshot("test-host")
	after := time.Now()

	if s.Host != "test-host" {
		t.Errorf("Host = %q, want %q", s.Host, "test-host")
	}

	if s.TakenAt.Before(before) || s.TakenAt.After(after) {
		t.Error("TakenAt timestamp not within expected range")
	}

	if s.Groups == nil {
		t.Error("Groups map should be initialized")
	}

	if len(s.Groups) != 0 {
		t.Error("Groups map should be empty")
	}
}
