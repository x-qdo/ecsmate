package engine

import (
	"reflect"
	"testing"
)

func TestOperationGraph_AddAndGetOperation(t *testing.T) {
	g := NewOperationGraph()

	op := &Operation{
		ID:           "svc/web",
		Type:         OpCreate,
		ResourceType: "Service",
		ResourceName: "web",
	}
	g.AddOperation(op)

	got, ok := g.GetOperation("svc/web")
	if !ok {
		t.Fatal("expected operation to be found")
	}
	if got.ID != "svc/web" {
		t.Errorf("expected ID 'svc/web', got %q", got.ID)
	}
}

func TestOperationGraph_AutoGenerateID(t *testing.T) {
	g := NewOperationGraph()

	op := &Operation{
		Type:         OpCreate,
		ResourceType: "Service",
		ResourceName: "web",
	}
	g.AddOperation(op)

	got, ok := g.GetOperation("Service/web")
	if !ok {
		t.Fatal("expected operation to be found with auto-generated ID")
	}
	if got.ID != "Service/web" {
		t.Errorf("expected auto-generated ID 'Service/web', got %q", got.ID)
	}
}

func TestOperationGraph_GetExecutionOrder_NoDependencies(t *testing.T) {
	g := NewOperationGraph()

	g.AddOperation(&Operation{ID: "a", Type: OpCreate, ResourceType: "Service", ResourceName: "a"})
	g.AddOperation(&Operation{ID: "b", Type: OpCreate, ResourceType: "Service", ResourceName: "b"})
	g.AddOperation(&Operation{ID: "c", Type: OpCreate, ResourceType: "Service", ResourceName: "c"})

	levels, err := g.GetExecutionOrder()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(levels) != 1 {
		t.Fatalf("expected 1 level (all parallel), got %d", len(levels))
	}

	if len(levels[0]) != 3 {
		t.Errorf("expected 3 operations in level 0, got %d", len(levels[0]))
	}
}

func TestOperationGraph_GetExecutionOrder_WithDependencies(t *testing.T) {
	g := NewOperationGraph()

	g.AddOperation(&Operation{ID: "db", Type: OpCreate, ResourceType: "Service", ResourceName: "db"})
	g.AddOperation(&Operation{ID: "api", Type: OpCreate, ResourceType: "Service", ResourceName: "api", DependsOn: []string{"db"}})
	g.AddOperation(&Operation{ID: "web", Type: OpCreate, ResourceType: "Service", ResourceName: "web", DependsOn: []string{"api"}})

	levels, err := g.GetExecutionOrder()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(levels) != 3 {
		t.Fatalf("expected 3 levels, got %d", len(levels))
	}

	// db should be in level 0
	if levels[0][0] != "db" {
		t.Errorf("expected 'db' in level 0, got %v", levels[0])
	}

	// api should be in level 1
	if levels[1][0] != "api" {
		t.Errorf("expected 'api' in level 1, got %v", levels[1])
	}

	// web should be in level 2
	if levels[2][0] != "web" {
		t.Errorf("expected 'web' in level 2, got %v", levels[2])
	}
}

func TestOperationGraph_GetExecutionOrder_ParallelWithDependencies(t *testing.T) {
	g := NewOperationGraph()

	// db has no dependencies
	g.AddOperation(&Operation{ID: "db", Type: OpCreate, ResourceType: "Service", ResourceName: "db"})
	// api and worker both depend on db, can run in parallel
	g.AddOperation(&Operation{ID: "api", Type: OpCreate, ResourceType: "Service", ResourceName: "api", DependsOn: []string{"db"}})
	g.AddOperation(&Operation{ID: "worker", Type: OpCreate, ResourceType: "Service", ResourceName: "worker", DependsOn: []string{"db"}})
	// web depends on api
	g.AddOperation(&Operation{ID: "web", Type: OpCreate, ResourceType: "Service", ResourceName: "web", DependsOn: []string{"api"}})

	levels, err := g.GetExecutionOrder()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(levels) != 3 {
		t.Fatalf("expected 3 levels, got %d", len(levels))
	}

	// Level 0: db
	if len(levels[0]) != 1 || levels[0][0] != "db" {
		t.Errorf("expected [db] in level 0, got %v", levels[0])
	}

	// Level 1: api, worker (in sorted order)
	if len(levels[1]) != 2 {
		t.Errorf("expected 2 items in level 1, got %d", len(levels[1]))
	}

	// Level 2: web
	if len(levels[2]) != 1 || levels[2][0] != "web" {
		t.Errorf("expected [web] in level 2, got %v", levels[2])
	}
}

func TestOperationGraph_GetExecutionOrder_CircularDependency(t *testing.T) {
	g := NewOperationGraph()

	g.AddOperation(&Operation{ID: "a", Type: OpCreate, ResourceType: "Service", ResourceName: "a", DependsOn: []string{"b"}})
	g.AddOperation(&Operation{ID: "b", Type: OpCreate, ResourceType: "Service", ResourceName: "b", DependsOn: []string{"a"}})

	_, err := g.GetExecutionOrder()
	if err == nil {
		t.Fatal("expected error for circular dependency")
	}
}

