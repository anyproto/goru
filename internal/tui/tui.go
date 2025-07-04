package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/anyproto/goru/internal/store"
	"github.com/anyproto/goru/pkg/model"
)

// Refresher interface for manual refresh capability
type Refresher interface {
	TriggerRefresh()
	SetPaused(bool)
	IsPaused() bool
}

// Model represents the TUI model
type Model struct {
	store        *store.Store
	refresher    Refresher
	interval     time.Duration
	table        table.Model
	filterInput  textinput.Model
	updates      <-chan store.Update
	selectedHost string
	filter       string
	filterMode   bool
	showDetails  bool
	width        int
	height       int
	lastUpdate   time.Time
	stats        store.Stats

	// For details view
	selectedRow   int
	selectedGroup *model.Group // Store the selected group when entering details

	// Keep track of displayed groups for details lookup
	displayedGroups []*model.Group

	// Sorting
	sortBy string // "count", "state", "function", "wait"
}

// New creates a new TUI model
func New(s *store.Store, refresher Refresher, interval time.Duration) Model {
	// Subscribe to store updates
	updates := make(chan store.Update, 10)
	s.Subscribe(updates)

	// Create table
	columns := []table.Column{
		{Title: "State", Width: 10},
		{Title: "Function", Width: 55},
		{Title: "Created By", Width: 75},
		{Title: "Count ↓", Width: 7}, // Default sort by count
		{Title: "Wait", Width: 10},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(20),
	)

	// Style the table
	s1 := table.DefaultStyles()
	s1.Header = s1.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		Bold(false)
	s1.Selected = s1.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s1)

	// Create filter input
	ti := textinput.New()
	ti.Placeholder = "Filter by function name..."
	ti.CharLimit = 50
	ti.Width = 50

	m := Model{
		store:       s,
		refresher:   refresher,
		interval:    interval,
		table:       t,
		filterInput: ti,
		updates:     updates,
		stats:       s.GetStats(),
		sortBy:      "count", // default sort by count
	}

	// Select first host if available
	hosts := m.getSortedHosts()
	if len(hosts) > 0 {
		m.selectedHost = hosts[0]
	}

	return m
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.waitForUpdate(),
		m.refreshData(),
	)
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetHeight(m.height - 10) // Leave room for header and footer
		m.table.SetWidth(m.width)

	case tea.KeyMsg:
		// Handle details view first
		if m.showDetails {
			switch msg.Type {
			case tea.KeyEnter, tea.KeyEsc:
				m.showDetails = false
				m.selectedGroup = nil // Clear the stored group
			case tea.KeyCtrlC:
				return m, tea.Quit
			}
			return m, nil
		}

		// Handle filter mode input
		if m.filterMode {
			switch msg.Type {
			case tea.KeyEnter:
				m.filter = m.filterInput.Value()
				m.filterMode = false
				m.filterInput.Blur()
				cmds = append(cmds, m.refreshData())
			case tea.KeyEsc:
				m.filterMode = false
				m.filterInput.Blur()
				m.filterInput.SetValue(m.filter) // Restore previous filter
			default:
				var cmd tea.Cmd
				m.filterInput, cmd = m.filterInput.Update(msg)
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		// Normal mode key handling
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		// Handle Alt+Up/Down for jumping by 10
		case msg.Type == tea.KeyUp && msg.Alt:
			// Jump up by 10
			currentCursor := m.table.Cursor()
			newCursor := currentCursor - 10
			if newCursor < 0 {
				newCursor = 0
			}
			m.table.SetCursor(newCursor)

		case msg.Type == tea.KeyDown && msg.Alt:
			// Jump down by 10
			currentCursor := m.table.Cursor()
			maxCursor := len(m.displayedGroups) - 1
			newCursor := currentCursor + 10
			if newCursor > maxCursor {
				newCursor = maxCursor
			}
			if newCursor >= 0 {
				m.table.SetCursor(newCursor)
			}

		case key.Matches(msg, keys.Enter):
			// Enter details view
			m.selectedRow = m.table.Cursor()
			if m.selectedRow >= 0 && m.selectedRow < len(m.displayedGroups) {
				// Store a copy of the selected group
				selectedGroup := m.displayedGroups[m.selectedRow]
				groupCopy := *selectedGroup
				m.selectedGroup = &groupCopy
				m.showDetails = true
			}

		case key.Matches(msg, keys.Filter):
			m.filterMode = true
			m.filterInput.Focus()
			m.filterInput.SetValue(m.filter)
			cmds = append(cmds, textinput.Blink)

		case key.Matches(msg, keys.Clear):
			m.filter = ""
			m.filterInput.SetValue("")
			cmds = append(cmds, m.refreshData())

		case key.Matches(msg, keys.Pause):
			if m.refresher != nil {
				paused := !m.refresher.IsPaused()
				m.refresher.SetPaused(paused)
				if !paused {
					// Resume updates
					cmds = append(cmds, m.waitForUpdate())
				}
			}

		case key.Matches(msg, keys.NextHost):
			m.selectNextHost()
			cmds = append(cmds, m.refreshData())

		case key.Matches(msg, keys.PrevHost):
			m.selectPrevHost()
			cmds = append(cmds, m.refreshData())

		case key.Matches(msg, keys.Sort):
			// Cycle through sort modes: count -> state -> function -> wait -> count
			switch m.sortBy {
			case "count":
				m.sortBy = "state"
			case "state":
				m.sortBy = "function"
			case "function":
				m.sortBy = "wait"
			case "wait":
				m.sortBy = "count"
			default:
				m.sortBy = "count"
			}
			// Update table columns with sort indicator
			m.updateTableColumns()
			// No need to call refreshData - updateTableColumns already rebuilds the table

		case key.Matches(msg, keys.Refresh):
			// Trigger manual refresh
			if m.refresher != nil {
				m.refresher.TriggerRefresh()
			}
		}

	case store.Update:
		if !m.showDetails {
			m.lastUpdate = time.Now()
			m.stats = m.store.GetStats()
			cmds = append(cmds, m.refreshData())
		}
		// Always continue waiting for updates
		cmds = append(cmds, m.waitForUpdate())

	case refreshMsg:
		rows := m.buildTableRows()
		m.table.SetRows(rows)
	}

	// Update table only if not in filter mode or details view
	if !m.filterMode && !m.showDetails {
		m.table, cmd = m.table.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// View renders the TUI
func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	// Show details screen if enabled
	if m.showDetails {
		return m.renderDetailsView()
	}

	// Otherwise show main table view
	return m.renderTableView()
}

