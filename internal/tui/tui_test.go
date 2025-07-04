package tui

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/anyproto/goru/internal/store"
	"github.com/anyproto/goru/pkg/model"
)

func TestModelInit(t *testing.T) {
	s := store.New()
	m := New(s)

	// Init should return commands
	cmd := m.Init()
	if cmd == nil {
		t.Error("Init should return commands")
	}
}

func TestModelView(t *testing.T) {
	s := store.New()
	m := New(s)

	// View without size should show loading
	view := m.View()
	if view != "Loading..." {
		t.Errorf("Expected loading message, got %q", view)
	}

	// Set size
	m.width = 80
	m.height = 24

	// View should render
	view = m.View()
	if view == "Loading..." {
		t.Error("Should render after size is set")
	}
}

func TestModelUpdate(t *testing.T) {
	s := store.New()

	// Add test data
	snapshot := &model.Snapshot{
		Host:    "test-host",
		TakenAt: time.Now(),
		Groups: map[model.GroupID]*model.Group{
			"g1": {
				ID:    "g1",
				State: model.StateRunning,
				Count: 5,
				Trace: model.StackTrace{{Func: "main.worker"}},
			},
		},
	}
	s.UpdateSnapshot(snapshot, nil)

	m := New(s)

	// Test window size message
	msg := tea.WindowSizeMsg{Width: 100, Height: 30}
	newModel, _ := m.Update(msg)
	m = newModel.(Model)

	if m.width != 100 || m.height != 30 {
		t.Error("Window size not updated")
	}

	// Test quit
	quitMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}
	_, cmd := m.Update(quitMsg)

	// Should return quit command
	if cmd == nil {
		t.Error("Expected quit command")
	}
}

func TestBuildTableRows(t *testing.T) {
	s := store.New()

	// Add test data
	snapshot := &model.Snapshot{
		Host:    "test-host",
		TakenAt: time.Now(),
		Groups: map[model.GroupID]*model.Group{
			"g1": {
				ID:            "g1",
				State:         model.StateRunning,
				Count:         10,
				Trace:         model.StackTrace{{Func: "main.worker"}},
				WaitDurations: []string{"5m"},
			},
			"g2": {
				ID:    "g2",
				State: model.StateBlocked,
				Count: 5,
				Trace: model.StackTrace{{Func: "main.handler"}},
			},
		},
	}

	changeSet := &model.ChangeSet{
		Host:  "test-host",
		Added: []*model.Group{{ID: "g1"}},
		Updated: map[model.GroupID]int{
			"g2": 2,
		},
	}

	s.UpdateSnapshot(snapshot, changeSet)

	m := New(s)
	m.selectedHost = "test-host"

	rows := m.buildTableRows()

	if len(rows) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(rows))
	}

	// Check first row (higher count)
	if rows[0][2] != "main.worker" {
		t.Errorf("Expected main.worker first, got %s", rows[0][2])
	}

	if rows[0][3] != "10" {
		t.Errorf("Expected count 10, got %s", rows[0][3])
	}

	if rows[0][4] != "5m" {
		t.Errorf("Expected wait 5m, got %s", rows[0][4])
	}
}

func TestHostNavigation(t *testing.T) {
	s := store.New()

	// Add multiple hosts
	for i := 1; i <= 3; i++ {
		snapshot := &model.Snapshot{
			Host:    fmt.Sprintf("host%d", i),
			TakenAt: time.Now(),
			Groups:  make(map[model.GroupID]*model.Group),
		}
		s.UpdateSnapshot(snapshot, nil)
	}

	m := New(s)
	m.selectedHost = "host1"

	// Test next host
	m.selectNextHost()
	if m.selectedHost != "host2" {
		t.Errorf("Expected host2, got %s", m.selectedHost)
	}

	// Test wrap around
	m.selectedHost = "host3"
	m.selectNextHost()
	if m.selectedHost != "host1" {
		t.Errorf("Expected host1 (wrap), got %s", m.selectedHost)
	}

	// Test prev host
	m.selectedHost = "host2"
	m.selectPrevHost()
	if m.selectedHost != "host1" {
		t.Errorf("Expected host1, got %s", m.selectedHost)
	}

	// Test wrap around backwards
	m.selectedHost = "host1"
	m.selectPrevHost()
	if m.selectedHost != "host3" {
		t.Errorf("Expected host3 (wrap), got %s", m.selectedHost)
	}
}
