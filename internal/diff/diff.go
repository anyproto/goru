package diff

import (
	"github.com/anyproto/goru/pkg/model"
)

// Diff computes the changes between two snapshots
type Diff struct{}

// New creates a new diff engine
func New() *Diff {
	return &Diff{}
}

// Compare computes the differences between old and new snapshots
func (d *Diff) Compare(old, new *model.Snapshot) *model.ChangeSet {
	changes := model.NewChangeSet(new.Host)

	if old == nil {
		// All groups are new
		for _, group := range new.Groups {
			changes.Added = append(changes.Added, group)
		}
		return changes
	}

	// Check for removed groups
	for id, oldGroup := range old.Groups {
		if _, exists := new.Groups[id]; !exists {
			changes.Removed = append(changes.Removed, oldGroup)
		}
	}

	// Check for added groups and count changes
	for id, newGroup := range new.Groups {
		oldGroup, exists := old.Groups[id]
		if !exists {
			// New group
			changes.Added = append(changes.Added, newGroup)
		} else if newGroup.Count != oldGroup.Count {
			// Count changed
			changes.Updated[id] = newGroup.Count - oldGroup.Count
		}
	}

	return changes
}

// DiffStats provides statistics about the differences
type DiffStats struct {
	TotalAdded        int
	TotalRemoved      int
	GroupsAdded       int
	GroupsRemoved     int
	GroupsWithChanges int
}

// Stats computes statistics for a changeset
func (d *Diff) Stats(changes *model.ChangeSet) DiffStats {
	stats := DiffStats{
		GroupsAdded:       len(changes.Added),
		GroupsRemoved:     len(changes.Removed),
		GroupsWithChanges: len(changes.Updated),
	}

	// Count total goroutines added
	for _, group := range changes.Added {
		stats.TotalAdded += group.Count
	}

	// Count total goroutines removed
	for _, group := range changes.Removed {
		stats.TotalRemoved += group.Count
	}

	// Add count changes
	for _, delta := range changes.Updated {
		if delta > 0 {
			stats.TotalAdded += delta
		} else {
			stats.TotalRemoved += -delta
		}
	}

	return stats
}
