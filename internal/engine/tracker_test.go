package engine

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestRenderServiceProgressLineCount(t *testing.T) {
	tests := []struct {
		name  string
		state *ServiceProgressState
	}{
		{
			name: "minimal state",
			state: &ServiceProgressState{
				ServiceName:    "test-service",
				DesiredCount:   1,
				RunningCount:   1,
				RolloutState:   "IN_PROGRESS",
				TaskDefinition: "test:1",
			},
		},
		{
			name: "with rollout reason",
			state: &ServiceProgressState{
				ServiceName:    "test-service",
				DesiredCount:   1,
				RunningCount:   1,
				RolloutState:   "IN_PROGRESS",
				TaskDefinition: "test:1",
				RolloutReason:  "Deployment in progress",
			},
		},
		{
			name: "with 1 event",
			state: &ServiceProgressState{
				ServiceName:    "test-service",
				DesiredCount:   1,
				RunningCount:   1,
				RolloutState:   "IN_PROGRESS",
				TaskDefinition: "test:1",
				Events: []EventInfo{
					{Timestamp: time.Now(), Message: "Event 1"},
				},
			},
		},
		{
			name: "with 3 events",
			state: &ServiceProgressState{
				ServiceName:    "test-service",
				DesiredCount:   1,
				RunningCount:   1,
				RolloutState:   "IN_PROGRESS",
				TaskDefinition: "test:1",
				Events: []EventInfo{
					{Timestamp: time.Now(), Message: "Event 1"},
					{Timestamp: time.Now(), Message: "Event 2"},
					{Timestamp: time.Now(), Message: "Event 3"},
				},
			},
		},
		{
			name: "with 5 events",
			state: &ServiceProgressState{
				ServiceName:    "test-service",
				DesiredCount:   1,
				RunningCount:   1,
				RolloutState:   "IN_PROGRESS",
				TaskDefinition: "test:1",
				Events: []EventInfo{
					{Timestamp: time.Now(), Message: "Event 1"},
					{Timestamp: time.Now(), Message: "Event 2"},
					{Timestamp: time.Now(), Message: "Event 3"},
					{Timestamp: time.Now(), Message: "Event 4"},
					{Timestamp: time.Now(), Message: "Event 5"},
				},
			},
		},
		{
			name: "with 7 events (capped at 5)",
			state: &ServiceProgressState{
				ServiceName:    "test-service",
				DesiredCount:   1,
				RunningCount:   1,
				RolloutState:   "IN_PROGRESS",
				TaskDefinition: "test:1",
				Events: []EventInfo{
					{Timestamp: time.Now(), Message: "Event 1"},
					{Timestamp: time.Now(), Message: "Event 2"},
					{Timestamp: time.Now(), Message: "Event 3"},
					{Timestamp: time.Now(), Message: "Event 4"},
					{Timestamp: time.Now(), Message: "Event 5"},
					{Timestamp: time.Now(), Message: "Event 6"},
					{Timestamp: time.Now(), Message: "Event 7"},
				},
			},
		},
		{
			name: "with old and new deployment",
			state: &ServiceProgressState{
				ServiceName:    "test-service",
				DesiredCount:   2,
				RunningCount:   2,
				RolloutState:   "IN_PROGRESS",
				TaskDefinition: "test:2",
				NewDeployment: &DeploymentProgressInfo{
					TaskDefinition: "test:2",
					RunningCount:   1,
					PendingCount:   1,
				},
				OldDeployment: &DeploymentProgressInfo{
					TaskDefinition: "test:1",
					RunningCount:   1,
				},
			},
		},
		{
			name: "with failed tasks",
			state: &ServiceProgressState{
				ServiceName:    "test-service",
				DesiredCount:   2,
				RunningCount:   1,
				RolloutState:   "IN_PROGRESS",
				TaskDefinition: "test:2",
				NewDeployment: &DeploymentProgressInfo{
					TaskDefinition: "test:2",
					RunningCount:   0,
					FailedTasks:    2,
				},
			},
		},
		{
			name: "full state with everything",
			state: &ServiceProgressState{
				ServiceName:    "test-service",
				DesiredCount:   2,
				RunningCount:   2,
				RolloutState:   "IN_PROGRESS",
				TaskDefinition: "test:2",
				RolloutReason:  "Deployment in progress",
				NewDeployment: &DeploymentProgressInfo{
					TaskDefinition: "test:2",
					RunningCount:   1,
					PendingCount:   1,
					FailedTasks:    1,
				},
				OldDeployment: &DeploymentProgressInfo{
					TaskDefinition: "test:1",
					RunningCount:   1,
				},
				Events: []EventInfo{
					{Timestamp: time.Now(), Message: "Event 1"},
					{Timestamp: time.Now(), Message: "Event 2"},
					{Timestamp: time.Now(), Message: "Event 3"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			tracker := NewTracker(buf, true) // noColor=true for predictable output

			// Render and get reported line count
			reportedLines := tracker.renderServiceProgress(tt.state)

			// Count actual newlines in output
			output := buf.String()
			actualLines := strings.Count(output, "\n")

			if reportedLines != actualLines {
				t.Errorf("line count mismatch: reported=%d, actual=%d\nOutput:\n%s",
					reportedLines, actualLines, output)
			}
		})
	}
}

func TestInteractiveRerender(t *testing.T) {
	buf := &bytes.Buffer{}
	tracker := NewTracker(buf, true)
	// Force interactive mode for testing
	tracker.interactive = true

	now := time.Now()

	// First update with 1 event
	tracker.UpdateServiceProgress(ServiceProgressUpdate{
		ServiceName:    "test-service",
		DesiredCount:   1,
		RunningCount:   0,
		RolloutState:   "IN_PROGRESS",
		TaskDefinition: "test:1",
		Events: []EventInfo{
			{Timestamp: now, Message: "Event 1"},
		},
	})

	firstOutput := buf.String()
	firstLines := strings.Count(firstOutput, "\n")

	// Verify state was stored correctly
	state := tracker.serviceProgress["test-service"]
	if state == nil {
		t.Fatal("service progress state not stored")
	}
	if state.LastLines != firstLines {
		t.Errorf("LastLines mismatch after first render: stored=%d, actual=%d",
			state.LastLines, firstLines)
	}

	// Second update with 3 more events (total 4)
	buf.Reset()
	tracker.UpdateServiceProgress(ServiceProgressUpdate{
		ServiceName:    "test-service",
		DesiredCount:   1,
		RunningCount:   1,
		RolloutState:   "IN_PROGRESS",
		TaskDefinition: "test:1",
		Events: []EventInfo{
			{Timestamp: now.Add(time.Second), Message: "Event 2"},
			{Timestamp: now.Add(2 * time.Second), Message: "Event 3"},
			{Timestamp: now.Add(3 * time.Second), Message: "Event 4"},
		},
	})

	// Count escape sequences for moving up (should match first render's line count)
	secondOutput := buf.String()
	escapeUp := "\033[A\033[2K"
	escapeCount := strings.Count(secondOutput, escapeUp)

	if escapeCount != firstLines {
		t.Errorf("escape sequence count mismatch: expected=%d (firstLines), got=%d\nFirst output (%d lines):\n%s\nSecond output:\n%s",
			firstLines, escapeCount, firstLines, firstOutput, secondOutput)
	}

	// Verify accumulated events
	if len(state.Events) != 4 {
		t.Errorf("expected 4 accumulated events, got %d", len(state.Events))
	}
}

func TestPollingSimulation(t *testing.T) {
	buf := &bytes.Buffer{}
	tracker := NewTracker(buf, true)
	tracker.interactive = true

	now := time.Now()

	// Simulate polling updates like executor does
	updates := []ServiceProgressUpdate{
		{
			ServiceName:    "test-service",
			DesiredCount:   1,
			RunningCount:   0,
			PendingCount:   1,
			RolloutState:   "IN_PROGRESS",
			TaskDefinition: "test:2",
			NewDeployment: &DeploymentProgressInfo{
				TaskDefinition: "test:2",
				RunningCount:   0,
				PendingCount:   1,
			},
			OldDeployment: &DeploymentProgressInfo{
				TaskDefinition: "test:1",
				RunningCount:   1,
			},
			Events: []EventInfo{
				{Timestamp: now, Message: "started 1 tasks"},
			},
		},
		{
			ServiceName:    "test-service",
			DesiredCount:   1,
			RunningCount:   1,
			PendingCount:   0,
			RolloutState:   "IN_PROGRESS",
			TaskDefinition: "test:2",
			NewDeployment: &DeploymentProgressInfo{
				TaskDefinition: "test:2",
				RunningCount:   1,
				PendingCount:   0,
			},
			OldDeployment: &DeploymentProgressInfo{
				TaskDefinition: "test:1",
				RunningCount:   1,
			},
			Events: []EventInfo{
				{Timestamp: now.Add(5 * time.Second), Message: "registered in target group"},
			},
		},
		{
			ServiceName:    "test-service",
			DesiredCount:   1,
			RunningCount:   1,
			PendingCount:   0,
			RolloutState:   "IN_PROGRESS",
			TaskDefinition: "test:2",
			NewDeployment: &DeploymentProgressInfo{
				TaskDefinition: "test:2",
				RunningCount:   1,
				PendingCount:   0,
			},
			OldDeployment: &DeploymentProgressInfo{
				TaskDefinition: "test:1",
				RunningCount:   0, // old task stopped
			},
			Events: []EventInfo{
				{Timestamp: now.Add(10 * time.Second), Message: "stopped 1 old tasks"},
			},
		},
		{
			ServiceName:    "test-service",
			DesiredCount:   1,
			RunningCount:   1,
			PendingCount:   0,
			RolloutState:   "COMPLETED",
			TaskDefinition: "test:2",
			NewDeployment: &DeploymentProgressInfo{
				TaskDefinition: "test:2",
				RunningCount:   1,
				PendingCount:   0,
			},
			// OldDeployment gone
			Events: []EventInfo{
				{Timestamp: now.Add(15 * time.Second), Message: "reached steady state"},
			},
		},
	}

	var prevLines int
	for i, update := range updates {
		buf.Reset()
		tracker.UpdateServiceProgress(update)

		state := tracker.serviceProgress["test-service"]
		output := buf.String()

		// Count escape sequences
		escapeCount := strings.Count(output, "\033[A\033[2K")

		// First update: no escapes (nothing to erase)
		// Subsequent: should erase previous line count
		if i == 0 {
			if escapeCount != 0 {
				t.Errorf("update %d: expected 0 escape sequences for first render, got %d", i, escapeCount)
			}
		} else {
			if escapeCount != prevLines {
				t.Errorf("update %d: escape count %d doesn't match previous lines %d\nEvents: %d\nOutput:\n%s",
					i, escapeCount, prevLines, len(state.Events), output)
			}
		}

		prevLines = state.LastLines
		t.Logf("update %d: events=%d, lastLines=%d", i, len(state.Events), state.LastLines)
	}
}

func TestDeploymentTransitions(t *testing.T) {
	// Test that line counts are correct when deployment state changes
	// (e.g., OldDeployment goes from present to nil)
	buf := &bytes.Buffer{}
	tracker := NewTracker(buf, true)
	tracker.interactive = true

	now := time.Now()

	// Update 1: Both old and new deployment (shows "X old + Y new" line)
	tracker.UpdateServiceProgress(ServiceProgressUpdate{
		ServiceName:    "test-service",
		DesiredCount:   1,
		RunningCount:   2,
		RolloutState:   "IN_PROGRESS",
		TaskDefinition: "test:2",
		NewDeployment: &DeploymentProgressInfo{
			TaskDefinition: "test:2",
			RunningCount:   1,
		},
		OldDeployment: &DeploymentProgressInfo{
			TaskDefinition: "test:1",
			RunningCount:   1,
		},
		Events: []EventInfo{
			{Timestamp: now, Message: "event 1"},
		},
	})

	state := tracker.serviceProgress["test-service"]
	linesWithOld := state.LastLines
	t.Logf("with OldDeployment: %d lines", linesWithOld)

	// Update 2: Only new deployment (old removed - different line format)
	buf.Reset()
	tracker.UpdateServiceProgress(ServiceProgressUpdate{
		ServiceName:    "test-service",
		DesiredCount:   1,
		RunningCount:   1,
		RolloutState:   "COMPLETED",
		TaskDefinition: "test:2",
		NewDeployment: &DeploymentProgressInfo{
			TaskDefinition: "test:2",
			RunningCount:   1,
		},
		// OldDeployment is nil now
		Events: []EventInfo{
			{Timestamp: now.Add(time.Second), Message: "event 2"},
		},
	})

	output := buf.String()
	escapeCount := strings.Count(output, "\033[A\033[2K")

	if escapeCount != linesWithOld {
		t.Errorf("escape count %d doesn't match previous lines %d when OldDeployment removed\nOutput:\n%s",
			escapeCount, linesWithOld, output)
	}

	linesWithoutOld := state.LastLines
	t.Logf("without OldDeployment: %d lines, erased %d", linesWithoutOld, escapeCount)
}

func TestCompleteTaskClearsServiceProgress(t *testing.T) {
	buf := &bytes.Buffer{}
	tracker := NewTracker(buf, true)
	tracker.interactive = true

	now := time.Now()

	// First, render service progress
	tracker.UpdateServiceProgress(ServiceProgressUpdate{
		ServiceName:    "test-service",
		DesiredCount:   1,
		RunningCount:   1,
		RolloutState:   "COMPLETED",
		TaskDefinition: "test:1",
		Events: []EventInfo{
			{Timestamp: now, Message: "event 1"},
			{Timestamp: now, Message: "event 2"},
		},
	})

	state := tracker.serviceProgress["test-service"]
	if state == nil {
		t.Fatal("service progress state not stored")
	}
	progressLines := state.LastLines
	t.Logf("service progress rendered with %d lines", progressLines)

	// Now call CompleteTask - it should clear the service progress first
	buf.Reset()
	tracker.AddTask("test-service", "service")
	tracker.StartTask("test-service")
	tracker.CompleteTask("test-service", "deployed")

	output := buf.String()
	escapeCount := strings.Count(output, "\033[A\033[2K")

	// Should have erased the service progress lines
	if escapeCount != progressLines {
		t.Errorf("CompleteTask should erase %d lines, but erased %d\nOutput:\n%s",
			progressLines, escapeCount, output)
	}

	// Service progress should be removed from tracking
	if _, ok := tracker.serviceProgress["test-service"]; ok {
		t.Error("service progress should be removed after CompleteTask")
	}
}

func TestLineCountWithVaryingEvents(t *testing.T) {
	buf := &bytes.Buffer{}
	tracker := NewTracker(buf, true)
	tracker.interactive = true

	now := time.Now()
	baseState := ServiceProgressUpdate{
		ServiceName:    "test-service",
		DesiredCount:   1,
		RunningCount:   0,
		RolloutState:   "IN_PROGRESS",
		TaskDefinition: "test:1",
	}

	// Simulate multiple updates with increasing events
	for i := 1; i <= 7; i++ {
		buf.Reset()

		update := baseState
		update.Events = []EventInfo{
			{Timestamp: now.Add(time.Duration(i) * time.Second), Message: "New event"},
		}

		tracker.UpdateServiceProgress(update)

		state := tracker.serviceProgress["test-service"]
		if state == nil {
			t.Fatalf("iteration %d: service progress state not stored", i)
		}

		// Get the output after escape sequences (the actual rendered content)
		output := buf.String()

		// Count lines in the rendered portion (after all escape sequences)
		lastEscape := strings.LastIndex(output, "\033[2K")
		renderedPortion := output
		if lastEscape >= 0 {
			renderedPortion = output[lastEscape+4:] // +4 for "\033[2K"
		}

		actualRenderedLines := strings.Count(renderedPortion, "\n")

		// The stored LastLines should match actual rendered lines
		if state.LastLines != actualRenderedLines {
			t.Errorf("iteration %d: LastLines=%d doesn't match actual rendered lines=%d\nEvents accumulated: %d\nRendered portion:\n%s",
				i, state.LastLines, actualRenderedLines, len(state.Events), renderedPortion)
		}
	}
}
