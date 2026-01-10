package engine

import (
	"testing"

	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/resources"
)

func TestDependencyGraph_TopologicalSort_Simple(t *testing.T) {
	graph := NewDependencyGraph()

	graph.AddNode("a", nil)
	graph.AddNode("b", nil)
	graph.AddNode("c", nil)

	if err := graph.AddEdge("a", "b"); err != nil {
		t.Fatalf("failed to add edge a->b: %v", err)
	}
	if err := graph.AddEdge("b", "c"); err != nil {
		t.Fatalf("failed to add edge b->c: %v", err)
	}

	levels, err := graph.TopologicalSort()
	if err != nil {
		t.Fatalf("topological sort failed: %v", err)
	}

	if len(levels) != 3 {
		t.Errorf("expected 3 levels, got %d", len(levels))
	}

	if len(levels) >= 3 {
		if levels[0][0] != "a" {
			t.Errorf("expected level 0 to contain 'a', got %v", levels[0])
		}
		if levels[1][0] != "b" {
			t.Errorf("expected level 1 to contain 'b', got %v", levels[1])
		}
		if levels[2][0] != "c" {
			t.Errorf("expected level 2 to contain 'c', got %v", levels[2])
		}
	}
}

func TestDependencyGraph_TopologicalSort_Parallel(t *testing.T) {
	graph := NewDependencyGraph()

	graph.AddNode("php", nil)
	graph.AddNode("nginx", nil)
	graph.AddNode("worker", nil)

	if err := graph.AddEdge("php", "nginx"); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}
	if err := graph.AddEdge("php", "worker"); err != nil {
		t.Fatalf("failed to add edge: %v", err)
	}

	levels, err := graph.TopologicalSort()
	if err != nil {
		t.Fatalf("topological sort failed: %v", err)
	}

	if len(levels) != 2 {
		t.Errorf("expected 2 levels, got %d: %v", len(levels), levels)
	}

	if len(levels) >= 2 {
		if levels[0][0] != "php" {
			t.Errorf("expected level 0 to contain 'php', got %v", levels[0])
		}

		if len(levels[1]) != 2 {
			t.Errorf("expected level 1 to have 2 nodes, got %d", len(levels[1]))
		}
	}
}

func TestDependencyGraph_TopologicalSort_CircularDependency(t *testing.T) {
	graph := NewDependencyGraph()

	graph.AddNode("a", nil)
	graph.AddNode("b", nil)
	graph.AddNode("c", nil)

	graph.AddEdge("a", "b")
	graph.AddEdge("b", "c")
	graph.AddEdge("c", "a")

	_, err := graph.TopologicalSort()
	if err == nil {
		t.Error("expected circular dependency error, got nil")
	}
}

func TestDependencyGraph_TopologicalSort_NoDependencies(t *testing.T) {
	graph := NewDependencyGraph()

	graph.AddNode("a", nil)
	graph.AddNode("b", nil)
	graph.AddNode("c", nil)

	levels, err := graph.TopologicalSort()
	if err != nil {
		t.Fatalf("topological sort failed: %v", err)
	}

	if len(levels) != 1 {
		t.Errorf("expected 1 level (all parallel), got %d", len(levels))
	}

	if len(levels) >= 1 && len(levels[0]) != 3 {
		t.Errorf("expected all 3 nodes in level 0, got %d", len(levels[0]))
	}
}

func TestBuildServiceGraph(t *testing.T) {
	state := &resources.DesiredState{
		Services: map[string]*resources.ServiceResource{
			"php": {
				Name: "php",
				Desired: &config.Service{
					Name:      "php",
					DependsOn: []string{},
				},
			},
			"nginx": {
				Name: "nginx",
				Desired: &config.Service{
					Name:      "nginx",
					DependsOn: []string{"php"},
				},
			},
			"worker": {
				Name: "worker",
				Desired: &config.Service{
					Name:      "worker",
					DependsOn: []string{"php"},
				},
			},
		},
	}

	graph, err := BuildServiceGraph(state)
	if err != nil {
		t.Fatalf("failed to build service graph: %v", err)
	}

	levels, err := graph.TopologicalSort()
	if err != nil {
		t.Fatalf("topological sort failed: %v", err)
	}

	if len(levels) != 2 {
		t.Errorf("expected 2 levels, got %d: %v", len(levels), levels)
	}

	if len(levels) >= 1 && levels[0][0] != "php" {
		t.Errorf("expected 'php' in level 0, got %v", levels[0])
	}
}

