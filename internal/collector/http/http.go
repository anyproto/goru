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
	interval time.Duration
	client   *http.Client
	parser   *parser.Parser
	workers  int
}

// NewHTTPSource creates a new HTTP source
func New(targets []string, interval time.Duration, workers int) *HTTPSource {
	return &HTTPSource{
		targets:  targets,
		interval: interval,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		parser:  parser.New(),
		workers: workers,
	}
}

// Name returns the name of this source
func (h *HTTPSource) Name() string {
	return "http"
}

// Collect starts collecting snapshots from all targets
func (h *HTTPSource) Collect(ctx context.Context, snapshots chan<- *model.Snapshot) error {
	defer close(snapshots)

	// Create a ticker for periodic collection
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()

	// Collect immediately on start
	h.collectAll(ctx, snapshots)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
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
				if snapshot, err := h.collectOne(ctx, target); err == nil {
					select {
					case snapshots <- snapshot:
					case <-ctx.Done():
						return
					}
				}
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

var _ collector.Source = (*HTTPSource)(nil)
