package engine

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"golang.org/x/term"

	"github.com/qdo/ecsmate/internal/log"
)

type TaskStatus string

const (
	TaskStatusPending TaskStatus = "PENDING"
	TaskStatusRunning TaskStatus = "RUNNING"
	TaskStatusSuccess TaskStatus = "SUCCESS"
	TaskStatusFailed  TaskStatus = "FAILED"
	TaskStatusSkipped TaskStatus = "SKIPPED"
)

type TrackedTask struct {
	Name      string
	Type      string
	Status    TaskStatus
	Message   string
	StartTime time.Time
	EndTime   time.Time
	Progress  int
	Total     int
}

// ServiceProgressState tracks state for interactive re-rendering
type ServiceProgressState struct {
	ServiceName    string
	DesiredCount   int32
	RunningCount   int32
	PendingCount   int32
	RolloutState   string
	TaskDefinition string
	RolloutReason  string
	Events         []EventInfo
	Tasks          []TaskDisplayInfo
	LastLines      int // number of lines rendered last time (for clearing)

	// Deployment tracking
	NewDeployment *DeploymentProgressInfo
	OldDeployment *DeploymentProgressInfo
	IsRollingBack bool
}

// EventInfo represents a service event
type EventInfo struct {
	Timestamp time.Time
	Message   string
}

// TaskDisplayInfo represents an ECS task for display
type TaskDisplayInfo struct {
	TaskID         string
	LastStatus     string
	DesiredStatus  string
	StartedAt      *time.Time
	TaskDefinition string
}

type Tracker struct {
	mu      sync.Mutex
	out     io.Writer
	tasks   map[string]*TrackedTask
	order   []string
	noColor bool
	started time.Time

	successColor *color.Color
	errorColor   *color.Color
	warnColor    *color.Color
	infoColor    *color.Color
	dimColor     *color.Color

	// Interactive mode support
	interactive     bool
	serviceProgress map[string]*ServiceProgressState
	lastStateHash   map[string]string // for non-interactive change detection
}

func NewTracker(out io.Writer, noColor bool) *Tracker {
	if noColor {
		color.NoColor = true
	}

	// Detect if terminal is interactive
	interactive := false
	if f, ok := out.(*os.File); ok {
		interactive = term.IsTerminal(int(f.Fd()))
	}

	return &Tracker{
		out:             out,
		tasks:           make(map[string]*TrackedTask),
		order:           make([]string, 0),
		noColor:         noColor,
		started:         time.Now(),
		successColor:    color.New(color.FgGreen),
		errorColor:      color.New(color.FgRed),
		warnColor:       color.New(color.FgYellow),
		infoColor:       color.New(color.FgCyan),
		dimColor:        color.New(color.FgWhite),
		interactive:     interactive,
		serviceProgress: make(map[string]*ServiceProgressState),
		lastStateHash:   make(map[string]string),
	}
}

// IsInteractive returns whether the tracker is in interactive mode
func (t *Tracker) IsInteractive() bool {
	return t.interactive
}

func (t *Tracker) AddTask(name, taskType string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.tasks[name] = &TrackedTask{
		Name:   name,
		Type:   taskType,
		Status: TaskStatusPending,
	}
	t.order = append(t.order, name)
}

func (t *Tracker) StartTask(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if task, ok := t.tasks[name]; ok {
		task.Status = TaskStatusRunning
		task.StartTime = time.Now()
		t.printTaskStart(task)
	}
}

func (t *Tracker) UpdateProgress(name string, progress, total int, message string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if task, ok := t.tasks[name]; ok {
		task.Progress = progress
		task.Total = total
		task.Message = message
		t.printTaskProgress(task)
	}
}

func (t *Tracker) CompleteTask(name string, message string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Clear interactive service progress if present
	t.clearServiceProgress(name)

	if task, ok := t.tasks[name]; ok {
		task.Status = TaskStatusSuccess
		task.EndTime = time.Now()
		task.Message = message
		t.printTaskComplete(task)
	}
}

func (t *Tracker) FailTask(name string, message string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Clear interactive service progress if present
	t.clearServiceProgress(name)

	if task, ok := t.tasks[name]; ok {
		task.Status = TaskStatusFailed
		task.EndTime = time.Now()
		task.Message = message
		t.printTaskFailed(task)
	}
}

