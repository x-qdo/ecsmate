package engine

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "PENDING"
	TaskStatusRunning   TaskStatus = "RUNNING"
	TaskStatusSuccess   TaskStatus = "SUCCESS"
	TaskStatusFailed    TaskStatus = "FAILED"
	TaskStatusSkipped   TaskStatus = "SKIPPED"
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

type Tracker struct {
	mu       sync.Mutex
	out      io.Writer
	tasks    map[string]*TrackedTask
	order    []string
	noColor  bool
	started  time.Time

	successColor *color.Color
	errorColor   *color.Color
	warnColor    *color.Color
	infoColor    *color.Color
	dimColor     *color.Color
}

func NewTracker(out io.Writer, noColor bool) *Tracker {
	if noColor {
		color.NoColor = true
	}

	return &Tracker{
		out:          out,
		tasks:        make(map[string]*TrackedTask),
		order:        make([]string, 0),
		noColor:      noColor,
		started:      time.Now(),
		successColor: color.New(color.FgGreen),
		errorColor:   color.New(color.FgRed),
		warnColor:    color.New(color.FgYellow),
		infoColor:    color.New(color.FgCyan),
		dimColor:     color.New(color.FgWhite),
	}
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
	ServiceName  string
	DesiredCount int32
	RunningCount int32
	PendingCount int32
	RolloutState string
	Healthy      int32
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

	switch status {
	case "COMPLETED":
		t.successColor.Fprintf(t.out, "  └─ Deployment complete\n")
	case "FAILED":
		t.errorColor.Fprintf(t.out, "  └─ Deployment failed\n")
	default:
		t.dimColor.Fprintf(t.out, "  └─ Waiting for tasks to become healthy...\n")
	}
}
