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
	hosts     map[string]bool             // all registered hosts
	snapshots map[string]*model.Snapshot  // keyed by host
	changes   map[string]*model.ChangeSet // latest changes per host
	errors    map[string]error            // latest error per host (nil = no error)
}

// Update represents a store update event
type Update struct {
	Host      string
	Snapshot  *model.Snapshot
	ChangeSet *model.ChangeSet
	Error     error
}

// New creates a new store
func New() *Store {
	s := &Store{}
	data := &storeData{
		hosts:     make(map[string]bool),
		snapshots: make(map[string]*model.Snapshot),
		changes:   make(map[string]*model.ChangeSet),
		errors:    make(map[string]error),
	}
	s.current.Store(data)
	return s
}

// RegisterHosts registers a list of hosts that will be monitored
// This ensures the store knows about all configured hosts even before they connect
func (s *Store) RegisterHosts(hosts []string) {
	oldData := s.current.Load()
	newData := &storeData{
		hosts:     make(map[string]bool, len(hosts)),
		snapshots: make(map[string]*model.Snapshot, len(oldData.snapshots)),
		changes:   make(map[string]*model.ChangeSet, len(oldData.changes)),
		errors:    make(map[string]error, len(oldData.errors)),
	}
	
	// Copy existing data
	for k, v := range oldData.hosts {
		newData.hosts[k] = v
	}
	for k, v := range oldData.snapshots {
		newData.snapshots[k] = v
	}
	for k, v := range oldData.changes {
		newData.changes[k] = v
	}
	for k, v := range oldData.errors {
		newData.errors[k] = v
	}
	
	// Register all hosts
	for _, host := range hosts {
		newData.hosts[host] = true
		// Don't set initial state - let the absence of snapshot/error indicate fetching
	}
	
	s.current.Store(newData)
}

// UpdateSnapshot updates the snapshot for a host
func (s *Store) UpdateSnapshot(snapshot *model.Snapshot, changeSet *model.ChangeSet) {
	// Create new data (copy-on-write)
	oldData := s.current.Load()
	newData := &storeData{
		hosts:     make(map[string]bool),
		snapshots: make(map[string]*model.Snapshot),
		changes:   make(map[string]*model.ChangeSet),
		errors:    make(map[string]error),
	}

	// Copy existing data
	for k, v := range oldData.hosts {
		newData.hosts[k] = v
	}
	for k, v := range oldData.snapshots {
		newData.snapshots[k] = v
	}
	for k, v := range oldData.changes {
		newData.changes[k] = v
	}
	for k, v := range oldData.errors {
		newData.errors[k] = v
	}

	// Update with new data
	newData.snapshots[snapshot.Host] = snapshot
	if changeSet != nil && !changeSet.IsEmpty() {
		newData.changes[snapshot.Host] = changeSet
	}
	// Clear any previous error for this host since we got a snapshot
	newData.errors[snapshot.Host] = nil

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

// UpdateError updates the error status for a host
func (s *Store) UpdateError(host string, err error) {
	// Create new data (copy-on-write)
	oldData := s.current.Load()
	
	// Check if error actually changed
	currentErr, exists := oldData.errors[host]
	if exists && currentErr != nil && err != nil && currentErr.Error() == err.Error() {
		// Same error, no change needed
		return
	}
	if !exists && err == nil {
		// No error before, no error now, no change needed
		return
	}
	if exists && currentErr == nil && err == nil {
		// No error before, no error now, no change needed
		return
	}
	
	newData := &storeData{
		hosts:     make(map[string]bool),
		snapshots: make(map[string]*model.Snapshot),
		changes:   make(map[string]*model.ChangeSet),
		errors:    make(map[string]error),
	}

	// Copy existing data
	for k, v := range oldData.hosts {
		newData.hosts[k] = v
	}
	for k, v := range oldData.snapshots {
		newData.snapshots[k] = v
	}
	for k, v := range oldData.changes {
		newData.changes[k] = v
	}
	for k, v := range oldData.errors {
		newData.errors[k] = v
	}

	// Update error
	newData.errors[host] = err

	// Atomic swap
	s.current.Store(newData)

	// Notify subscribers only when there's an actual change
	s.notifySubscribers(Update{
		Host:  host,
		Error: err,
	})
}

// GetErrors returns all current errors
func (s *Store) GetErrors() map[string]error {
	data := s.current.Load()
	// Return a copy to prevent external modification
	result := make(map[string]error)
	for k, v := range data.errors {
		// Only include hosts with actual errors (not nil)
		if v != nil {
			result[k] = v
		}
	}
	return result
}

// GetAllHosts returns all registered hosts
func (s *Store) GetAllHosts() []string {
	data := s.current.Load()
	hosts := make([]string, 0, len(data.hosts))
	for host := range data.hosts {
		hosts = append(hosts, host)
	}
	return hosts
}

// GetFetchingHosts returns hosts that are currently being fetched
// A host is considered fetching if it has no snapshot and no error
func (s *Store) GetFetchingHosts() map[string]bool {
	data := s.current.Load()
	result := make(map[string]bool)
	
	for host := range data.hosts {
		_, hasSnapshot := data.snapshots[host]
		err, hasError := data.errors[host]
		
		// Host is fetching if it has no snapshot and no error (or nil error)
		if !hasSnapshot && (!hasError || err == nil) {
			result[host] = true
		}
	}
	
	return result
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