func (t *Tracker) SkipTask(name string, message string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if task, ok := t.tasks[name]; ok {
		task.Status = TaskStatusSkipped
		task.Message = message
		t.printTaskSkipped(task)
	}
}

// clearServiceProgress clears the interactive service progress output (must be called with lock held)
func (t *Tracker) clearServiceProgress(serviceName string) {
	if !t.interactive {
		return
	}

	state, ok := t.serviceProgress[serviceName]
	if !ok || state.LastLines == 0 {
		return
	}

	// Move cursor up and clear each line
	for i := 0; i < state.LastLines; i++ {
		fmt.Fprintf(t.out, "\033[A\033[2K")
	}

	// Remove from tracking
	delete(t.serviceProgress, serviceName)
}

func (t *Tracker) printTaskStart(task *TrackedTask) {
	t.infoColor.Fprintf(t.out, "  ⏳ %s (%s)\n", task.Name, task.Type)
}

func (t *Tracker) printTaskProgress(task *TrackedTask) {
	bar := t.progressBar(task.Progress, task.Total, 20)
	percent := 0
	if task.Total > 0 {
		percent = (task.Progress * 100) / task.Total
	}
	fmt.Fprintf(t.out, "\r  ⏳ %s %s %d%% %s", task.Name, bar, percent, task.Message)
}

func (t *Tracker) printTaskComplete(task *TrackedTask) {
	duration := task.EndTime.Sub(task.StartTime).Round(time.Second)
	t.successColor.Fprintf(t.out, "  ✓ %s", task.Name)
	if task.Message != "" {
		fmt.Fprintf(t.out, " (%s)", task.Message)
	}
	t.dimColor.Fprintf(t.out, " [%s]\n", duration)
}

func (t *Tracker) printTaskFailed(task *TrackedTask) {
	duration := task.EndTime.Sub(task.StartTime).Round(time.Second)
	t.errorColor.Fprintf(t.out, "  ✗ %s", task.Name)
	if task.Message != "" {
		fmt.Fprintf(t.out, " (%s)", task.Message)
	}
	t.dimColor.Fprintf(t.out, " [%s]\n", duration)
}

func (t *Tracker) printTaskSkipped(task *TrackedTask) {
	t.dimColor.Fprintf(t.out, "  - %s (skipped", task.Name)
	if task.Message != "" {
		fmt.Fprintf(t.out, ": %s", task.Message)
	}
	fmt.Fprintln(t.out, ")")
}

func (t *Tracker) progressBar(current, total, width int) string {
	if total == 0 {
		return strings.Repeat("░", width)
	}

	filled := (current * width) / total
	if filled > width {
		filled = width
	}

	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func (t *Tracker) PrintHeader(cluster string) {
	t.infoColor.Fprintf(t.out, "\nApplying changes to cluster: %s\n\n", cluster)
}

func (t *Tracker) PrintSection(title string) {
	t.infoColor.Fprintf(t.out, "%s:\n", title)
}

func (t *Tracker) PrintSummary() {
	t.mu.Lock()
	defer t.mu.Unlock()

	duration := time.Since(t.started).Round(time.Second)

	var success, failed, skipped int
	for _, task := range t.tasks {
		switch task.Status {
		case TaskStatusSuccess:
			success++
		case TaskStatusFailed:
			failed++
		case TaskStatusSkipped:
			skipped++
		}
	}

	fmt.Fprintln(t.out)
	if failed > 0 {
		t.errorColor.Fprintf(t.out, "✗ Deployment failed")
	} else {
		t.successColor.Fprintf(t.out, "✓ All deployments complete")
	}
	t.dimColor.Fprintf(t.out, " (%d succeeded, %d failed, %d skipped) [%s]\n", success, failed, skipped, duration)
}

func (t *Tracker) HasFailures() bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, task := range t.tasks {
		if task.Status == TaskStatusFailed {
			return true
		}
	}
	return false
}

type DeploymentProgress struct {
	ServiceName        string
	DesiredCount       int32
	RunningCount       int32
	PendingCount       int32
	RolloutState       string
	Healthy            int32
	TaskDefinition     string
	RolloutStateReason string
}

