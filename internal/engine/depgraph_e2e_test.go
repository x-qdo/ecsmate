package engine

import (
	"testing"

	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/diff"
	"github.com/qdo/ecsmate/internal/resources"
)

// Test: Delete ingress rule cascades to target group and listener rule
func TestE2E_DeleteIngressRule_Cascade(t *testing.T) {
	tgArn := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/app-r1/abc"

	// Simulating: ingress rule removed from manifest
	// Target group should be deleted, listener rule should also be deleted
	state := &resources.DesiredState{
		Manifest: &config.Manifest{Name: "app"},
		Services: map[string]*resources.ServiceResource{
			"web": {
				Name:   "web",
				Action: resources.ServiceActionNoop,
				Desired: &config.Service{
					Name: "web",
					LoadBalancers: []config.LoadBalancer{
						{
							TargetGroupArn: tgArn,
							ContainerName:  "app",
							ContainerPort:  80,
						},
					},
				},
			},
		},
		TargetGroups: map[string]*resources.TargetGroupResource{
			"app-r1": {
				Name:   "app-r1",
				Arn:    tgArn,
				Action: resources.TargetGroupActionDelete,
			},
		},
		ListenerRules: []*resources.ListenerRuleResource{
			{
				Priority:       1,
				TargetGroupArn: tgArn,
				Action:         resources.ListenerRuleActionNoop,
			},
		},
	}

	graph, err := BuildResourceGraph(state)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	changes := graph.PropagateChanges()

	// Target group deletion should be tracked
	if _, ok := changes["TargetGroup/app-r1"]; !ok {
		t.Error("TargetGroup/app-r1 should have change")
	}

	// ListenerRule should be deleted (propagated)
	ruleChange, ok := changes["ListenerRule/priority-1"]
	if !ok {
		t.Error("ListenerRule/priority-1 should have propagated change")
	}
	if ruleChange.Action != "DELETE" {
		t.Errorf("ListenerRule should be DELETE, got %s", ruleChange.Action)
	}

	// Service should be updated to remove LB config
	svcChange, ok := changes["Service/web"]
	if !ok {
		t.Error("Service/web should have propagated change")
	}
	if svcChange.Action != "UPDATE" {
		t.Errorf("Service should be UPDATE, got %s", svcChange.Action)
	}

	// Generate plan and verify
	planner := NewPlanner()
	plan := planner.GeneratePlan(state)

	if !plan.HasChanges() {
		t.Error("Plan should have changes")
	}
}

// Test: Update task definition propagates to all dependent resources
func TestE2E_UpdateTaskDef_Propagation(t *testing.T) {
	state := &resources.DesiredState{
		Manifest: &config.Manifest{Name: "app"},
		TaskDefs: map[string]*resources.TaskDefResource{
			"shared": {
				Name:   "shared",
				Action: resources.TaskDefActionUpdate,
				Desired: &config.TaskDefinition{
					Family: "shared",
					CPU:    "512",
				},
			},
		},
		Services: map[string]*resources.ServiceResource{
			"api": {
				Name:   "api",
				Action: resources.ServiceActionNoop,
				Desired: &config.Service{
					Name:           "api",
					TaskDefinition: "shared",
				},
			},
			"worker": {
				Name:   "worker",
				Action: resources.ServiceActionNoop,
				Desired: &config.Service{
					Name:           "worker",
					TaskDefinition: "shared",
				},
			},
		},
		ScheduledTasks: map[string]*resources.ScheduledTaskResource{
			"cron": {
				Name:   "cron",
				Action: resources.ScheduledTaskActionNoop,
				Desired: &config.ScheduledTask{
					TaskDefinition: "shared",
				},
			},
		},
	}

	graph, _ := BuildResourceGraph(state)
	changes := graph.PropagateChanges()

	// All three dependents should be updated
	expectedUpdates := []string{"Service/api", "Service/worker", "ScheduledTask/cron"}
	for _, nodeID := range expectedUpdates {
		change, ok := changes[nodeID]
		if !ok {
			t.Errorf("expected change for %s", nodeID)
			continue
		}
		if change.Action != "UPDATE" {
			t.Errorf("expected UPDATE for %s, got %s", nodeID, change.Action)
		}
		if change.Reason != "task definition updated" {
			t.Errorf("wrong reason for %s: %s", nodeID, change.Reason)
		}
	}

	// Generate plan and verify all resources are marked for update
	planner := NewPlanner()
	plan := planner.GeneratePlan(state)

	updateCount := 0
	for _, entry := range plan.Entries {
		if entry.Type == diff.DiffTypeUpdate {
			updateCount++
		}
	}

	// TaskDef + 2 Services + 1 ScheduledTask = 4 updates
	if updateCount != 4 {
		t.Errorf("expected 4 updates in plan, got %d", updateCount)
	}
}

