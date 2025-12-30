package diff

import (
	"bytes"
	"strings"
	"testing"
)

func TestRenderer_RenderDiff_NoChanges(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	renderer.RenderDiff([]DiffEntry{})

	output := buf.String()
	if !strings.Contains(output, "No changes detected") {
		t.Errorf("expected 'No changes detected' message, got: %s", output)
	}
}

func TestRenderer_RenderDiff_WithEntries(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	entries := []DiffEntry{
		{
			Type:     DiffTypeCreate,
			Name:     "php-taskdef",
			Resource: "TaskDefinition",
			Details:  "new task definition",
		},
		{
			Type:     DiffTypeUpdate,
			Name:     "web-service",
			Resource: "Service",
		},
		{
			Type:     DiffTypeNoop,
			Name:     "unchanged-service",
			Resource: "Service",
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	if !strings.Contains(output, "TaskDefinitions:") {
		t.Error("expected TaskDefinitions section")
	}

	if !strings.Contains(output, "Services:") {
		t.Error("expected Services section")
	}

	if !strings.Contains(output, "+ php-taskdef") {
		t.Error("expected create marker for php-taskdef")
	}

	if !strings.Contains(output, "~ web-service") {
		t.Error("expected update marker for web-service")
	}

	if !strings.Contains(output, "= unchanged-service") {
		t.Error("expected noop marker for unchanged-service")
	}
}

func TestRenderer_RenderSummary(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	summary := DiffSummary{
		Creates: 2,
		Updates: 1,
		Deletes: 0,
		Noops:   3,
	}

	renderer.RenderSummary(summary)

	output := buf.String()

	if !strings.Contains(output, "2 to create") {
		t.Errorf("expected '2 to create', got: %s", output)
	}

	if !strings.Contains(output, "1 to update") {
		t.Errorf("expected '1 to update', got: %s", output)
	}

	if !strings.Contains(output, "3 unchanged") {
		t.Errorf("expected '3 unchanged', got: %s", output)
	}

	if strings.Contains(output, "to delete") {
		t.Error("should not show delete count when 0")
	}
}

func TestRenderer_RenderSummary_NoChanges(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	summary := DiffSummary{
		Creates: 0,
		Updates: 0,
		Deletes: 0,
		Noops:   0,
	}

	renderer.RenderSummary(summary)

	output := buf.String()

	if !strings.Contains(output, "No changes") {
		t.Errorf("expected 'No changes', got: %s", output)
	}
}

func TestRenderer_RenderDelete(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	entries := []DiffEntry{
		{
			Type:     DiffTypeDelete,
			Name:     "old-service",
			Resource: "Service",
			Details:  "will be removed",
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	if !strings.Contains(output, "- old-service") {
		t.Errorf("expected delete marker for old-service, got: %s", output)
	}
}

func TestRenderer_RenderDiff_GroupsByResource(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	entries := []DiffEntry{
		{Type: DiffTypeCreate, Name: "svc-a", Resource: "Service"},
		{Type: DiffTypeCreate, Name: "td-a", Resource: "TaskDefinition"},
		{Type: DiffTypeCreate, Name: "svc-b", Resource: "Service"},
		{Type: DiffTypeCreate, Name: "st-a", Resource: "ScheduledTask"},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	tdIdx := strings.Index(output, "TaskDefinitions:")
	svcIdx := strings.Index(output, "Services:")
	stIdx := strings.Index(output, "ScheduledTasks:")

	if tdIdx == -1 || svcIdx == -1 || stIdx == -1 {
		t.Fatalf("missing section headers in output: %s", output)
	}

	if tdIdx > svcIdx {
		t.Error("TaskDefinitions should appear before Services")
	}

	if svcIdx > stIdx {
		t.Error("Services should appear before ScheduledTasks")
	}
}

func TestDiffEntry_Types(t *testing.T) {
	if DiffTypeCreate != "CREATE" {
		t.Errorf("DiffTypeCreate = %s, expected CREATE", DiffTypeCreate)
	}
	if DiffTypeUpdate != "UPDATE" {
		t.Errorf("DiffTypeUpdate = %s, expected UPDATE", DiffTypeUpdate)
	}
	if DiffTypeDelete != "DELETE" {
		t.Errorf("DiffTypeDelete = %s, expected DELETE", DiffTypeDelete)
	}
	if DiffTypeRecreate != "RECREATE" {
		t.Errorf("DiffTypeRecreate = %s, expected RECREATE", DiffTypeRecreate)
	}
	if DiffTypeNoop != "NOOP" {
		t.Errorf("DiffTypeNoop = %s, expected NOOP", DiffTypeNoop)
	}
}

func TestRenderer_RenderRecreate(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	entries := []DiffEntry{
		{
			Type:            DiffTypeRecreate,
			Name:            "web-service",
			Resource:        "Service",
			RecreateReasons: []string{"launchType changed from EC2 to FARGATE"},
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	if !strings.Contains(output, "-/+ web-service") {
		t.Errorf("expected recreate marker '-/+' for web-service, got: %s", output)
	}

	if !strings.Contains(output, "must be recreated") {
		t.Errorf("expected 'must be recreated' message, got: %s", output)
	}

	if !strings.Contains(output, "launchType changed") {
		t.Errorf("expected recreate reason in output, got: %s", output)
	}
}

func TestRenderer_RenderRecreate_WithMultipleReasons(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	entries := []DiffEntry{
		{
			Type:     DiffTypeRecreate,
			Name:     "web-service",
			Resource: "Service",
			RecreateReasons: []string{
				"launchType changed from EC2 to FARGATE",
				"loadBalancers changed (immutable after creation)",
			},
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	if !strings.Contains(output, "launchType changed") {
		t.Errorf("expected first recreate reason, got: %s", output)
	}

	if !strings.Contains(output, "loadBalancers changed") {
		t.Errorf("expected second recreate reason, got: %s", output)
	}
}

func TestRenderer_RenderRecreate_WithDiff(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	entries := []DiffEntry{
		{
			Type:            DiffTypeRecreate,
			Name:            "web-service",
			Resource:        "Service",
			RecreateReasons: []string{"launchType changed"},
			Current: map[string]interface{}{
				"launchType": "EC2",
			},
			Desired: map[string]interface{}{
				"launchType": "FARGATE",
			},
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	if !strings.Contains(output, "-/+ web-service") {
		t.Errorf("expected recreate marker, got: %s", output)
	}
}

func TestRenderer_RenderSummary_WithRecreates(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	summary := DiffSummary{
		Creates:   1,
		Updates:   1,
		Recreates: 2,
		Deletes:   0,
		Noops:     3,
	}

	renderer.RenderSummary(summary)

	output := buf.String()

	if !strings.Contains(output, "1 to create") {
		t.Errorf("expected '1 to create', got: %s", output)
	}

	if !strings.Contains(output, "1 to update") {
		t.Errorf("expected '1 to update', got: %s", output)
	}

	if !strings.Contains(output, "2 to recreate") {
		t.Errorf("expected '2 to recreate', got: %s", output)
	}

	if !strings.Contains(output, "3 unchanged") {
		t.Errorf("expected '3 unchanged', got: %s", output)
	}
}

func TestRenderer_RenderSummary_RecreateOnly(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	summary := DiffSummary{
		Creates:   0,
		Updates:   0,
		Recreates: 1,
		Deletes:   0,
		Noops:     0,
	}

	renderer.RenderSummary(summary)

	output := buf.String()

	if !strings.Contains(output, "1 to recreate") {
		t.Errorf("expected '1 to recreate', got: %s", output)
	}

	if strings.Contains(output, "No changes") {
		t.Error("should not say 'No changes' when there are recreates")
	}
}