func (t *Tracker) PrintDeploymentProgress(dp DeploymentProgress) {
	bar := t.progressBar(int(dp.RunningCount), int(dp.DesiredCount), 20)

	status := dp.RolloutState
	if status == "" {
		status = "IN_PROGRESS"
	}

	fmt.Fprintf(t.out, "  %s (rolling deployment)\n", dp.ServiceName)
	fmt.Fprintf(t.out, "  ├─ Desired: %d, Running: %d, Pending: %d\n",
		dp.DesiredCount, dp.RunningCount, dp.PendingCount)
	fmt.Fprintf(t.out, "  ├─ %s %d/%d healthy\n", bar, dp.Healthy, dp.DesiredCount)
	if dp.TaskDefinition != "" {
		fmt.Fprintf(t.out, "  ├─ Task definition: %s\n", t.compactTaskDef(dp.TaskDefinition))
	}
	if dp.RolloutStateReason != "" {
		fmt.Fprintf(t.out, "  ├─ Rollout reason: %s\n", dp.RolloutStateReason)
	}

	switch status {
	case "COMPLETED":
		t.successColor.Fprintf(t.out, "  └─ Deployment complete\n")
	case "FAILED":
		t.errorColor.Fprintf(t.out, "  └─ Deployment failed\n")
	default:
		t.dimColor.Fprintf(t.out, "  └─ Waiting for tasks to become healthy...\n")
	}
}

func (t *Tracker) PrintEvent(serviceName, timestamp, message string) {
	t.infoColor.Fprintf(t.out, "  %s ", timestamp)
	t.warnColor.Fprintf(t.out, "%s ", serviceName)
	fmt.Fprintf(t.out, "%s\n", message)
}

func (t *Tracker) compactTaskDef(arn string) string {
	if arn == "" {
		return arn
	}
	if idx := strings.LastIndex(arn, "task-definition/"); idx >= 0 {
		return arn[idx+len("task-definition/"):]
	}
	if idx := strings.LastIndex(arn, "/"); idx >= 0 && idx < len(arn)-1 {
		return arn[idx+1:]
	}
	return arn
}

// DeploymentProgressInfo contains info about a specific deployment
type DeploymentProgressInfo struct {
	ID             string
	TaskDefinition string
	RunningCount   int32
	PendingCount   int32
	FailedTasks    int32
}

// ServiceProgressUpdate contains all info needed to update service progress display
type ServiceProgressUpdate struct {
	ServiceName    string
	DesiredCount   int32
	RunningCount   int32
	PendingCount   int32
	RolloutState   string
	TaskDefinition string
	RolloutReason  string
	Events         []EventInfo       // up to 5 recent events, newest first
	Tasks          []TaskDisplayInfo // individual ECS tasks

	// Deployment tracking
	NewDeployment *DeploymentProgressInfo // PRIMARY deployment (new)
	OldDeployment *DeploymentProgressInfo // ACTIVE deployment (being replaced)
	IsRollingBack bool
}

// UpdateServiceProgress updates the progress display for a service.
// In interactive mode, it re-renders the service section in place.
// In non-interactive mode, it prints events and state changes as log lines.
func (t *Tracker) UpdateServiceProgress(update ServiceProgressUpdate) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.interactive {
		t.updateServiceProgressInteractive(update)
	} else {
		t.updateServiceProgressNonInteractive(update)
	}
}

func (t *Tracker) updateServiceProgressInteractive(update ServiceProgressUpdate) {
	state, exists := t.serviceProgress[update.ServiceName]
	if !exists {
		state = &ServiceProgressState{ServiceName: update.ServiceName}
		t.serviceProgress[update.ServiceName] = state
	}

	// Clear previous output if we rendered before
	if state.LastLines > 0 {
		log.Debug("clearing service progress", "service", update.ServiceName, "lines", state.LastLines)
		// Move cursor up and clear each line
		for i := 0; i < state.LastLines; i++ {
			fmt.Fprintf(t.out, "\033[A\033[2K")
		}
	} else {
		log.Debug("first render for service", "service", update.ServiceName)
	}

	// Update state
	state.DesiredCount = update.DesiredCount
	state.RunningCount = update.RunningCount
	state.PendingCount = update.PendingCount
	state.RolloutState = update.RolloutState
	state.TaskDefinition = update.TaskDefinition
	state.RolloutReason = update.RolloutReason
	state.NewDeployment = update.NewDeployment
	state.OldDeployment = update.OldDeployment
	state.IsRollingBack = update.IsRollingBack
	state.Tasks = update.Tasks

	// Accumulate new events (prepend new ones to keep newest first)
	if len(update.Events) > 0 {
		state.Events = append(update.Events, state.Events...)
	}

	// Render and count lines
	lines := t.renderServiceProgress(state)
	state.LastLines = lines
	log.Debug("rendered service progress", "service", update.ServiceName, "lines", lines, "events", len(state.Events))
}

