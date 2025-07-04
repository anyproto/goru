package store

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/anyproto/goru/pkg/model"
)

func TestStoreUpdateSnapshot(t *testing.T) {
	store := New()

	snapshot := &model.Snapshot{
		Host:    "test-host",
		TakenAt: time.Now(),
		Groups: map[model.GroupID]*model.Group{
			"g1": {ID: "g1", Count: 5},
			"g2": {ID: "g2", Count: 3},
		},
	}

	changeSet := &model.ChangeSet{
		Host:  "test-host",
		Added: []*model.Group{{ID: "g1", Count: 5}},
	}

	store.UpdateSnapshot(snapshot, changeSet)

	// Verify snapshot is stored
	retrieved := store.GetSnapshot("test-host")
	if retrieved == nil {
		t.Fatal("Snapshot not found")
	}

	if retrieved.Host != "test-host" {
		t.Errorf("Host = %q, want %q", retrieved.Host, "test-host")
	}

	if len(retrieved.Groups) != 2 {
		t.Errorf("Groups count = %d, want 2", len(retrieved.Groups))
	}

	// Verify changeset is stored
	changes := store.GetChangeSet("test-host")
	if changes == nil {
		t.Fatal("ChangeSet not found")
	}

	if len(changes.Added) != 1 {
		t.Errorf("Added count = %d, want 1", len(changes.Added))
	}
}

func TestStoreGetAllSnapshots(t *testing.T) {
	store := New()

	// Add multiple snapshots
	hosts := []string{"host1", "host2", "host3"}
	for _, host := range hosts {
		snapshot := &model.Snapshot{
			Host:    host,
			TakenAt: time.Now(),
			Groups:  make(map[model.GroupID]*model.Group),
		}
		store.UpdateSnapshot(snapshot, nil)
	}

	all := store.GetAllSnapshots()
	if len(all) != 3 {
		t.Errorf("Expected 3 snapshots, got %d", len(all))
	}

	for _, host := range hosts {
		if _, exists := all[host]; !exists {
			t.Errorf("Missing snapshot for host %s", host)
		}
	}
}

func TestStoreSubscriptions(t *testing.T) {
	store := New()

	// Create subscriber
	ch := make(chan Update, 1)
	store.Subscribe(ch)

	// Send update
	snapshot := &model.Snapshot{
		Host:    "test-host",
		TakenAt: time.Now(),
		Groups:  make(map[model.GroupID]*model.Group),
	}
	changeSet := &model.ChangeSet{
		Host:  "test-host",
		Added: []*model.Group{{ID: "g1"}},
	}

	store.UpdateSnapshot(snapshot, changeSet)

	// Check notification received
	select {
	case update := <-ch:
		if update.Host != "test-host" {
			t.Errorf("Update host = %q, want %q", update.Host, "test-host")
		}
		if update.Snapshot == nil {
			t.Error("Update snapshot is nil")
		}
		if update.ChangeSet == nil {
			t.Error("Update changeset is nil")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("No update received")
	}

	// Unsubscribe
	store.Unsubscribe(ch)

	// Send another update
	store.UpdateSnapshot(snapshot, changeSet)

	// Should not receive notification
	select {
	case <-ch:
		t.Error("Received update after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

func TestStoreConcurrentAccess(t *testing.T) {
	store := New()

	var wg sync.WaitGroup

	// Multiple writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				snapshot := &model.Snapshot{
					Host:    fmt.Sprintf("host%d", id),
					TakenAt: time.Now(),
					Groups: map[model.GroupID]*model.Group{
						model.GroupID(fmt.Sprintf("g%d", j)): {
							ID:    model.GroupID(fmt.Sprintf("g%d", j)),
							Count: j,
						},
					},
				}
				store.UpdateSnapshot(snapshot, nil)
			}
		}(i)
	}

	// Multiple readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = store.GetSnapshot(fmt.Sprintf("host%d", id))
				_ = store.GetAllSnapshots()
				_ = store.GetStats()
			}
		}(i)
	}

	// Multiple subscribers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := make(chan Update, 10)
			store.Subscribe(ch)

			// Read some updates
			timeout := time.After(100 * time.Millisecond)
			for {
				select {
				case <-ch:
					// Got update
				case <-timeout:
					store.Unsubscribe(ch)
					return
				}
			}
		}()
	}

	wg.Wait()

	// Verify final state
	stats := store.GetStats()
	if stats.Hosts != 10 {
		t.Errorf("Expected 10 hosts, got %d", stats.Hosts)

		// Debug: print all hosts
		all := store.GetAllSnapshots()
		t.Logf("Hosts found: %v", len(all))
		for host := range all {
			t.Logf("  - %s", host)
		}
	}
}

func TestStoreStats(t *testing.T) {
	store := New()

	// Add subscribers
	ch1 := make(chan Update)
	ch2 := make(chan Update)
	store.Subscribe(ch1)
	store.Subscribe(ch2)

	// Add snapshots
	snapshot1 := &model.Snapshot{
		Host: "host1",
		Groups: map[model.GroupID]*model.Group{
			"g1": {ID: "g1", Count: 5},
			"g2": {ID: "g2", Count: 3},
		},
	}

	snapshot2 := &model.Snapshot{
		Host: "host2",
		Groups: map[model.GroupID]*model.Group{
			"g3": {ID: "g3", Count: 10},
		},
	}

	store.UpdateSnapshot(snapshot1, nil)
	store.UpdateSnapshot(snapshot2, nil)

	stats := store.GetStats()

	if stats.Hosts != 2 {
		t.Errorf("Hosts = %d, want 2", stats.Hosts)
	}

	if stats.TotalGroups != 3 {
		t.Errorf("TotalGroups = %d, want 3", stats.TotalGroups)
	}

	if stats.TotalGoroutines != 18 {
		t.Errorf("TotalGoroutines = %d, want 18", stats.TotalGoroutines)
	}

	if stats.SubscriberCount != 2 {
		t.Errorf("SubscriberCount = %d, want 2", stats.SubscriberCount)
	}
}

func TestStoreEmptyChangeSet(t *testing.T) {
	store := New()

	snapshot := &model.Snapshot{
		Host:    "test-host",
		TakenAt: time.Now(),
		Groups:  make(map[model.GroupID]*model.Group),
	}

	// Empty changeset should not be stored
	emptyChangeSet := &model.ChangeSet{
		Host: "test-host",
	}

	store.UpdateSnapshot(snapshot, emptyChangeSet)

	// Should not find changeset
	changes := store.GetChangeSet("test-host")
	if changes != nil {
		t.Error("Empty changeset should not be stored")
	}
}