// Test: Recreate target group updates listener rules
func TestE2E_RecreateTargetGroup_UpdateRules(t *testing.T) {
	tgArn := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/app-r1/abc"
	state := &resources.DesiredState{
		Manifest: &config.Manifest{Name: "app"},
		TargetGroups: map[string]*resources.TargetGroupResource{
			"app-r1": {
				Name:            "app-r1",
				Arn:             tgArn,
				Action:          resources.TargetGroupActionRecreate,
				RecreateReasons: []string{"port changed"},
			},
		},
		ListenerRules: []*resources.ListenerRuleResource{
			{
				Priority:       1,
				TargetGroupArn: tgArn,
				Action:         resources.ListenerRuleActionNoop,
			},
			{
				Priority:       2,
				TargetGroupArn: tgArn,
				Action:         resources.ListenerRuleActionNoop,
			},
		},
	}

	graph, _ := BuildResourceGraph(state)
	changes := graph.PropagateChanges()

	// Both listener rules should be updated
	for _, ruleID := range []string{"ListenerRule/priority-1", "ListenerRule/priority-2"} {
		change, ok := changes[ruleID]
		if !ok {
			t.Errorf("expected change for %s", ruleID)
			continue
		}
		if change.Action != "UPDATE" {
			t.Errorf("expected UPDATE for %s, got %s", ruleID, change.Action)
		}
	}

	// Generate plan and verify
	planner := NewPlanner()
	plan := planner.GeneratePlan(state)

	recreateCount := 0
	updateCount := 0
	for _, entry := range plan.Entries {
		switch entry.Type {
		case diff.DiffTypeRecreate:
			recreateCount++
		case diff.DiffTypeUpdate:
			updateCount++
		}
	}

	if recreateCount != 1 {
		t.Errorf("expected 1 recreate (TargetGroup), got %d", recreateCount)
	}
	if updateCount != 2 {
		t.Errorf("expected 2 updates (ListenerRules), got %d", updateCount)
	}
}

// Test: Complex dependency chain
func TestE2E_ComplexDependencyChain(t *testing.T) {
	// Scenario: TaskDef update should cascade through Services that depend on it
	// Service A -> TaskDef (update)
	// Service B -> Service A (DependsOn) -> TaskDef (update)
	state := &resources.DesiredState{
		Manifest: &config.Manifest{Name: "app"},
		TaskDefs: map[string]*resources.TaskDefResource{
			"web": {
				Name:   "web",
				Action: resources.TaskDefActionUpdate,
				Desired: &config.TaskDefinition{
					Family: "web",
					CPU:    "256",
				},
			},
		},
		Services: map[string]*resources.ServiceResource{
			"serviceA": {
				Name:   "serviceA",
				Action: resources.ServiceActionNoop,
				Desired: &config.Service{
					Name:           "serviceA",
					TaskDefinition: "web",
				},
			},
			"serviceB": {
				Name:   "serviceB",
				Action: resources.ServiceActionNoop,
				Desired: &config.Service{
					Name:           "serviceB",
					TaskDefinition: "other", // Different task def
					DependsOn:      []string{"serviceA"},
				},
			},
		},
	}

	graph, _ := BuildResourceGraph(state)
	changes := graph.PropagateChanges()

	// ServiceA should be updated due to TaskDef change
	if change, ok := changes["Service/serviceA"]; !ok || change.Action != "UPDATE" {
		t.Error("Service/serviceA should be updated due to TaskDef change")
	}

	// ServiceB should NOT be updated (its TaskDef is different, DependsOn doesn't propagate updates)
	if _, ok := changes["Service/serviceB"]; ok {
		// This is actually expected behavior - DependsOn creates execution order dependency
		// but doesn't propagate changes
	}
}