func TestBuildServiceGraph_InvalidDependency(t *testing.T) {
	state := &resources.DesiredState{
		Services: map[string]*resources.ServiceResource{
			"nginx": {
				Name: "nginx",
				Desired: &config.Service{
					Name:      "nginx",
					DependsOn: []string{"nonexistent"},
				},
			},
		},
	}

	_, err := BuildServiceGraph(state)
	if err == nil {
		t.Error("expected error for invalid dependency, got nil")
	}
}

func TestBuildExecutionPlan(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: map[string]*resources.TaskDefResource{
			"php": {
				Name:   "php",
				Type:   "managed",
				Action: resources.TaskDefActionCreate,
			},
		},
		Services: map[string]*resources.ServiceResource{
			"web": {
				Name: "web",
				Desired: &config.Service{
					Name: "web",
				},
				Action: resources.ServiceActionCreate,
			},
		},
		ScheduledTasks: map[string]*resources.ScheduledTaskResource{},
	}

	plan, err := BuildExecutionPlan(state)
	if err != nil {
		t.Fatalf("failed to build execution plan: %v", err)
	}

	if len(plan.TaskDefs) != 1 {
		t.Errorf("expected 1 task def, got %d", len(plan.TaskDefs))
	}

	if len(plan.ServiceLevels) != 1 {
		t.Errorf("expected 1 service level, got %d", len(plan.ServiceLevels))
	}
}

// Phase 1: Tests for generalized node structure

func TestDependencyGraph_AddNodeWithType(t *testing.T) {
	tests := []struct {
		name     string
		nodeType string
		nodeName string
	}{
		{"task definition", "TaskDef", "web"},
		{"service", "Service", "api"},
		{"target group", "TargetGroup", "app-r1"},
		{"listener rule", "ListenerRule", "priority-1"},
		{"scheduled task", "ScheduledTask", "cron"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewDependencyGraph()
			nodeID := tt.nodeType + "/" + tt.nodeName

			g.AddNodeWithType(nodeID, tt.nodeType, tt.nodeName, nil)

			node, ok := g.GetNode(nodeID)
			if !ok {
				t.Fatalf("node %s not found", nodeID)
			}
			if node.Type != tt.nodeType {
				t.Errorf("expected type %s, got %s", tt.nodeType, node.Type)
			}
			if node.Name != tt.nodeName {
				t.Errorf("expected name %s, got %s", tt.nodeName, node.Name)
			}
		})
	}
}

func TestDependencyGraph_ReverseEdges(t *testing.T) {
	g := NewDependencyGraph()

	g.AddNodeWithType("TaskDef/web", "TaskDef", "web", nil)
	g.AddNodeWithType("Service/api", "Service", "api", nil)
	g.AddNodeWithType("Service/worker", "Service", "worker", nil)

	// Both services depend on task def
	g.AddEdge("TaskDef/web", "Service/api")
	g.AddEdge("TaskDef/web", "Service/worker")

	// Check dependents (reverse lookup)
	dependents := g.GetDependents("TaskDef/web")
	if len(dependents) != 2 {
		t.Errorf("expected 2 dependents, got %d", len(dependents))
	}

	// Check dependencies (forward lookup)
	deps := g.GetDependencies("Service/api")
	if len(deps) != 1 || deps[0] != "TaskDef/web" {
		t.Errorf("expected dependency on TaskDef/web, got %v", deps)
	}
}