func (m Model) renderTableView() string {
	var b strings.Builder

	// Header
	header := m.renderHeader()
	b.WriteString(header)
	b.WriteString("\n\n")

	// Filter input if in filter mode
	if m.filterMode {
		filterStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205"))
		b.WriteString(filterStyle.Render("Filter: "))
		b.WriteString(m.filterInput.View())
		b.WriteString("\n\n")
	} else if m.filter != "" {
		filterStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241"))
		b.WriteString(filterStyle.Render(fmt.Sprintf("Filter: %s", m.filter)))
		b.WriteString("\n\n")
	}

	// Always show table
	b.WriteString(m.table.View())
	b.WriteString("\n")

	// Footer
	footer := m.renderFooter()
	b.WriteString(footer)

	return b.String()
}

func (m Model) renderDetailsView() string {
	if m.selectedGroup == nil {
		return "No details available"
	}

	g := m.selectedGroup
	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("229")).
		MarginBottom(1)
	b.WriteString(titleStyle.Render("Goroutine Group Details"))
	b.WriteString("\n\n")

	// Group info
	infoStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Width(15)
	fileStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("243"))

	b.WriteString(labelStyle.Render("Host:") + infoStyle.Render(m.selectedHost) + "\n")
	b.WriteString(labelStyle.Render("State:") + infoStyle.Render(string(g.State)) + "\n")
	b.WriteString(labelStyle.Render("Count:") + infoStyle.Render(fmt.Sprintf("%d", g.Count)) + "\n")
	b.WriteString(labelStyle.Render("Group ID:") + infoStyle.Render(string(g.ID)) + "\n")

	b.WriteString("\n")

	// Stack trace
	stackTitle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("229"))
	b.WriteString(stackTitle.Render("Stack Trace:"))
	b.WriteString("\n")

	frameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("252"))

	for i, frame := range g.Trace {
		b.WriteString(fmt.Sprintf("\n%2d. ", i+1))
		b.WriteString(frameStyle.Render(frame.Func))
		if frame.File != "" {
			b.WriteString("\n    ")
			b.WriteString(fileStyle.Render(fmt.Sprintf("%s:%d", frame.File, frame.Line)))
		}
	}

	// Show created by after stack trace if present
	if g.CreatedBy != nil {
		b.WriteString("\n\n")
		b.WriteString(stackTitle.Render("Created By:"))
		b.WriteString("\n")
		b.WriteString(frameStyle.Render(g.CreatedBy.Func))
		if g.CreatedBy.File != "" {
			b.WriteString("\n")
			b.WriteString(fileStyle.Render(fmt.Sprintf("%s:%d", g.CreatedBy.File, g.CreatedBy.Line)))
		}
	}

	// Wait durations
	if len(g.WaitDurations) > 0 {
		b.WriteString("\n\n")
		b.WriteString(stackTitle.Render(fmt.Sprintf("Wait Durations (%d total):", len(g.WaitDurations))))
		b.WriteString("\n")

		// Group wait durations by value
		waitGroups := make(map[string]int)
		for _, dur := range g.WaitDurations {
			waitGroups[dur]++
		}

		// Sort by count (descending) then by duration
		type waitGroup struct {
			duration string
			count    int
		}
		var sortedGroups []waitGroup
		for dur, count := range waitGroups {
			sortedGroups = append(sortedGroups, waitGroup{dur, count})
		}
		sort.Slice(sortedGroups, func(i, j int) bool {
			if sortedGroups[i].count != sortedGroups[j].count {
				return sortedGroups[i].count > sortedGroups[j].count
			}
			return sortedGroups[i].duration < sortedGroups[j].duration
		})

		// Display grouped durations
		for _, wg := range sortedGroups {
			if wg.count > 1 {
				b.WriteString(fmt.Sprintf("  • %s (%d)\n", wg.duration, wg.count))
			} else {
				b.WriteString(fmt.Sprintf("  • %s\n", wg.duration))
			}
		}
	}

	// Footer
	b.WriteString("\n")
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
	b.WriteString(helpStyle.Render("Press Enter or Esc to return"))

	return b.String()
}

