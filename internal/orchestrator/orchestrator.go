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
	
	// Centralized refresh control
	refreshCh chan struct{}
	interval  time.Duration
	paused    bool
	pauseMu   sync.RWMutex
}

// New creates a new orchestrator
func New(store *store.Store, interval time.Duration, sources ...collector.Source) *Orchestrator {
	return &Orchestrator{
		sources:       sources,
		store:         store,
		diff:          diff.New(),
		lastSnapshots: make(map[string]*model.Snapshot),
		refreshCh:     make(chan struct{}, 1), // Buffered to avoid blocking
		interval:      interval,
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
	
	// Start centralized refresh controller
	go o.refreshController(ctx)

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

// TriggerRefresh manually triggers a refresh for all sources
func (o *Orchestrator) TriggerRefresh() {
	select {
	case o.refreshCh <- struct{}{}:
		// Refresh triggered
	default:
		// Channel is full, refresh already pending
	}
}

// SetPaused sets the pause state
func (o *Orchestrator) SetPaused(paused bool) {
	o.pauseMu.Lock()
	defer o.pauseMu.Unlock()
	o.paused = paused
}

// IsPaused returns the current pause state
func (o *Orchestrator) IsPaused() bool {
	o.pauseMu.RLock()
	defer o.pauseMu.RUnlock()
	return o.paused
}

// refreshController manages the centralized refresh logic
func (o *Orchestrator) refreshController(ctx context.Context) {
	// Trigger initial collection only if not paused
	if !o.IsPaused() {
		o.triggerAllSources()
	}
	
	// If interval is 0, only collect on manual refresh
	if o.interval == 0 {
		for {
			select {
			case <-ctx.Done():
				return
			case <-o.refreshCh:
				o.triggerAllSources()
			}
		}
	}
	
	// Normal periodic collection mode
	ticker := time.NewTicker(o.interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Only collect if not paused
			if !o.IsPaused() {
				o.triggerAllSources()
			}
			// Note: when paused, we simply ignore the ticker event
		case <-o.refreshCh:
			// Allow manual refresh even when paused (user explicitly requested it)
			o.triggerAllSources()
		}
	}
}

// triggerAllSources triggers collection for all sources
func (o *Orchestrator) triggerAllSources() {
	for _, source := range o.sources {
		if httpSource, ok := source.(*http.HTTPSource); ok {
			httpSource.TriggerRefresh()
		}
		// Add support for other source types as needed
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
