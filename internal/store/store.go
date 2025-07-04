package store

import (
	"sync"
	"sync/atomic"

	"github.com/anyproto/goru/pkg/model"
)

// Store manages snapshots and change notifications
type Store struct {
	// Atomic pointer for lock-free reads
	current atomic.Pointer[storeData]

	// Subscribers for changes
	mu          sync.RWMutex
	subscribers []chan<- Update
}

type storeData struct {
	snapshots map[string]*model.Snapshot  // keyed by host
	changes   map[string]*model.ChangeSet // latest changes per host
}

// Update represents a store update event
type Update struct {
	Host      string
	Snapshot  *model.Snapshot
	ChangeSet *model.ChangeSet
}

// New creates a new store
func New() *Store {
	s := &Store{}
	data := &storeData{
		snapshots: make(map[string]*model.Snapshot),
		changes:   make(map[string]*model.ChangeSet),
	}
	s.current.Store(data)
	return s
}

// UpdateSnapshot updates the snapshot for a host
func (s *Store) UpdateSnapshot(snapshot *model.Snapshot, changeSet *model.ChangeSet) {
	// Create new data (copy-on-write)
	oldData := s.current.Load()
	newData := &storeData{
		snapshots: make(map[string]*model.Snapshot),
		changes:   make(map[string]*model.ChangeSet),
	}

	// Copy existing data
	for k, v := range oldData.snapshots {
		newData.snapshots[k] = v
	}
	for k, v := range oldData.changes {
		newData.changes[k] = v
	}

	// Update with new data
	newData.snapshots[snapshot.Host] = snapshot
	if changeSet != nil && !changeSet.IsEmpty() {
		newData.changes[snapshot.Host] = changeSet
	}

	// Atomic swap
	s.current.Store(newData)

	// Notify subscribers
	s.notifySubscribers(Update{
		Host:      snapshot.Host,
		Snapshot:  snapshot,
		ChangeSet: changeSet,
	})
}

// GetSnapshot returns the current snapshot for a host
func (s *Store) GetSnapshot(host string) *model.Snapshot {
	data := s.current.Load()
	return data.snapshots[host]
}

// GetAllSnapshots returns all current snapshots
func (s *Store) GetAllSnapshots() map[string]*model.Snapshot {
	data := s.current.Load()
	// Return a copy to prevent external modification
	result := make(map[string]*model.Snapshot)
	for k, v := range data.snapshots {
		result[k] = v
	}
	return result
}

// GetChangeSet returns the latest changeset for a host
func (s *Store) GetChangeSet(host string) *model.ChangeSet {
	data := s.current.Load()
	return data.changes[host]
}

// Subscribe registers a channel to receive updates
func (s *Store) Subscribe(ch chan<- Update) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subscribers = append(s.subscribers, ch)
}

// Unsubscribe removes a channel from receiving updates
func (s *Store) Unsubscribe(ch chan<- Update) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, sub := range s.subscribers {
		if sub == ch {
			// Remove by swapping with last and truncating
			s.subscribers[i] = s.subscribers[len(s.subscribers)-1]
			s.subscribers = s.subscribers[:len(s.subscribers)-1]
			break
		}
	}
}

func (s *Store) notifySubscribers(update Update) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, ch := range s.subscribers {
		// Non-blocking send
		select {
		case ch <- update:
		default:
			// Subscriber is not ready, skip
		}
	}
}

// Stats returns statistics about the store
type Stats struct {
	Hosts           int
	TotalGroups     int
	TotalGoroutines int
	SubscriberCount int
}

// GetStats returns current store statistics
func (s *Store) GetStats() Stats {
	data := s.current.Load()

	stats := Stats{
		Hosts: len(data.snapshots),
	}

	for _, snapshot := range data.snapshots {
		stats.TotalGroups += len(snapshot.Groups)
		stats.TotalGoroutines += snapshot.TotalGoroutines()
	}

	s.mu.RLock()
	stats.SubscriberCount = len(s.subscribers)
	s.mu.RUnlock()

	return stats
}