func (m Model) renderHeader() string {
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("229")).
		Render("Goroutine Explorer")

	statusIndicator := ""
	paused := m.refresher != nil && m.refresher.IsPaused()
	if paused {
		pauseStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("196")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)
		statusIndicator = " " + pauseStyle.Render("PAUSED")
	} else if m.interval == 0 {
		manualStyle := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("226")).
			Background(lipgloss.Color("235")).
			Padding(0, 1)
		statusIndicator = " " + manualStyle.Render("MANUAL")
	}

	displayedGroups := len(m.displayedGroups)
	totalHosts := len(m.getSortedHosts())
	hostIndex := 0
	for i, h := range m.getSortedHosts() {
		if h == m.selectedHost {
			hostIndex = i + 1
			break
		}
	}
	stats := fmt.Sprintf("Host %d/%d: %s | Groups: %d/%d | Goroutines: %d | Updated: %s%s",
		hostIndex,
		totalHosts,
		m.selectedHost,
		displayedGroups,
		m.stats.TotalGroups,
		m.stats.TotalGoroutines,
		m.lastUpdate.Format("15:04:05"),
		statusIndicator,
	)

	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	// Check for errors and fetching status
	errors := m.store.GetErrors()
	fetching := m.store.GetFetchingHosts()
	
	var statusDisplay string
	
	// Check if current host is fetching
	if _, isFetching := fetching[m.selectedHost]; isFetching {
		fetchingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("226")).
			Bold(true)
		statusDisplay = fetchingStyle.Render("⟳ Fetching...")
	} else if err, hasError := errors[m.selectedHost]; hasError {
		// Show error for current host
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true)
		statusDisplay = errorStyle.Render(fmt.Sprintf("⚠ Error: %v", err))
	} else if len(errors) > 0 || len(fetching) > 0 {
		// Show summary of other hosts with issues
		var parts []string
		if len(errors) > 0 {
			errorStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("196"))
			parts = append(parts, errorStyle.Render(fmt.Sprintf("%d error(s)", len(errors))))
		}
		if len(fetching) > 0 {
			fetchingStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("226"))
			parts = append(parts, fetchingStyle.Render(fmt.Sprintf("%d fetching", len(fetching))))
		}
		if len(parts) > 0 {
			statusDisplay = strings.Join(parts, " | ")
		}
	}
	
	if statusDisplay != "" {
		return lipgloss.JoinVertical(lipgloss.Left, title, statsStyle.Render(stats), statusDisplay)
	}

	return lipgloss.JoinVertical(lipgloss.Left, title, statsStyle.Render(stats))
}

func (m Model) renderFooter() string {
	help := []string{
		"↑/↓: Navigate",
		"Alt+↑/↓: ±10",
		"←/→: Host",
		"Enter: Details",
		"f: Filter",
		"c: Clear",
		"s: Sort",
		"r: Refresh",
		"p: Pause",
		"q: Quit",
	}

	if m.filterMode {
		help = []string{
			"Enter: Apply",
			"Esc: Cancel",
		}
	}

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	return helpStyle.Render(strings.Join(help, " • "))
}

