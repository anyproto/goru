package collector

import (
	"context"

	"github.com/anyproto/goru/pkg/model"
)

// Source represents a source of goroutine snapshots
type Source interface {
	// Name returns the name of this source (for logging)
	Name() string

	// Collect starts collecting snapshots and sends them to the channel
	// The implementation should close the channel when done
	Collect(ctx context.Context, snapshots chan<- *model.Snapshot) error
}

// Config holds common configuration for collectors
type Config struct {
	Workers int
}
