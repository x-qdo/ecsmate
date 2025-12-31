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
			Desired: map[string]interface{}{
				"family": "php-task",
			},
		},
		{
			Type:     DiffTypeUpdate,
			Name:     "web-service",
			Resource: "Service",
			Current: map[string]interface{}{
				"desiredCount": 1,
			},
			Desired: map[string]interface{}{
				"desiredCount": 2,
			},
		},
		{
			Type:     DiffTypeNoop,
			Name:     "unchanged-service",
			Resource: "Service",
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	// New format uses box drawing and Resource/Name format
	if !strings.Contains(output, "TaskDefinition/php-taskdef") {
		t.Errorf("expected TaskDefinition/php-taskdef, got: %s", output)
	}

	if !strings.Contains(output, "Service/web-service") {
		t.Errorf("expected Service/web-service, got: %s", output)
	}

	// Should contain Create and Update actions
	if !strings.Contains(output, "Create") {
		t.Error("expected Create action marker")
	}

	if !strings.Contains(output, "Update") {
		t.Error("expected Update action marker")
	}

	// Noop entries should not appear in new format
	if strings.Contains(output, "unchanged-service") {
		t.Error("noop entries should be filtered out")
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

	renderer.RenderSummary(summary, "test-manifest")

	output := buf.String()

	if !strings.Contains(output, "create: 2 resources") {
		t.Errorf("expected 'create: 2 resources', got: %s", output)
	}

	if !strings.Contains(output, "update: 1 resources") {
		t.Errorf("expected 'update: 1 resources', got: %s", output)
	}

	if !strings.Contains(output, "test-manifest") {
		t.Errorf("expected manifest name in output, got: %s", output)
	}

	if strings.Contains(output, "delete") {
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

	renderer.RenderSummary(summary, "")

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
			Current: map[string]interface{}{
				"name": "old-service",
			},
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	if !strings.Contains(output, "Delete") {
		t.Errorf("expected Delete action marker, got: %s", output)
	}

	if !strings.Contains(output, "Service/old-service") {
		t.Errorf("expected Service/old-service, got: %s", output)
	}
}

func TestRenderer_RenderDiff_AllChanges(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	entries := []DiffEntry{
		{Type: DiffTypeCreate, Name: "svc-a", Resource: "Service", Desired: map[string]interface{}{"name": "a"}},
		{Type: DiffTypeCreate, Name: "td-a", Resource: "TaskDefinition", Desired: map[string]interface{}{"name": "a"}},
		{Type: DiffTypeCreate, Name: "svc-b", Resource: "Service", Desired: map[string]interface{}{"name": "b"}},
		{Type: DiffTypeCreate, Name: "st-a", Resource: "ScheduledTask", Desired: map[string]interface{}{"name": "a"}},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	// All create entries should be present
	if !strings.Contains(output, "Service/svc-a") {
		t.Errorf("expected Service/svc-a, got: %s", output)
	}
	if !strings.Contains(output, "TaskDefinition/td-a") {
		t.Errorf("expected TaskDefinition/td-a, got: %s", output)
	}
	if !strings.Contains(output, "ScheduledTask/st-a") {
		t.Errorf("expected ScheduledTask/st-a, got: %s", output)
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

	if !strings.Contains(output, "Recreate") {
		t.Errorf("expected Recreate action marker, got: %s", output)
	}

	if !strings.Contains(output, "Service/web-service") {
		t.Errorf("expected Service/web-service, got: %s", output)
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

	if !strings.Contains(output, "launchType changed") {
		t.Errorf("expected first recreate reason, got: %s", output)
	}

	if !strings.Contains(output, "loadBalancers changed") {
		t.Errorf("expected second recreate reason, got: %s", output)
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

	renderer.RenderSummary(summary, "")

	output := buf.String()

	if !strings.Contains(output, "create: 1 resources") {
		t.Errorf("expected 'create: 1 resources', got: %s", output)
	}

	if !strings.Contains(output, "update: 1 resources") {
		t.Errorf("expected 'update: 1 resources', got: %s", output)
	}

	if !strings.Contains(output, "recreate: 2 resources") {
		t.Errorf("expected 'recreate: 2 resources', got: %s", output)
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

	renderer.RenderSummary(summary, "")

	output := buf.String()

	if !strings.Contains(output, "recreate: 1 resources") {
		t.Errorf("expected 'recreate: 1 resources', got: %s", output)
	}

	if strings.Contains(output, "No changes") {
		t.Error("should not say 'No changes' when there are recreates")
	}
}

func TestRenderer_RenderHeader(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	renderer.RenderHeader("my-manifest")

	output := buf.String()

	if !strings.Contains(output, "Planning") {
		t.Errorf("expected 'Planning' in header, got: %s", output)
	}

	if !strings.Contains(output, "my-manifest") {
		t.Errorf("expected manifest name in header, got: %s", output)
	}
}

func TestRenderer_BoxedFormat(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	entries := []DiffEntry{
		{
			Type:     DiffTypeUpdate,
			Name:     "test-service",
			Resource: "Service",
			Current: map[string]interface{}{
				"desiredCount": 1,
				"cluster":      "test-cluster",
			},
			Desired: map[string]interface{}{
				"desiredCount": 2,
				"cluster":      "test-cluster",
			},
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	// Should contain box drawing characters
	if !strings.Contains(output, "┌") {
		t.Errorf("expected box top character, got: %s", output)
	}

	if !strings.Contains(output, "└") {
		t.Errorf("expected box bottom character, got: %s", output)
	}

	// Should show the changed field
	if !strings.Contains(output, "desiredCount") {
		t.Errorf("expected desiredCount field, got: %s", output)
	}
}

func TestRenderer_ArrayDiffExpanded(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	// Test that arrays with complex objects are expanded, not compacted
	entries := []DiffEntry{
		{
			Type:     DiffTypeUpdate,
			Name:     "web-task",
			Resource: "TaskDefinition",
			Current: map[string]interface{}{
				"family": "web-task",
				"portMappings": []interface{}{
					map[string]interface{}{
						"containerPort": 80,
						"hostPort":      80,
						"protocol":      "tcp",
					},
				},
			},
			Desired: map[string]interface{}{
				"family": "web-task",
				"portMappings": []interface{}{
					map[string]interface{}{
						"containerPort": 80,
						"protocol":      "tcp",
					},
					map[string]interface{}{
						"containerPort": 443,
						"protocol":      "tcp",
					},
				},
			},
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	// Should NOT contain compacted format like {...3 fields}
	if strings.Contains(output, "{...") {
		t.Errorf("should not compact objects, got: %s", output)
	}

	// Should show individual fields expanded
	if !strings.Contains(output, "containerPort") {
		t.Errorf("expected containerPort to be expanded, got: %s", output)
	}

	if !strings.Contains(output, "protocol") {
		t.Errorf("expected protocol to be expanded, got: %s", output)
	}

	// Should show port 443 for the new mapping
	if !strings.Contains(output, "443") {
		t.Errorf("expected port 443 in output, got: %s", output)
	}
}

func TestRenderer_NestedObjectsExpanded(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	entries := []DiffEntry{
		{
			Type:     DiffTypeCreate,
			Name:     "new-service",
			Resource: "Service",
			Desired: map[string]interface{}{
				"cluster": "my-cluster",
				"networkConfiguration": map[string]interface{}{
					"subnets":        []interface{}{"subnet-1", "subnet-2"},
					"securityGroups": []interface{}{"sg-1"},
				},
				"loadBalancers": []interface{}{
					map[string]interface{}{
						"targetGroupArn": "arn:aws:elasticloadbalancing:...",
						"containerName":  "web",
						"containerPort":  80,
					},
				},
			},
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	// Should NOT contain compacted format
	if strings.Contains(output, "{...") {
		t.Errorf("should not compact nested objects, got: %s", output)
	}

	// Should show nested fields
	if !strings.Contains(output, "subnets") {
		t.Errorf("expected subnets field, got: %s", output)
	}

	if !strings.Contains(output, "targetGroupArn") {
		t.Errorf("expected targetGroupArn field, got: %s", output)
	}

	if !strings.Contains(output, "containerName") {
		t.Errorf("expected containerName field, got: %s", output)
	}
}

func TestRenderer_ECSAssignedDefaultsFiltered(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	// Test that ECS-assigned defaults (like hostPort) are not shown as removed
	// when they exist in current (remote) but not in desired (local config)
	entries := []DiffEntry{
		{
			Type:     DiffTypeUpdate,
			Name:     "web-task",
			Resource: "TaskDefinition",
			Current: map[string]interface{}{
				"family": "web-task",
				"containerDefinitions": []interface{}{
					map[string]interface{}{
						"name": "web",
						"portMappings": []interface{}{
							map[string]interface{}{
								"containerPort": 80,
								"hostPort":      80, // ECS assigns this when not specified
								"protocol":      "tcp",
							},
						},
					},
				},
			},
			Desired: map[string]interface{}{
				"family": "web-task",
				"containerDefinitions": []interface{}{
					map[string]interface{}{
						"name": "web",
						"portMappings": []interface{}{
							map[string]interface{}{
								"containerPort": 80,
								// hostPort not specified - ECS will assign it
								"protocol": "tcp",
							},
						},
					},
				},
			},
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	// hostPort should NOT appear as a removed field (- hostPort: 80)
	// because it's an ECS-assigned default
	if strings.Contains(output, "- ") && strings.Contains(output, "hostPort") {
		t.Errorf("hostPort should not appear as removed when it's an ECS-assigned default, got: %s", output)
	}
}

func TestRenderer_ECSAssignedDefaultsShownWhenInDesired(t *testing.T) {
	var buf bytes.Buffer
	renderer := NewRenderer(&buf, true)

	// Test that ECS-assigned defaults ARE shown when they're explicitly in the desired config
	entries := []DiffEntry{
		{
			Type:     DiffTypeUpdate,
			Name:     "web-task",
			Resource: "TaskDefinition",
			Current: map[string]interface{}{
				"family": "web-task",
				"portMappings": []interface{}{
					map[string]interface{}{
						"containerPort": 80,
						"hostPort":      80,
						"protocol":      "tcp",
					},
				},
			},
			Desired: map[string]interface{}{
				"family": "web-task",
				"portMappings": []interface{}{
					map[string]interface{}{
						"containerPort": 80,
						"hostPort":      8080, // Explicitly specified different value
						"protocol":      "tcp",
					},
				},
			},
		},
	}

	renderer.RenderDiff(entries)

	output := buf.String()

	// hostPort SHOULD appear because it's explicitly in desired with a different value
	if !strings.Contains(output, "hostPort") {
		t.Errorf("hostPort should appear when explicitly specified in desired, got: %s", output)
	}

	// Should show the change from 80 to 8080
	if !strings.Contains(output, "8080") {
		t.Errorf("expected new hostPort value 8080, got: %s", output)
	}
}
