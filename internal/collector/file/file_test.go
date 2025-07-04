package file

import (
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anyproto/goru/pkg/model"
)

func TestFileSourceReadFile(t *testing.T) {
	// Create a temporary test file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	content := `goroutine 1 [running]:
main.main()
	/app/main.go:10 +0x20

goroutine 2 [chan receive]:
main.worker()
	/app/worker.go:25 +0x100
`

	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	source := New([]string{testFile}, false, time.Second)
	snapshot, err := source.readFile(testFile)
	if err != nil {
		t.Fatalf("readFile failed: %v", err)
	}

	expectedHost := "file:test.txt"
	if snapshot.Host != expectedHost {
		t.Errorf("Host = %q, want %q", snapshot.Host, expectedHost)
	}

	if total := snapshot.TotalGoroutines(); total != 2 {
		t.Errorf("TotalGoroutines = %d, want 2", total)
	}
}

func TestFileSourceReadGzipFile(t *testing.T) {
	// Create a temporary gzip file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.gz")

	content := `goroutine 1 [running]:
main.main()
	/app/main.go:10 +0x20
`

	// Write gzip file
	file, err := os.Create(testFile)
	if err != nil {
		t.Fatal(err)
	}

	gzWriter := gzip.NewWriter(file)
	if _, err := gzWriter.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}

	if err := gzWriter.Close(); err != nil {
		t.Fatal(err)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	source := New([]string{testFile}, false, time.Second)
	snapshot, err := source.readFile(testFile)
	if err != nil {
		t.Fatalf("readFile failed: %v", err)
	}

	expectedHost := "file:test.gz"
	if snapshot.Host != expectedHost {
		t.Errorf("Host = %q, want %q", snapshot.Host, expectedHost)
	}

	if total := snapshot.TotalGoroutines(); total != 1 {
		t.Errorf("TotalGoroutines = %d, want 1", total)
	}
}

func TestFileSourceGlobPattern(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple test files
	files := []string{"dump1.txt", "dump2.txt", "other.log"}
	for _, name := range files {
		content := `goroutine 1 [running]:
main.` + name + `()
	/app/main.go:10 +0x20
`
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	pattern := filepath.Join(tmpDir, "dump*.txt")
	source := New([]string{pattern}, false, time.Second)

	foundFiles, err := source.findFiles()
	if err != nil {
		t.Fatal(err)
	}

	if len(foundFiles) != 2 {
		t.Errorf("Expected 2 files, got %d", len(foundFiles))
	}
}

func TestFileSourceCollectOnce(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test files
	for i := 1; i <= 3; i++ {
		content := `goroutine 1 [running]:
main.main()
	/app/main.go:10 +0x20
`
		filename := filepath.Join(tmpDir, fmt.Sprintf("dump%d.txt", i))
		if err := os.WriteFile(filename, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	pattern := filepath.Join(tmpDir, "*.txt")
	source := New([]string{pattern}, false, time.Second)

	ctx := context.Background()
	snapshots := make(chan *model.Snapshot, 10)

	err := source.Collect(ctx, snapshots)
	if err != nil {
		t.Fatal(err)
	}

	// Should have 3 snapshots
	if len(snapshots) != 3 {
		t.Errorf("Expected 3 snapshots, got %d", len(snapshots))
	}
}

func TestFileSourceFollowMode(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.txt")

	// Create initial file
	content1 := `goroutine 1 [running]:
main.main()
	/app/main.go:10 +0x20
`
	if err := os.WriteFile(testFile, []byte(content1), 0644); err != nil {
		t.Fatal(err)
	}

	source := New([]string{testFile}, true, 50*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	snapshots := make(chan *model.Snapshot, 10)
	go source.Collect(ctx, snapshots)

	// Wait for initial scan
	time.Sleep(30 * time.Millisecond)

	// Modify the file
	content2 := content1 + `
goroutine 2 [chan receive]:
main.worker()
	/app/worker.go:25 +0x100
`
	if err := os.WriteFile(testFile, []byte(content2), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait for rescan
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Should have at least 2 snapshots (initial + after modification)
	snapshotCount := len(snapshots)
	if snapshotCount < 2 {
		t.Errorf("Expected at least 2 snapshots, got %d", snapshotCount)
	}

	// Check that snapshots are different
	var firstSnapshot, secondSnapshot *model.Snapshot
	select {
	case firstSnapshot = <-snapshots:
	default:
		t.Fatal("No first snapshot")
	}

	select {
	case secondSnapshot = <-snapshots:
	default:
		t.Fatal("No second snapshot")
	}

	if firstSnapshot.TotalGoroutines() >= secondSnapshot.TotalGoroutines() {
		t.Error("Second snapshot should have more goroutines")
	}
}

func TestFileSourceErrorHandling(t *testing.T) {
	source := New([]string{"/nonexistent/file.txt"}, false, time.Second)

	ctx := context.Background()
	snapshots := make(chan *model.Snapshot, 1)

	// Should not error, just skip missing files
	err := source.Collect(ctx, snapshots)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	// Should have no snapshots
	if len(snapshots) != 0 {
		t.Errorf("Expected 0 snapshots, got %d", len(snapshots))
	}
}
