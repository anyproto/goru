package file

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anyproto/goru/internal/collector"
	"github.com/anyproto/goru/internal/parser"
	"github.com/anyproto/goru/pkg/model"
)

// FileSource collects goroutine dumps from files
type FileSource struct {
	patterns []string
	follow   bool
	interval time.Duration
	parser   *parser.Parser

	// Track file state for follow mode
	mu         sync.Mutex
	fileStates map[string]*fileState
}

type fileState struct {
	size    int64
	modTime time.Time
	offset  int64
}

// New creates a new file source
func New(patterns []string, follow bool, interval time.Duration) *FileSource {
	return &FileSource{
		patterns:   patterns,
		follow:     follow,
		interval:   interval,
		parser:     parser.New(),
		fileStates: make(map[string]*fileState),
	}
}

// Name returns the name of this source
func (f *FileSource) Name() string {
	return "file"
}

// Collect starts collecting snapshots from files
func (f *FileSource) Collect(ctx context.Context, snapshots chan<- *model.Snapshot) error {
	defer close(snapshots)

	if f.follow {
		return f.collectWithFollow(ctx, snapshots)
	}

	// One-shot collection
	return f.collectOnce(ctx, snapshots)
}

func (f *FileSource) collectOnce(ctx context.Context, snapshots chan<- *model.Snapshot) error {
	files, err := f.findFiles()
	if err != nil {
		return fmt.Errorf("finding files: %w", err)
	}

	for _, file := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if snapshot, err := f.readFile(file); err == nil {
				select {
				case snapshots <- snapshot:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}

	return nil
}

func (f *FileSource) collectWithFollow(ctx context.Context, snapshots chan<- *model.Snapshot) error {
	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()

	// Initial scan
	if err := f.scanAndCollect(ctx, snapshots); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := f.scanAndCollect(ctx, snapshots); err != nil {
				return err
			}
		}
	}
}

func (f *FileSource) scanAndCollect(ctx context.Context, snapshots chan<- *model.Snapshot) error {
	files, err := f.findFiles()
	if err != nil {
		return fmt.Errorf("finding files: %w", err)
	}

	for _, file := range files {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if snapshot, err := f.checkAndReadFile(file); err == nil && snapshot != nil {
				select {
				case snapshots <- snapshot:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}

	return nil
}

func (f *FileSource) findFiles() ([]string, error) {
	var files []string
	seen := make(map[string]bool)

	for _, pattern := range f.patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("glob %q: %w", pattern, err)
		}

		for _, match := range matches {
			abs, err := filepath.Abs(match)
			if err != nil {
				continue
			}
			if !seen[abs] {
				seen[abs] = true
				files = append(files, abs)
			}
		}
	}

	return files, nil
}

func (f *FileSource) checkAndReadFile(path string) (*model.Snapshot, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	f.mu.Lock()
	state, exists := f.fileStates[path]
	if !exists {
		state = &fileState{}
		f.fileStates[path] = state
	}

	// Check if file has changed
	if exists && state.size == info.Size() && state.modTime.Equal(info.ModTime()) {
		f.mu.Unlock()
		return nil, nil // No changes
	}

	// Update state
	state.size = info.Size()
	state.modTime = info.ModTime()
	f.mu.Unlock()

	return f.readFile(path)
}

func (f *FileSource) readFile(path string) (*model.Snapshot, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening file: %w", err)
	}
	defer file.Close()

	var reader io.Reader = file

	// Handle gzip files
	if strings.HasSuffix(path, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("creating gzip reader: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	}

	// Generate host name from file path
	host := fmt.Sprintf("file:%s", filepath.Base(path))

	snapshot, err := f.parser.Parse(reader, host)
	if err != nil {
		return nil, fmt.Errorf("parsing file %s: %w", path, err)
	}

	return snapshot, nil
}

var _ collector.Source = (*FileSource)(nil)