func (m *Model) buildTableRows() []table.Row {
	var rows []table.Row

	// Clear displayed groups - MUST do this every time we rebuild
	m.displayedGroups = nil

	// Get current snapshot
	var snapshot *model.Snapshot
	if m.selectedHost != "" {
		snapshot = m.store.GetSnapshot(m.selectedHost)
	} else {
		// Select first available host
		hosts := m.getSortedHosts()
		if len(hosts) > 0 {
			m.selectedHost = hosts[0]
			snapshot = m.store.GetSnapshot(m.selectedHost)
		}
	}

	// If no snapshot yet (host might be fetching or have error), return empty
	if snapshot == nil {
		return rows
	}

	// Collect groups
	var groups []*model.Group
	for _, g := range snapshot.Groups {
		groups = append(groups, g)
	}

	// Sort based on current sort mode
	switch m.sortBy {
	case "state":
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].State != groups[j].State {
				return groups[i].State < groups[j].State
			}
			// Secondary sort by count
			if groups[i].Count != groups[j].Count {
				return groups[i].Count > groups[j].Count
			}
			// Tertiary sort by group ID for deterministic ordering
			return groups[i].ID < groups[j].ID
		})
	case "function":
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].Trace[0].Func != groups[j].Trace[0].Func {
				return groups[i].Trace[0].Func < groups[j].Trace[0].Func
			}
			// Secondary sort by count
			if groups[i].Count != groups[j].Count {
				return groups[i].Count > groups[j].Count
			}
			// Tertiary sort by group ID for deterministic ordering
			return groups[i].ID < groups[j].ID
		})
	case "wait":
		sort.Slice(groups, func(i, j int) bool {
			// Get max wait time for each group
			maxI := getMaxWaitMinutes(groups[i].WaitDurations)
			maxJ := getMaxWaitMinutes(groups[j].WaitDurations)
			if maxI != maxJ {
				return maxI > maxJ // Longer waits first
			}
			// Secondary sort by count
			if groups[i].Count != groups[j].Count {
				return groups[i].Count > groups[j].Count
			}
			// Tertiary sort by group ID for deterministic ordering
			return groups[i].ID < groups[j].ID
		})
	default: // "count"
		sort.Slice(groups, func(i, j int) bool {
			if groups[i].Count != groups[j].Count {
				return groups[i].Count > groups[j].Count
			}
			// Secondary sort by group ID for deterministic ordering
			return groups[i].ID < groups[j].ID
		})
	}

	// Build rows
	for _, g := range groups {

		// Apply filter - search entire stack trace
		if m.filter != "" {
			found := false
			searchTerm := strings.ToLower(m.filter)
			for _, frame := range g.Trace {
				if strings.Contains(strings.ToLower(frame.Func), searchTerm) ||
					strings.Contains(strings.ToLower(frame.File), searchTerm) {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		// Store the group for details view
		m.displayedGroups = append(m.displayedGroups, g)

		// Format wait duration with abbreviated units
		wait := ""
		if len(g.WaitDurations) > 0 {
			wait = formatWaitRange(g.WaitDurations)
		}

		// Format created by
		createdBy := ""
		if g.CreatedBy != nil {
			createdBy = g.CreatedBy.Func
			// Truncate if too long
			if len(createdBy) > 75 {
				createdBy = createdBy[:72] + "..."
			}
		}

		// Main row
		mainRow := table.Row{
			string(g.State),
			g.Trace[0].Func,
			createdBy,
			fmt.Sprintf("%d", g.Count),
			wait,
		}
		rows = append(rows, mainRow)
	}

	return rows
}

func (m *Model) selectNextHost() {
	hosts := m.getSortedHosts()
	if len(hosts) == 0 {
		return
	}

	for i, h := range hosts {
		if h == m.selectedHost {
			m.selectedHost = hosts[(i+1)%len(hosts)]
			return
		}
	}
}

func (m *Model) selectPrevHost() {
	hosts := m.getSortedHosts()
	if len(hosts) == 0 {
		return
	}

	for i, h := range hosts {
		if h == m.selectedHost {
			idx := i - 1
			if idx < 0 {
				idx = len(hosts) - 1
			}
			m.selectedHost = hosts[idx]
			return
		}
	}
}

func (m Model) getSortedHosts() []string {
	// Get all registered hosts from the store
	hosts := m.store.GetAllHosts()
	sort.Strings(hosts)
	return hosts
}

func (m *Model) updateTableColumns() {
	// Create columns with sort indicator
	columns := []table.Column{
		{Title: "State", Width: 10},
		{Title: "Function", Width: 55},
		{Title: "Created By", Width: 75},
		{Title: "Count", Width: 7},
		{Title: "Wait", Width: 10},
	}

	// Add arrow to the sorted column
	switch m.sortBy {
	case "state":
		columns[0].Title = "State ↓"
	case "function":
		columns[1].Title = "Function ↓"
	case "count":
		columns[3].Title = "Count ↓"
	case "wait":
		columns[4].Title = "Wait ↓"
	}

	// Get current cursor position
	cursor := m.table.Cursor()

	// Create new table with updated columns
	// Use the same height as set during window resize
	tableHeight := m.height - 10 // Same as in WindowSizeMsg handler
	if tableHeight < 5 {
		tableHeight = 5
	}
	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(tableHeight),
	)

	// Apply the same styles
	s1 := table.DefaultStyles()
	s1.Header = s1.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		Bold(false)
	s1.Selected = s1.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s1)

	// Set the current rows and cursor
	rows := m.buildTableRows()
	t.SetRows(rows)
	if cursor >= 0 && cursor < len(rows) {
		t.SetCursor(cursor)
	}

	m.table = t
}