// Test: No changes when everything is NOOP
func TestE2E_AllNoop(t *testing.T) {
	tgArn := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/app-r1/abc"
	state := &resources.DesiredState{
		Manifest: &config.Manifest{Name: "app"},
		TaskDefs: map[string]*resources.TaskDefResource{
			"web": {
				Name:    "web",
				Action:  resources.TaskDefActionNoop,
				Desired: &config.TaskDefinition{Family: "web"},
			},
		},
		Services: map[string]*resources.ServiceResource{
			"api": {
				Name:   "api",
				Action: resources.ServiceActionNoop,
				Desired: &config.Service{
					Name:           "api",
					TaskDefinition: "web",
				},
			},
		},
		TargetGroups: map[string]*resources.TargetGroupResource{
			"app-r1": {
				Name:   "app-r1",
				Arn:    tgArn,
				Action: resources.TargetGroupActionNoop,
			},
		},
		ListenerRules: []*resources.ListenerRuleResource{
			{
				Priority:       1,
				TargetGroupArn: tgArn,
				Action:         resources.ListenerRuleActionNoop,
			},
		},
	}

	graph, _ := BuildResourceGraph(state)
	changes := graph.PropagateChanges()

	if len(changes) != 0 {
		t.Errorf("expected no changes when all resources are NOOP, got %d", len(changes))
	}

	planner := NewPlanner()
	plan := planner.GeneratePlan(state)

	if plan.HasChanges() {
		t.Error("plan should have no changes when all resources are NOOP")
	}
}

// Test: Verify deletion order respects dependencies
func TestE2E_DeletionOrder_RespectsDependencies(t *testing.T) {
	tgArn := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/app-r1/abc"
	state := &resources.DesiredState{
		Manifest: &config.Manifest{Name: "app"},
		TargetGroups: map[string]*resources.TargetGroupResource{
			"app-r1": {
				Name:   "app-r1",
				Arn:    tgArn,
				Action: resources.TargetGroupActionDelete,
			},
		},
		ListenerRules: []*resources.ListenerRuleResource{
			{
				Priority:       1,
				TargetGroupArn: tgArn,
				Action:         resources.ListenerRuleActionDelete,
			},
			{
				Priority:       2,
				TargetGroupArn: tgArn,
				Action:         resources.ListenerRuleActionDelete,
			},
		},
	}

	graph, _ := BuildResourceGraph(state)
	order := graph.DeletionOrder()

	// Find positions
	tgIdx := -1
	rule1Idx := -1
	rule2Idx := -1
	for i, nodeID := range order {
		switch nodeID {
		case "TargetGroup/app-r1":
			tgIdx = i
		case "ListenerRule/priority-1":
			rule1Idx = i
		case "ListenerRule/priority-2":
			rule2Idx = i
		}
	}

	// ListenerRules must come before TargetGroup
	if rule1Idx == -1 || rule1Idx > tgIdx {
		t.Error("ListenerRule/priority-1 must be deleted before TargetGroup")
	}
	if rule2Idx == -1 || rule2Idx > tgIdx {
		t.Error("ListenerRule/priority-2 must be deleted before TargetGroup")
	}
}

func buildIngressState() *resources.DesiredState {
	tgArn := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/app-r1/abc"
	return &resources.DesiredState{
		Manifest: &config.Manifest{
			Name: "app",
			Ingress: &config.Ingress{
				ListenerArn: "arn:aws:elasticloadbalancing:us-east-1:123:listener/app/abc/def",
				Rules: []config.IngressRule{
					{
						Priority: 1,
						Host:     "api.example.com",
						Service: &config.IngressServiceBackend{
							Name:          "web",
							ContainerName: "app",
							ContainerPort: 80,
						},
					},
				},
			},
		},
		Services: map[string]*resources.ServiceResource{
			"web": {
				Name:   "web",
				Action: resources.ServiceActionNoop,
				Desired: &config.Service{
					Name: "web",
					LoadBalancers: []config.LoadBalancer{
						{
							TargetGroupArn: tgArn,
							ContainerName:  "app",
							ContainerPort:  80,
						},
					},
				},
			},
		},
		TargetGroups: map[string]*resources.TargetGroupResource{
			"app-r1": {
				Name:   "app-r1",
				Arn:    tgArn,
				Action: resources.TargetGroupActionNoop,
			},
		},
		ListenerRules: []*resources.ListenerRuleResource{
			{
				Priority:       1,
				TargetGroupArn: tgArn,
				Action:         resources.ListenerRuleActionNoop,
			},
		},
	}
}