func (t *Tracker) updateServiceProgressNonInteractive(update ServiceProgressUpdate) {
	// Generate state hash to detect changes
	stateHash := fmt.Sprintf("%d/%d/%d/%s",
		update.RunningCount, update.PendingCount, update.DesiredCount, update.RolloutState)

	lastHash := t.lastStateHash[update.ServiceName]

	// Print new events
	state := t.serviceProgress[update.ServiceName]
	seenEventCount := 0
	if state != nil {
		seenEventCount = len(state.Events)
	}

	// Print events we haven't seen (events are newest first, so print in reverse)
	if len(update.Events) > seenEventCount {
		newEvents := update.Events[0 : len(update.Events)-seenEventCount]
		for i := len(newEvents) - 1; i >= 0; i-- {
			event := newEvents[i]
			timestamp := event.Timestamp.Local().Format("15:04:05")
			t.infoColor.Fprintf(t.out, "  %s ", timestamp)
			t.warnColor.Fprintf(t.out, "%s ", update.ServiceName)
			fmt.Fprintf(t.out, "%s\n", event.Message)
		}
	}

	// Print progress only if state changed
	if stateHash != lastHash {
		status := update.RolloutState
		if status == "" {
			status = "IN_PROGRESS"
		}
		t.dimColor.Fprintf(t.out, "  %s ", update.ServiceName)
		fmt.Fprintf(t.out, "%d/%d running, %s\n",
			update.RunningCount, update.DesiredCount, status)
	}

	// Update tracking state
	t.lastStateHash[update.ServiceName] = stateHash
	if t.serviceProgress[update.ServiceName] == nil {
		t.serviceProgress[update.ServiceName] = &ServiceProgressState{}
	}
	t.serviceProgress[update.ServiceName].Events = update.Events
}