func TestOperationGraph_HasChanges(t *testing.T) {
	tests := []struct {
		name     string
		ops      []*Operation
		expected bool
	}{
		{
			name:     "empty graph",
			ops:      nil,
			expected: false,
		},
		{
			name: "only noop",
			ops: []*Operation{
				{ID: "a", Type: OpNoop},
			},
			expected: false,
		},
		{
			name: "has create",
			ops: []*Operation{
				{ID: "a", Type: OpCreate},
			},
			expected: true,
		},
		{
			name: "has update",
			ops: []*Operation{
				{ID: "a", Type: OpUpdate},
			},
			expected: true,
		},
		{
			name: "has delete",
			ops: []*Operation{
				{ID: "a", Type: OpDelete},
			},
			expected: true,
		},
		{
			name: "has recreate",
			ops: []*Operation{
				{ID: "a", Type: OpRecreate},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewOperationGraph()
			for _, op := range tt.ops {
				g.AddOperation(op)
			}

			if got := g.HasChanges(); got != tt.expected {
				t.Errorf("HasChanges() = %v, expected %v", got, tt.expected)
			}
		})
	}
}

func TestOperationGraph_Summary(t *testing.T) {
	g := NewOperationGraph()

	g.AddOperation(&Operation{ID: "c1", Type: OpCreate})
	g.AddOperation(&Operation{ID: "c2", Type: OpCreate})
	g.AddOperation(&Operation{ID: "u1", Type: OpUpdate})
	g.AddOperation(&Operation{ID: "d1", Type: OpDelete})
	g.AddOperation(&Operation{ID: "r1", Type: OpRecreate})
	g.AddOperation(&Operation{ID: "n1", Type: OpNoop})
	g.AddOperation(&Operation{ID: "n2", Type: OpNoop})

	summary := g.Summary()

	if summary.Creates != 2 {
		t.Errorf("expected 2 creates, got %d", summary.Creates)
	}
	if summary.Updates != 1 {
		t.Errorf("expected 1 update, got %d", summary.Updates)
	}
	if summary.Deletes != 1 {
		t.Errorf("expected 1 delete, got %d", summary.Deletes)
	}
	if summary.Recreates != 1 {
		t.Errorf("expected 1 recreate, got %d", summary.Recreates)
	}
	if summary.Noops != 2 {
		t.Errorf("expected 2 noops, got %d", summary.Noops)
	}
}

func TestOperationGraph_ExpandRecreateOperations(t *testing.T) {
	g := NewOperationGraph()

	g.AddOperation(&Operation{
		ID:           "svc/web",
		Type:         OpRecreate,
		ResourceType: "Service",
		ResourceName: "web",
		Reason:       "launchType changed",
	})
	g.AddOperation(&Operation{
		ID:           "svc/api",
		Type:         OpUpdate,
		ResourceType: "Service",
		ResourceName: "api",
	})

	expanded := g.ExpandRecreateOperations()

	// Should have 3 operations: delete, create (from recreate), and update
	ops := expanded.Operations()
	if len(ops) != 3 {
		t.Fatalf("expected 3 operations after expansion, got %d", len(ops))
	}

	// Check delete operation exists
	deleteOp, ok := expanded.GetOperation("svc/web/delete")
	if !ok {
		t.Fatal("expected delete operation from recreate expansion")
	}
	if deleteOp.Type != OpDelete {
		t.Errorf("expected delete type, got %v", deleteOp.Type)
	}

	// Check create operation exists and depends on delete
	createOp, ok := expanded.GetOperation("svc/web/create")
	if !ok {
		t.Fatal("expected create operation from recreate expansion")
	}
	if createOp.Type != OpCreate {
		t.Errorf("expected create type, got %v", createOp.Type)
	}
	if !reflect.DeepEqual(createOp.DependsOn, []string{"svc/web/delete"}) {
		t.Errorf("expected create to depend on delete, got %v", createOp.DependsOn)
	}

	// Check update operation is preserved
	updateOp, ok := expanded.GetOperation("svc/api")
	if !ok {
		t.Fatal("expected update operation to be preserved")
	}
	if updateOp.Type != OpUpdate {
		t.Errorf("expected update type, got %v", updateOp.Type)
	}
}

func TestOperationGraph_ExpandRecreateOperations_ExecutionOrder(t *testing.T) {
	g := NewOperationGraph()

	g.AddOperation(&Operation{
		ID:           "svc/web",
		Type:         OpRecreate,
		ResourceType: "Service",
		ResourceName: "web",
	})

	expanded := g.ExpandRecreateOperations()
	levels, err := expanded.GetExecutionOrder()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 2 levels: delete first, then create
	if len(levels) != 2 {
		t.Fatalf("expected 2 levels, got %d", len(levels))
	}

	if levels[0][0] != "svc/web/delete" {
		t.Errorf("expected delete in level 0, got %v", levels[0])
	}

	if levels[1][0] != "svc/web/create" {
		t.Errorf("expected create in level 1, got %v", levels[1])
	}
}
