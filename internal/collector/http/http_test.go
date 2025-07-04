package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/anyproto/goru/pkg/model"
)

func TestHTTPSourceCollectOne(t *testing.T) {
	// Sample goroutine dump
	dump := `goroutine 1 [running]:
main.main()
	/app/main.go:10 +0x20

goroutine 2 [chan receive]:
main.worker()
	/app/worker.go:25 +0x100
`

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/debug/pprof/goroutine" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, dump)
	}))
	defer server.Close()

	// Extract host:port from server URL
	target := server.URL[7:] // Remove "http://"

	source := New([]string{target}, time.Second, 1)
	ctx := context.Background()

	snapshot, err := source.collectOne(ctx, target)
	if err != nil {
		t.Fatalf("collectOne failed: %v", err)
	}

	if snapshot.Host != target {
		t.Errorf("Host = %q, want %q", snapshot.Host, target)
	}

	if total := snapshot.TotalGoroutines(); total != 2 {
		t.Errorf("TotalGoroutines = %d, want 2", total)
	}
}

func TestHTTPSourceCollect(t *testing.T) {
	// Sample goroutine dump
	dump := `goroutine 1 [running]:
main.main()
	/app/main.go:10 +0x20
`

	callCount := 0
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		fmt.Fprint(w, dump)
	}))
	defer server.Close()

	// Extract host:port from server URL
	target := server.URL[7:] // Remove "http://"

	source := New([]string{target}, 100*time.Millisecond, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	snapshots := make(chan *model.Snapshot, 10)
	err := source.Collect(ctx, snapshots)

	// Should have context deadline exceeded
	if err != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got %v", err)
	}

	// Should have at least 2 snapshots (immediate + one tick)
	if callCount < 2 {
		t.Errorf("Expected at least 2 calls, got %d", callCount)
	}

	// Check we got snapshots
	snapshotCount := len(snapshots)
	if snapshotCount < 2 {
		t.Errorf("Expected at least 2 snapshots, got %d", snapshotCount)
	}
}

func TestHTTPSourceMultipleTargets(t *testing.T) {
	// Create multiple test servers
	servers := make([]*httptest.Server, 3)
	targets := make([]string, 3)

	for i := range servers {
		id := i
		servers[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `goroutine %d [running]:
main.server%d()
	/app/server.go:%d +0x20
`, id+1, id, id*10)
		}))
		defer servers[i].Close()
		targets[i] = servers[i].URL[7:] // Remove "http://"
	}

	source := New(targets, time.Second, 2) // 2 workers
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	snapshots := make(chan *model.Snapshot, 10)
	go source.Collect(ctx, snapshots)

	// Collect snapshots
	time.Sleep(30 * time.Millisecond)
	cancel()

	// Should have one snapshot per target
	if len(snapshots) != 3 {
		t.Errorf("Expected 3 snapshots, got %d", len(snapshots))
	}

	// Verify each snapshot
	hostsSeen := make(map[string]bool)
	for i := 0; i < 3; i++ {
		select {
		case snapshot := <-snapshots:
			hostsSeen[snapshot.Host] = true
		default:
			t.Error("Missing snapshot")
		}
	}

	if len(hostsSeen) != 3 {
		t.Errorf("Expected 3 unique hosts, got %d", len(hostsSeen))
	}
}

func TestHTTPSourceErrorHandling(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		shouldError bool
	}{
		{
			name: "404 error",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.NotFound(w, r)
			}),
			shouldError: true,
		},
		{
			name: "500 error",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}),
			shouldError: true,
		},
		{
			name: "invalid content",
			handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, "not a valid goroutine dump")
			}),
			shouldError: false, // Parser handles invalid content gracefully
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			target := server.URL[7:] // Remove "http://"
			source := New([]string{target}, time.Second, 1)
			ctx := context.Background()

			_, err := source.collectOne(ctx, target)
			if tt.shouldError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}
