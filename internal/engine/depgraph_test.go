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