func abbreviateWaitTime(waitTime string) string {
	// Replace "minutes" with "min" or "mins"
	waitTime = strings.ReplaceAll(waitTime, " minutes", " mins")
	waitTime = strings.ReplaceAll(waitTime, " minute", " min")
	return waitTime
}

func formatWaitRange(durations []string) string {
	if len(durations) == 0 {
		return ""
	}

	// Get unique values
	uniqueMap := make(map[string]int)
	for _, d := range durations {
		uniqueMap[d]++
	}

	// If only one unique value, just return it without count
	if len(uniqueMap) == 1 {
		dur := durations[0]
		return abbreviateWaitTime(dur)
	}

	// Multiple unique values - find min and max
	// Parse durations to find range
	minMinutes := int64(999999)
	maxMinutes := int64(0)

	for dur := range uniqueMap {
		minutes := parseMinutes(dur)
		if minutes < minMinutes {
			minMinutes = minutes
		}
		if minutes > maxMinutes {
			maxMinutes = minutes
		}
	}

	// Format range as "X-Ymin"
	if minMinutes == maxMinutes {
		return formatMinutes(minMinutes)
	}
	return fmt.Sprintf("%d-%dmin", minMinutes, maxMinutes)
}

func parseMinutes(duration string) int64 {
	// Simple parser for "X minutes" format
	parts := strings.Fields(duration)
	if len(parts) >= 2 {
		if val, err := strconv.ParseInt(parts[0], 10, 64); err == nil {
			return val
		}
	}
	return 0
}

func formatMinutes(minutes int64) string {
	if minutes == 1 {
		return "1 min"
	}
	return fmt.Sprintf("%d mins", minutes)
}

// getMaxWaitMinutes returns the maximum wait time in minutes from a list of wait durations
func getMaxWaitMinutes(durations []string) int64 {
	if len(durations) == 0 {
		return 0
	}
	
	maxMinutes := int64(0)
	for _, dur := range durations {
		minutes := parseMinutes(dur)
		if minutes > maxMinutes {
			maxMinutes = minutes
		}
	}
	return maxMinutes
}

// Messages
type refreshMsg struct{}

// Commands
func (m Model) waitForUpdate() tea.Cmd {
	return func() tea.Msg {
		return <-m.updates
	}
}

func (m Model) refreshData() tea.Cmd {
	return func() tea.Msg {
		return refreshMsg{}
	}
}

// Key bindings
type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	NextHost key.Binding
	PrevHost key.Binding
	Enter    key.Binding
	Filter   key.Binding
	Clear    key.Binding
	Pause    key.Binding
	Sort     key.Binding
	Refresh  key.Binding
	Quit     key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	NextHost: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("→/l", "next host"),
	),
	PrevHost: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("←/h", "prev host"),
	),
	Enter: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "toggle details"),
	),
	Filter: key.NewBinding(
		key.WithKeys("f", "/"),
		key.WithHelp("f", "filter"),
	),
	Clear: key.NewBinding(
		key.WithKeys("c"),
		key.WithHelp("c", "clear filter"),
	),
	Pause: key.NewBinding(
		key.WithKeys("p", " "),
		key.WithHelp("p/space", "pause updates"),
	),
	Sort: key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "sort"),
	),
	Refresh: key.NewBinding(
		key.WithKeys("r"),
		key.WithHelp("r", "refresh"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
}