func TestDependencyGraph_GetDependencies_Empty(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNodeWithType("Service/api", "Service", "api", nil)

	deps := g.GetDependencies("Service/api")
	if len(deps) != 0 {
		t.Errorf("expected no dependencies, got %v", deps)
	}
}

func TestDependencyGraph_GetDependents_Empty(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNodeWithType("Service/api", "Service", "api", nil)

	dependents := g.GetDependents("Service/api")
	if len(dependents) != 0 {
		t.Errorf("expected no dependents, got %v", dependents)
	}
}

func TestDependencyGraph_NodeAction(t *testing.T) {
	g := NewDependencyGraph()
	g.AddNodeWithType("TaskDef/web", "TaskDef", "web", nil)

	node, _ := g.GetNode("TaskDef/web")
	node.Action = "CREATE"

	if node.Action != "CREATE" {
		t.Errorf("expected action CREATE, got %s", node.Action)
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// Phase 2: Tests for BuildResourceGraph

func TestBuildResourceGraph_AllResourceTypes(t *testing.T) {
	tgArn := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/api-tg/abc"
	state := &resources.DesiredState{
		TaskDefs: map[string]*resources.TaskDefResource{
			"web": {Name: "web", Action: resources.TaskDefActionCreate},
		},
		Services: map[string]*resources.ServiceResource{
			"api": {
				Name: "api",
				Desired: &config.Service{
					Name:           "api",
					TaskDefinition: "web",
				},
			},
		},
		TargetGroups: map[string]*resources.TargetGroupResource{
			"api-tg": {Name: "api-tg", Arn: tgArn},
		},
		ListenerRules: []*resources.ListenerRuleResource{
			{Priority: 1, TargetGroupArn: tgArn},
		},
		ScheduledTasks: map[string]*resources.ScheduledTaskResource{
			"cron": {Name: "cron", Desired: &config.ScheduledTask{TaskDefinition: "web"}},
		},
	}

	graph, err := BuildResourceGraph(state)
	if err != nil {
		t.Fatalf("failed to build resource graph: %v", err)
	}

	// Verify all nodes exist
	expectedNodes := []string{
		"TaskDef/web",
		"Service/api",
		"TargetGroup/api-tg",
		"ListenerRule/priority-1",
		"ScheduledTask/cron",
	}
	for _, nodeID := range expectedNodes {
		if _, ok := graph.GetNode(nodeID); !ok {
			t.Errorf("node %s not found", nodeID)
		}
	}

	// Verify Service depends on TaskDef
	deps := graph.GetDependencies("Service/api")
	if !contains(deps, "TaskDef/web") {
		t.Error("Service/api should depend on TaskDef/web")
	}

	// Verify ScheduledTask depends on TaskDef
	deps = graph.GetDependencies("ScheduledTask/cron")
	if !contains(deps, "TaskDef/web") {
		t.Error("ScheduledTask/cron should depend on TaskDef/web")
	}
}

func TestBuildResourceGraph_ServiceTargetGroupRelationship(t *testing.T) {
	tgArn := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/api-tg/abc"
	state := &resources.DesiredState{
		Services: map[string]*resources.ServiceResource{
			"api": {
				Name: "api",
				Desired: &config.Service{
					Name: "api",
					LoadBalancers: []config.LoadBalancer{
						{TargetGroupArn: tgArn, ContainerName: "app", ContainerPort: 80},
					},
				},
			},
		},
		TargetGroups: map[string]*resources.TargetGroupResource{
			"api-tg": {
				Name: "api-tg",
				Arn:  tgArn,
			},
		},
	}

	graph, err := BuildResourceGraph(state)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	// Service should depend on target group
	deps := graph.GetDependencies("Service/api")
	if !contains(deps, "TargetGroup/api-tg") {
		t.Error("Service should depend on its TargetGroup")
	}
}

func TestBuildResourceGraph_ListenerRuleTargetGroupRelationship(t *testing.T) {
	tgArn := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/api-tg/abc"
	state := &resources.DesiredState{
		TargetGroups: map[string]*resources.TargetGroupResource{
			"api-tg": {Name: "api-tg", Arn: tgArn},
		},
		ListenerRules: []*resources.ListenerRuleResource{
			{Priority: 1, TargetGroupArn: tgArn},
		},
	}

	graph, err := BuildResourceGraph(state)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	// ListenerRule depends on TargetGroup
	deps := graph.GetDependencies("ListenerRule/priority-1")
	if !contains(deps, "TargetGroup/api-tg") {
		t.Error("ListenerRule should depend on TargetGroup")
	}

	// TargetGroup has ListenerRule as dependent (reverse)
	dependents := graph.GetDependents("TargetGroup/api-tg")
	if !contains(dependents, "ListenerRule/priority-1") {
		t.Error("TargetGroup should have ListenerRule as dependent")
	}
}

func TestBuildResourceGraph_ServiceDependsOnService(t *testing.T) {
	state := &resources.DesiredState{
		Services: map[string]*resources.ServiceResource{
			"php": {
				Name:    "php",
				Desired: &config.Service{Name: "php"},
			},
			"nginx": {
				Name:    "nginx",
				Desired: &config.Service{Name: "nginx", DependsOn: []string{"php"}},
			},
		},
	}

	graph, err := BuildResourceGraph(state)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	// nginx depends on php
	deps := graph.GetDependencies("Service/nginx")
	if !contains(deps, "Service/php") {
		t.Error("Service/nginx should depend on Service/php")
	}
}

func TestBuildResourceGraph_EmptyState(t *testing.T) {
	state := &resources.DesiredState{}

	graph, err := BuildResourceGraph(state)
	if err != nil {
		t.Fatalf("failed to build graph: %v", err)
	}

	if len(graph.nodes) != 0 {
		t.Errorf("expected 0 nodes, got %d", len(graph.nodes))
	}
}

// Phase 3: Tests for change propagation

func TestDependencyGraph_PropagateDelete_TargetGroup(t *testing.T) {
	tgArn := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/api-tg/abc"
	state := &resources.DesiredState{
		Services: map[string]*resources.ServiceResource{
			"api": {
				Name: "api",
				Desired: &config.Service{
					LoadBalancers: []config.LoadBalancer{
						{TargetGroupArn: tgArn, ContainerName: "app", ContainerPort: 80},
					},
				},
				Action: resources.ServiceActionNoop,
			},
		},
		TargetGroups: map[string]*resources.TargetGroupResource{
			"api-tg": {Name: "api-tg", Arn: tgArn, Action: resources.TargetGroupActionDelete},
		},
		ListenerRules: []*resources.ListenerRuleResource{
			{Priority: 1, TargetGroupArn: tgArn, Action: resources.ListenerRuleActionNoop},
		},
	}

	graph, _ := BuildResourceGraph(state)
	changes := graph.PropagateChanges()

	// Deleting target group should affect listener rule and service
	expected := map[string]string{
		"TargetGroup/api-tg":      "DELETE",
		"ListenerRule/priority-1": "DELETE",
		"Service/api":             "UPDATE",
	}

	for nodeID, expectedAction := range expected {
		change, ok := changes[nodeID]
		if !ok {
			t.Errorf("expected change for %s", nodeID)
			continue
		}
		if change.Action != expectedAction {
			t.Errorf("expected %s for %s, got %s", expectedAction, nodeID, change.Action)
		}
	}
}

func TestDependencyGraph_PropagateUpdate_TaskDefinition(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: map[string]*resources.TaskDefResource{
			"web": {Name: "web", Action: resources.TaskDefActionUpdate},
		},
		Services: map[string]*resources.ServiceResource{
			"api":    {Name: "api", Desired: &config.Service{TaskDefinition: "web"}, Action: resources.ServiceActionNoop},
			"worker": {Name: "worker", Desired: &config.Service{TaskDefinition: "web"}, Action: resources.ServiceActionNoop},
		},
		ScheduledTasks: map[string]*resources.ScheduledTaskResource{
			"cron": {Name: "cron", Desired: &config.ScheduledTask{TaskDefinition: "web"}, Action: resources.ScheduledTaskActionNoop},
		},
	}

	graph, _ := BuildResourceGraph(state)
	changes := graph.PropagateChanges()

	// TaskDef update should propagate to all dependent services and scheduled tasks
	if changes["Service/api"].Action != "UPDATE" {
		t.Error("Service/api should be updated when TaskDef changes")
	}
	if changes["Service/worker"].Action != "UPDATE" {
		t.Error("Service/worker should be updated when TaskDef changes")
	}
	if changes["ScheduledTask/cron"].Action != "UPDATE" {
		t.Error("ScheduledTask/cron should be updated when TaskDef changes")
	}
}

func TestDependencyGraph_PropagateRecreate_TargetGroup(t *testing.T) {
	tgArn := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/api-tg/abc"
	state := &resources.DesiredState{
		TargetGroups: map[string]*resources.TargetGroupResource{
			"api-tg": {Name: "api-tg", Arn: tgArn, Action: resources.TargetGroupActionRecreate},
		},
		ListenerRules: []*resources.ListenerRuleResource{
			{Priority: 1, TargetGroupArn: tgArn, Action: resources.ListenerRuleActionNoop},
		},
	}

	graph, _ := BuildResourceGraph(state)
	changes := graph.PropagateChanges()

	// Recreating TG requires updating listener rules (to point to new ARN)
	ruleChange := changes["ListenerRule/priority-1"]
	if ruleChange == nil {
		t.Fatal("expected change for ListenerRule/priority-1")
	}
	if ruleChange.Action != "UPDATE" {
		t.Errorf("ListenerRule should be updated when TargetGroup is recreated, got %s", ruleChange.Action)
	}
	if ruleChange.Reason != "TargetGroup recreated" {
		t.Errorf("wrong reason: %s", ruleChange.Reason)
	}
}

func TestDependencyGraph_DeletionOrder(t *testing.T) {
	tgArn := "arn:aws:elasticloadbalancing:us-east-1:123:targetgroup/api-tg/abc"
	state := &resources.DesiredState{
		TargetGroups: map[string]*resources.TargetGroupResource{
			"api-tg": {Name: "api-tg", Arn: tgArn, Action: resources.TargetGroupActionDelete},
		},
		ListenerRules: []*resources.ListenerRuleResource{
			{Priority: 1, TargetGroupArn: tgArn, Action: resources.ListenerRuleActionDelete},
		},
	}

	graph, _ := BuildResourceGraph(state)
	order := graph.DeletionOrder()

	// ListenerRule must be deleted before TargetGroup
	ruleIdx := -1
	tgIdx := -1
	for i, nodeID := range order {
		if nodeID == "ListenerRule/priority-1" {
			ruleIdx = i
		}
		if nodeID == "TargetGroup/api-tg" {
			tgIdx = i
		}
	}

	if ruleIdx == -1 {
		t.Error("ListenerRule not found in deletion order")
	}
	if tgIdx == -1 {
		t.Error("TargetGroup not found in deletion order")
	}
	if ruleIdx > tgIdx {
		t.Error("ListenerRule must be deleted before TargetGroup")
	}
}

func TestDependencyGraph_NoChangesPropagation(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: map[string]*resources.TaskDefResource{
			"web": {Name: "web", Action: resources.TaskDefActionNoop},
		},
		Services: map[string]*resources.ServiceResource{
			"api": {Name: "api", Desired: &config.Service{TaskDefinition: "web"}, Action: resources.ServiceActionNoop},
		},
	}

	graph, _ := BuildResourceGraph(state)
	changes := graph.PropagateChanges()

	if len(changes) != 0 {
		t.Errorf("expected no changes, got %d", len(changes))
	}
}
