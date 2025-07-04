package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/anyproto/goru/internal/collector"
	"github.com/anyproto/goru/internal/store"
	"github.com/anyproto/goru/pkg/model"
)

// Mock source for testing
type mockSource struct {
	name      string
	snapshots []*model.Snapshot
	interval  time.Duration
}

func (m *mockSource) Name() string {
	return m.name
}

func (m *mockSource) Collect(ctx context.Context, snapshots chan<- *model.Snapshot) error {
	defer close(snapshots)

	for _, snapshot := range m.snapshots {
		select {
		case snapshots <- snapshot:
		case <-ctx.Done():
			return ctx.Err()
		}

		if m.interval > 0 {
			select {
			case <-time.After(m.interval):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	return nil
}

func TestOrchestratorBasic(t *testing.T) {
	s := store.New()

	// Create mock source
	source := &mockSource{
		name: "test",
		snapshots: []*model.Snapshot{
			{
				Host:    "test-host",
				TakenAt: time.Now(),
				Groups: map[model.GroupID]*model.Group{
					"g1": {ID: "g1", Count: 5},
				},
			},
		},
	}

	o := New(s, source)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Subscribe to store updates
	updates := make(chan store.Update, 1)
	s.Subscribe(updates)

	// Start orchestrator
	go o.Start(ctx)

	// Wait for update
	select {
	case update := <-updates:
		if update.Host != "test-host" {
			t.Errorf("Expected host test-host, got %s", update.Host)
		}
		if update.Snapshot == nil {
			t.Error("Expected snapshot, got nil")
		}
		if update.ChangeSet == nil {
			t.Error("Expected changeset, got nil")
		}
		if len(update.ChangeSet.Added) != 1 {
			t.Errorf("Expected 1 added group, got %d", len(update.ChangeSet.Added))
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("No update received")
	}
}

func TestOrchestratorMultipleSources(t *testing.T) {
	s := store.New()

	// Create multiple sources
	sources := []collector.Source{
		&mockSource{
			name: "source1",
			snapshots: []*model.Snapshot{
				{
					Host:    "host1",
					TakenAt: time.Now(),
					Groups: map[model.GroupID]*model.Group{
						"g1": {ID: "g1", Count: 1},
					},
				},
			},
		},
		&mockSource{
			name: "source2",
			snapshots: []*model.Snapshot{
				{
					Host:    "host2",
					TakenAt: time.Now(),
					Groups: map[model.GroupID]*model.Group{
						"g2": {ID: "g2", Count: 2},
					},
				},
			},
		},
	}

	o := New(s, sources...)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Start orchestrator
	go o.Start(ctx)

	// Wait a bit for processing
	time.Sleep(50 * time.Millisecond)

	// Check store has both hosts
	stats := o.GetStats()
	if stats.HostsMonitored != 2 {
		t.Errorf("Expected 2 hosts monitored, got %d", stats.HostsMonitored)
	}

	if stats.ActiveSources != 2 {
		t.Errorf("Expected 2 active sources, got %d", stats.ActiveSources)
	}

	// Verify snapshots in store
	all := s.GetAllSnapshots()
	if len(all) != 2 {
		t.Errorf("Expected 2 snapshots in store, got %d", len(all))
	}
}

func TestOrchestratorDiffComputation(t *testing.T) {
	s := store.New()

	// Create source with evolving snapshots
	source := &mockSource{
		name: "test",
		snapshots: []*model.Snapshot{
			{
				Host:    "test-host",
				TakenAt: time.Now(),
				Groups: map[model.GroupID]*model.Group{
					"g1": {ID: "g1", Count: 5},
				},
			},
			{
				Host:    "test-host",
				TakenAt: time.Now().Add(time.Second),
				Groups: map[model.GroupID]*model.Group{
					"g1": {ID: "g1", Count: 10}, // Count increased
					"g2": {ID: "g2", Count: 3},  // New group
				},
			},
		},
		interval: 20 * time.Millisecond,
	}

	o := New(s, source)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Subscribe to store updates
	updates := make(chan store.Update, 10)
	s.Subscribe(updates)

	// Start orchestrator
	go o.Start(ctx)

	// Collect updates
	var changesets []*model.ChangeSet
	timeout := time.After(80 * time.Millisecond)

	for {
		select {
		case update := <-updates:
			if update.ChangeSet != nil {
				changesets = append(changesets, update.ChangeSet)
			}
		case <-timeout:
			goto done
		}
	}

done:
	if len(changesets) < 2 {
		t.Fatalf("Expected at least 2 changesets, got %d", len(changesets))
	}

	// First changeset should have 1 added group
	if len(changesets[0].Added) != 1 {
		t.Errorf("First changeset should have 1 added group, got %d", len(changesets[0].Added))
	}

	// Second changeset should have 1 added group and 1 update
	if len(changesets[1].Added) != 1 {
		t.Errorf("Second changeset should have 1 added group, got %d", len(changesets[1].Added))
	}

	if len(changesets[1].Updated) != 1 {
		t.Errorf("Second changeset should have 1 update, got %d", len(changesets[1].Updated))
	}

	if delta := changesets[1].Updated["g1"]; delta != 5 {
		t.Errorf("Expected count delta +5 for g1, got %d", delta)
	}
}

func TestOrchestratorNoSources(t *testing.T) {
	s := store.New()
	o := New(s) // No sources

	ctx := context.Background()
	err := o.Start(ctx)

	if err == nil {
		t.Error("Expected error for no sources")
	}
}

func TestOrchestratorContextCancellation(t *testing.T) {
	s := store.New()

	// Create a source that would run forever
	source := &mockSource{
		name:     "test",
		interval: time.Hour,
		snapshots: []*model.Snapshot{
			{Host: "test-host", Groups: make(map[model.GroupID]*model.Group)},
			{Host: "test-host", Groups: make(map[model.GroupID]*model.Group)},
		},
	}

	o := New(s, source)

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- o.Start(ctx)
	}()

	// Cancel after a short time
	time.Sleep(10 * time.Millisecond)
	cancel()

	// Should get context error
	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Orchestrator didn't stop on context cancellation")
	}
}
