package diff

import (
	"fmt"
	"testing"

	"github.com/anyproto/goru/pkg/model"
)

func TestDiffCompareNilOld(t *testing.T) {
	d := New()

	newSnapshot := model.NewSnapshot("test-host")
	g1 := &model.Group{
		ID:    "group1",
		State: model.StateRunning,
		Count: 5,
		Trace: model.StackTrace{{Func: "main.worker"}},
	}
	g2 := &model.Group{
		ID:    "group2",
		State: model.StateWaiting,
		Count: 3,
		Trace: model.StackTrace{{Func: "main.handler"}},
	}
	newSnapshot.Groups[g1.ID] = g1
	newSnapshot.Groups[g2.ID] = g2

	changes := d.Compare(nil, newSnapshot)

	if len(changes.Added) != 2 {
		t.Errorf("Expected 2 added groups, got %d", len(changes.Added))
	}

	if len(changes.Removed) != 0 {
		t.Errorf("Expected 0 removed groups, got %d", len(changes.Removed))
	}

	if len(changes.Updated) != 0 {
		t.Errorf("Expected 0 updated groups, got %d", len(changes.Updated))
	}
}

func TestDiffCompareAddedAndRemoved(t *testing.T) {
	d := New()

	// Old snapshot
	oldSnapshot := model.NewSnapshot("test-host")
	g1 := &model.Group{
		ID:    "group1",
		State: model.StateRunning,
		Count: 5,
		Trace: model.StackTrace{{Func: "main.worker"}},
	}
	g2 := &model.Group{
		ID:    "group2",
		State: model.StateWaiting,
		Count: 3,
		Trace: model.StackTrace{{Func: "main.handler"}},
	}
	oldSnapshot.Groups[g1.ID] = g1
	oldSnapshot.Groups[g2.ID] = g2

	// New snapshot
	newSnapshot := model.NewSnapshot("test-host")
	g3 := &model.Group{
		ID:    "group3",
		State: model.StateBlocked,
		Count: 2,
		Trace: model.StackTrace{{Func: "main.processor"}},
	}
	newSnapshot.Groups[g1.ID] = g1 // Keep group1
	newSnapshot.Groups[g3.ID] = g3 // Add group3
	// Remove group2

	changes := d.Compare(oldSnapshot, newSnapshot)

	if len(changes.Added) != 1 {
		t.Errorf("Expected 1 added group, got %d", len(changes.Added))
	}

	if changes.Added[0].ID != "group3" {
		t.Errorf("Expected added group3, got %s", changes.Added[0].ID)
	}

	if len(changes.Removed) != 1 {
		t.Errorf("Expected 1 removed group, got %d", len(changes.Removed))
	}

	if changes.Removed[0].ID != "group2" {
		t.Errorf("Expected removed group2, got %s", changes.Removed[0].ID)
	}
}

func TestDiffCompareCountChanges(t *testing.T) {
	d := New()

	// Old snapshot
	oldSnapshot := model.NewSnapshot("test-host")
	g1 := &model.Group{
		ID:    "group1",
		State: model.StateRunning,
		Count: 5,
		Trace: model.StackTrace{{Func: "main.worker"}},
	}
	g2 := &model.Group{
		ID:    "group2",
		State: model.StateWaiting,
		Count: 3,
		Trace: model.StackTrace{{Func: "main.handler"}},
	}
	oldSnapshot.Groups[g1.ID] = g1
	oldSnapshot.Groups[g2.ID] = g2

	// New snapshot with count changes
	newSnapshot := model.NewSnapshot("test-host")
	g1New := &model.Group{
		ID:    "group1",
		State: model.StateRunning,
		Count: 10, // Increased from 5
		Trace: model.StackTrace{{Func: "main.worker"}},
	}
	g2New := &model.Group{
		ID:    "group2",
		State: model.StateWaiting,
		Count: 1, // Decreased from 3
		Trace: model.StackTrace{{Func: "main.handler"}},
	}
	newSnapshot.Groups[g1New.ID] = g1New
	newSnapshot.Groups[g2New.ID] = g2New

	changes := d.Compare(oldSnapshot, newSnapshot)

	if len(changes.Updated) != 2 {
		t.Errorf("Expected 2 updated groups, got %d", len(changes.Updated))
	}

	if delta := changes.Updated["group1"]; delta != 5 {
		t.Errorf("Expected group1 delta +5, got %d", delta)
	}

	if delta := changes.Updated["group2"]; delta != -2 {
		t.Errorf("Expected group2 delta -2, got %d", delta)
	}
}