func (t *Tracker) renderServiceProgress(state *ServiceProgressState) int {
	lines := 0

	// Service header
	status := state.RolloutState
	if status == "" {
		status = "IN_PROGRESS"
	}

	statusSymbol := "⏳"
	statusColor := t.infoColor
	if status == "COMPLETED" {
		statusSymbol = "✓"
		statusColor = t.successColor
	} else if status == "FAILED" || state.IsRollingBack || strings.Contains(strings.ToLower(state.RolloutReason), "rollback") {
		statusSymbol = "⚠"
		statusColor = t.warnColor
	}

	statusColor.Fprintf(t.out, "  %s %s", statusSymbol, state.ServiceName)
	if state.IsRollingBack || strings.Contains(strings.ToLower(state.RolloutReason), "rollback") {
		t.warnColor.Fprintf(t.out, " ROLLING BACK")
	}
	fmt.Fprintln(t.out)
	lines++

	// Task definition transition (old → new)
	if state.OldDeployment != nil && state.NewDeployment != nil {
		oldDef := t.compactTaskDef(state.OldDeployment.TaskDefinition)
		newDef := t.compactTaskDef(state.NewDeployment.TaskDefinition)
		if oldDef != newDef {
			fmt.Fprintf(t.out, "     Task Def: %s → ", oldDef)
			t.infoColor.Fprintf(t.out, "%s\n", newDef)
			lines++
		} else {
			fmt.Fprintf(t.out, "     Task Def: %s\n", newDef)
			lines++
		}
	} else if state.TaskDefinition != "" {
		fmt.Fprintf(t.out, "     Task Def: %s\n", t.compactTaskDef(state.TaskDefinition))
		lines++
	}

	// Deployment progress (use new deployment counts if available)
	newRunning := state.RunningCount
	newPending := state.PendingCount
	oldRunning := int32(0)
	if state.NewDeployment != nil {
		newRunning = state.NewDeployment.RunningCount
		newPending = state.NewDeployment.PendingCount
	}
	if state.OldDeployment != nil {
		oldRunning = state.OldDeployment.RunningCount
	}

	// Task counts with old/new breakdown during deployment
	if state.OldDeployment != nil && oldRunning > 0 {
		fmt.Fprintf(t.out, "     Tasks: %d desired, ", state.DesiredCount)
		t.dimColor.Fprintf(t.out, "%d old", oldRunning)
		fmt.Fprintf(t.out, " + ")
		t.infoColor.Fprintf(t.out, "%d new", newRunning)
		if newPending > 0 {
			fmt.Fprintf(t.out, " (%d pending)", newPending)
		}
		fmt.Fprintln(t.out)
	} else {
		fmt.Fprintf(t.out, "     Tasks: %d desired, %d running", state.DesiredCount, newRunning)
		if newPending > 0 {
			fmt.Fprintf(t.out, ", %d pending", newPending)
		}
		fmt.Fprintln(t.out)
	}
	lines++

	// Task list
	if len(state.Tasks) > 0 {
		for _, task := range state.Tasks {
			shortID := task.TaskID
			if len(shortID) > 8 {
				shortID = shortID[:8]
			}
			startedAt := "-"
			if task.StartedAt != nil {
				startedAt = task.StartedAt.Local().Format("15:04:05")
			}
			taskDef := t.compactTaskDef(task.TaskDefinition)

			statusColor := t.dimColor
			switch task.LastStatus {
			case "RUNNING":
				statusColor = t.successColor
			case "PENDING", "PROVISIONING", "ACTIVATING":
				statusColor = t.warnColor
			case "STOPPED", "DEPROVISIONING", "STOPPING":
				statusColor = t.errorColor
			}

			fmt.Fprintf(t.out, "       %s  ", shortID)
			statusColor.Fprintf(t.out, "%-10s", task.LastStatus)
			fmt.Fprintf(t.out, "  %-10s  %s  %s\n", task.DesiredStatus, startedAt, taskDef)
			lines++
		}
	}

	// Progress bar (based on new deployment progress)
	bar := t.progressBar(int(newRunning), int(state.DesiredCount), 20)
	pct := 0
	if state.DesiredCount > 0 {
		pct = int(newRunning * 100 / state.DesiredCount)
	}
	fmt.Fprintf(t.out, "     Progress: %s %d%%\n", bar, pct)
	lines++

	// Failed tasks warning
	if state.NewDeployment != nil && state.NewDeployment.FailedTasks > 0 {
		t.errorColor.Fprintf(t.out, "     Failed: %d tasks\n", state.NewDeployment.FailedTasks)
		lines++
	}

	// Rollout state
	rolloutColor := t.dimColor
	if status == "COMPLETED" {
		rolloutColor = t.successColor
	} else if status == "FAILED" {
		rolloutColor = t.errorColor
	}
	rolloutColor.Fprintf(t.out, "     Rollout: %s\n", status)
	lines++

	// Rollout reason if present
	if state.RolloutReason != "" {
		t.dimColor.Fprintf(t.out, "     Reason: %s\n", state.RolloutReason)
		lines++
	}

	// Events (up to 5)
	if len(state.Events) > 0 {
		fmt.Fprintf(t.out, "     Events:\n")
		lines++
		count := len(state.Events)
		if count > 5 {
			count = 5
		}
		for i := 0; i < count; i++ {
			event := state.Events[i]
			timestamp := event.Timestamp.Local().Format("15:04:05")
			t.dimColor.Fprintf(t.out, "       %s %s\n", timestamp, event.Message)
			lines++
		}
	}

	fmt.Fprintln(t.out) // blank line after service
	lines++

	return lines
}

// PrintLogs prints log lines (used for task failure logs)
func (t *Tracker) PrintLogs(serviceName string, logs []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(logs) == 0 {
		return
	}

	t.errorColor.Fprintf(t.out, "  %s task logs:\n", serviceName)
	for _, line := range logs {
		t.dimColor.Fprintf(t.out, "    %s\n", line)
	}
	fmt.Fprintln(t.out)
}
