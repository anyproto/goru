package http

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/anyproto/goru/internal/collector"
	"github.com/anyproto/goru/internal/parser"
	"github.com/anyproto/goru/pkg/model"
)

// HTTPSource collects goroutine dumps from HTTP endpoints
type HTTPSource struct {
	targets  []string
	client   *http.Client
	parser   *parser.Parser
	workers  int
	
	// Manual refresh support
	refreshCh chan struct{}
	
	// Track errors per host
	errorsMu sync.RWMutex
	errors   map[string]error
}

// NewHTTPSource creates a new HTTP source
func New(targets []string, timeout time.Duration, workers int) *HTTPSource {
	return &HTTPSource{
		targets:   targets,
		refreshCh: make(chan struct{}, 1), // Buffered to avoid blocking
		client: &http.Client{
			Timeout: timeout,
		},
		parser:  parser.New(),
		workers: workers,
		errors:  make(map[string]error),
	}
}

// Name returns the name of this source
func (h *HTTPSource) Name() string {
	return "http"
}

// Collect starts collecting snapshots from all targets
func (h *HTTPSource) Collect(ctx context.Context, snapshots chan<- *model.Snapshot) error {
	defer close(snapshots)

	// Wait for refresh triggers from orchestrator
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-h.refreshCh:
			h.collectAll(ctx, snapshots)
		}
	}
}

func (h *HTTPSource) collectAll(ctx context.Context, snapshots chan<- *model.Snapshot) {
	var wg sync.WaitGroup
	workCh := make(chan string, len(h.targets))

	// Start workers
	for i := 0; i < h.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for target := range workCh {
				snapshot, err := h.collectOne(ctx, target)
				
				// Update error status
				h.errorsMu.Lock()
				if err != nil {
					h.errors[target] = err
				} else {
					delete(h.errors, target)
				}
				h.errorsMu.Unlock()
				
				if err == nil {
					select {
					case snapshots <- snapshot:
					case <-ctx.Done():
						return
					}
				}
				// Note: errors are tracked and we continue processing other targets
			}
		}()
	}

	// Queue work
	for _, target := range h.targets {
		select {
		case workCh <- target:
		case <-ctx.Done():
			close(workCh)
			wg.Wait()
			return
		}
	}

	close(workCh)
	wg.Wait()
}

func (h *HTTPSource) collectOne(ctx context.Context, target string) (*model.Snapshot, error) {
	url := fmt.Sprintf("http://%s/debug/pprof/goroutine?debug=2", target)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from %s", resp.StatusCode, url)
	}

	// Read the response body
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	// Parse the goroutine dump
	snapshot, err := h.parser.ParseBytes(data, target)
	if err != nil {
		return nil, fmt.Errorf("parsing dump from %s: %w", target, err)
	}

	return snapshot, nil
}

// GetErrors returns the current errors for each host
func (h *HTTPSource) GetErrors() map[string]error {
	h.errorsMu.RLock()
	defer h.errorsMu.RUnlock()
	
	// Return a copy
	result := make(map[string]error)
	for k, v := range h.errors {
		result[k] = v
	}
	return result
}

// GetTargets returns all configured targets for this source
func (h *HTTPSource) GetTargets() []string {
	return h.targets
}

// TriggerRefresh manually triggers a refresh of all targets
func (h *HTTPSource) TriggerRefresh() {
	select {
	case h.refreshCh <- struct{}{}:
		// Refresh triggered
	default:
		// Channel is full, refresh already pending
	}
}



var _ collector.Source = (*HTTPSource)(nil)