func TestDiffStats(t *testing.T) {
	d := New()

	changes := &model.ChangeSet{
		Host: "test-host",
		Added: []*model.Group{
			{Count: 5},
			{Count: 3},
		},
		Removed: []*model.Group{
			{Count: 2},
		},
		Updated: map[model.GroupID]int{
			"g1": 4,  // +4
			"g2": -1, // -1
		},
	}

	stats := d.Stats(changes)

	if stats.GroupsAdded != 2 {
		t.Errorf("GroupsAdded = %d, want 2", stats.GroupsAdded)
	}

	if stats.GroupsRemoved != 1 {
		t.Errorf("GroupsRemoved = %d, want 1", stats.GroupsRemoved)
	}

	if stats.GroupsWithChanges != 2 {
		t.Errorf("GroupsWithChanges = %d, want 2", stats.GroupsWithChanges)
	}

	// Total added: 5 + 3 (new groups) + 4 (increase) = 12
	if stats.TotalAdded != 12 {
		t.Errorf("TotalAdded = %d, want 12", stats.TotalAdded)
	}

	// Total removed: 2 (removed group) + 1 (decrease) = 3
	if stats.TotalRemoved != 3 {
		t.Errorf("TotalRemoved = %d, want 3", stats.TotalRemoved)
	}
}

func TestDiffNoChanges(t *testing.T) {
	d := New()

	// Create identical snapshots
	snapshot := model.NewSnapshot("test-host")
	g1 := &model.Group{
		ID:    "group1",
		State: model.StateRunning,
		Count: 5,
		Trace: model.StackTrace{{Func: "main.worker"}},
	}
	snapshot.Groups[g1.ID] = g1

	changes := d.Compare(snapshot, snapshot)

	if !changes.IsEmpty() {
		t.Error("Expected no changes for identical snapshots")
	}
}

func BenchmarkDiffCompare(b *testing.B) {
	d := New()

	// Create large snapshots
	oldSnapshot := model.NewSnapshot("bench-host")
	newSnapshot := model.NewSnapshot("bench-host")

	// Add 1000 groups to old snapshot
	for i := 0; i < 1000; i++ {
		g := &model.Group{
			ID:    model.GroupID(fmt.Sprintf("group%d", i)),
			State: model.StateRunning,
			Count: i%10 + 1,
			Trace: model.StackTrace{{Func: fmt.Sprintf("func%d", i)}},
		}
		oldSnapshot.Groups[g.ID] = g

		// 80% remain in new snapshot
		if i%5 != 0 {
			gNew := *g
			// 50% have count changes
			if i%2 == 0 {
				gNew.Count = g.Count + 2
			}
			newSnapshot.Groups[gNew.ID] = &gNew
		}
	}

	// Add 200 new groups
	for i := 1000; i < 1200; i++ {
		g := &model.Group{
			ID:    model.GroupID(fmt.Sprintf("group%d", i)),
			State: model.StateWaiting,
			Count: i%5 + 1,
			Trace: model.StackTrace{{Func: fmt.Sprintf("func%d", i)}},
		}
		newSnapshot.Groups[g.ID] = g
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = d.Compare(oldSnapshot, newSnapshot)
	}
}
