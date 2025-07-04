package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/anyproto/goru/internal/collector"
	"github.com/anyproto/goru/internal/collector/http"
	"github.com/anyproto/goru/internal/diff"
	"github.com/anyproto/goru/internal/store"
	"github.com/anyproto/goru/pkg/model"
)

// Orchestrator coordinates collectors, diff computation, and store updates
type Orchestrator struct {
	sources []collector.Source
	store   *store.Store
	diff    *diff.Diff

	// Track previous snapshots for diff computation
	mu            sync.RWMutex
	lastSnapshots map[string]*model.Snapshot
}

// New creates a new orchestrator
func New(store *store.Store, sources ...collector.Source) *Orchestrator {
	return &Orchestrator{
		sources:       sources,
		store:         store,
		diff:          diff.New(),
		lastSnapshots: make(map[string]*model.Snapshot),
	}
}

// Start begins orchestration
func (o *Orchestrator) Start(ctx context.Context) error {
	if len(o.sources) == 0 {
		return fmt.Errorf("no sources configured")
	}

	// Create channels for each source
	channels := make([]<-chan *model.Snapshot, len(o.sources))

	var wg sync.WaitGroup
	errCh := make(chan error, len(o.sources))

	// Start each source
	for i, source := range o.sources {
		ch := make(chan *model.Snapshot, 10)
		channels[i] = ch

		wg.Add(1)
		go func(src collector.Source, snapshots chan<- *model.Snapshot) {
			defer wg.Done()
			if err := src.Collect(ctx, snapshots); err != nil {
				select {
				case errCh <- fmt.Errorf("%s: %w", src.Name(), err):
				case <-ctx.Done():
				}
			}
		}(source, ch)
	}

	// Start processing snapshots
	go o.processSnapshots(ctx, channels)

	// Start error monitoring for HTTP sources
	go o.monitorErrors(ctx)

	// Wait for completion
	go func() {
		wg.Wait()
		close(errCh)
	}()

	// Return first error if any
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (o *Orchestrator) processSnapshots(ctx context.Context, channels []<-chan *model.Snapshot) {
	// Merge all channels into one
	merged := make(chan *model.Snapshot)

	var wg sync.WaitGroup
	for _, ch := range channels {
		wg.Add(1)
		go func(c <-chan *model.Snapshot) {
			defer wg.Done()
			for snapshot := range c {
				select {
				case merged <- snapshot:
				case <-ctx.Done():
					return
				}
			}
		}(ch)
	}

	// Close merged channel when all sources are done
	go func() {
		wg.Wait()
		close(merged)
	}()

	// Process snapshots
	for {
		select {
		case snapshot, ok := <-merged:
			if !ok {
				return
			}
			o.handleSnapshot(snapshot)
		case <-ctx.Done():
			return
		}
	}
}

func (o *Orchestrator) handleSnapshot(snapshot *model.Snapshot) {
	// Get previous snapshot
	o.mu.RLock()
	lastSnapshot := o.lastSnapshots[snapshot.Host]
	o.mu.RUnlock()

	// Compute diff
	changeSet := o.diff.Compare(lastSnapshot, snapshot)

	// Update store
	o.store.UpdateSnapshot(snapshot, changeSet)

	// Update last snapshot
	o.mu.Lock()
	o.lastSnapshots[snapshot.Host] = snapshot
	o.mu.Unlock()
}

// GetStats returns orchestrator statistics
type Stats struct {
	ActiveSources  int
	HostsMonitored int
	StoreStats     store.Stats
}

func (o *Orchestrator) GetStats() Stats {
	o.mu.RLock()
	hostsMonitored := len(o.lastSnapshots)
	o.mu.RUnlock()

	return Stats{
		ActiveSources:  len(o.sources),
		HostsMonitored: hostsMonitored,
		StoreStats:     o.store.GetStats(),
	}
}

func (o *Orchestrator) monitorErrors(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check each source for errors
			for _, source := range o.sources {
				if httpSource, ok := source.(*http.HTTPSource); ok {
					currentErrors := httpSource.GetErrors()
					sourceTargets := httpSource.GetTargets()
					
					// Update error status only for hosts managed by this source
					for _, host := range sourceTargets {
						if err, hasError := currentErrors[host]; hasError {
							// Host has an error
							o.store.UpdateError(host, err)
						} else {
							// Host is working (no error in the errors map)
							o.store.UpdateError(host, nil)
						}
					}
				}
			}
		}
	}
}
