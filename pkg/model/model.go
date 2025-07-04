package model

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

type StackFrame struct {
	Func string `json:"func"`
	File string `json:"file"`
	Line int    `json:"line,omitempty"`
}

type StackTrace []StackFrame

func (s StackTrace) String() string {
	var b strings.Builder
	for i, frame := range s {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(frame.Func)
		if frame.File != "" {
			b.WriteString(fmt.Sprintf(" %s:%d", frame.File, frame.Line))
		}
	}
	return b.String()
}

type GroupID string

type GoroutineState string

const (
	StateRunning   GoroutineState = "running"
	StateRunnable  GoroutineState = "runnable"
	StateBlocked   GoroutineState = "blocked"
	StateWaiting   GoroutineState = "waiting"
	StateSyscall   GoroutineState = "syscall"
	StateDead      GoroutineState = "dead"
	StateCopystack GoroutineState = "copystack"
	StatePreempted GoroutineState = "preempted"
)

type Group struct {
	ID            GroupID        `json:"id"`
	State         GoroutineState `json:"state"`
	Count         int            `json:"count"`
	WaitDurations []string       `json:"wait_durations,omitempty"`
	Trace         StackTrace     `json:"trace"`
	CreatedBy     *StackFrame    `json:"created_by,omitempty"`
}

func (g *Group) GenerateID() GroupID {
	h := sha256.New()
	h.Write([]byte(g.State))
	h.Write([]byte(g.Trace.String()))
	return GroupID(hex.EncodeToString(h.Sum(nil))[:16])
}

type Snapshot struct {
	Host    string             `json:"host"`
	TakenAt time.Time          `json:"taken_at"`
	Groups  map[GroupID]*Group `json:"groups"`
}

func NewSnapshot(host string) *Snapshot {
	return &Snapshot{
		Host:    host,
		TakenAt: time.Now(),
		Groups:  make(map[GroupID]*Group),
	}
}

func (s *Snapshot) AddGoroutine(state GoroutineState, trace StackTrace, waitDuration string, createdBy *StackFrame) {
	g := &Group{
		State:     state,
		Count:     1,
		Trace:     trace,
		CreatedBy: createdBy,
	}
	if waitDuration != "" {
		g.WaitDurations = []string{waitDuration}
	}

	g.ID = g.GenerateID()

	if existing, ok := s.Groups[g.ID]; ok {
		existing.Count++
		if waitDuration != "" {
			existing.WaitDurations = append(existing.WaitDurations, waitDuration)
		}
	} else {
		s.Groups[g.ID] = g
	}
}

func (s *Snapshot) TotalGoroutines() int {
	total := 0
	for _, g := range s.Groups {
		total += g.Count
	}
	return total
}

type ChangeType string

const (
	ChangeAdded   ChangeType = "added"
	ChangeRemoved ChangeType = "removed"
	ChangeUpdated ChangeType = "updated"
)

type Change struct {
	Type       ChangeType `json:"type"`
	Group      *Group     `json:"group"`
	CountDelta int        `json:"count_delta,omitempty"`
}

type ChangeSet struct {
	Host      string          `json:"host"`
	Timestamp time.Time       `json:"timestamp"`
	Added     []*Group        `json:"added,omitempty"`
	Removed   []*Group        `json:"removed,omitempty"`
	Updated   map[GroupID]int `json:"updated,omitempty"`
}

func NewChangeSet(host string) *ChangeSet {
	return &ChangeSet{
		Host:      host,
		Timestamp: time.Now(),
		Updated:   make(map[GroupID]int),
	}
}

func (c *ChangeSet) IsEmpty() bool {
	return len(c.Added) == 0 && len(c.Removed) == 0 && len(c.Updated) == 0
}
