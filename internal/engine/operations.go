package engine

import (
	"fmt"
	"sort"
)

// OperationType defines the type of operation to perform
type OperationType string

const (
	OpCreate   OperationType = "CREATE"
	OpUpdate   OperationType = "UPDATE"
	OpDelete   OperationType = "DELETE"
	OpRecreate OperationType = "RECREATE"
	OpNoop     OperationType = "NOOP"
)

// Operation represents a single operation in the execution plan
type Operation struct {
	ID           string
	Type         OperationType
	ResourceType string
	ResourceName string
	DependsOn    []string
	Reason       string
	SubOps       []*Operation // For RECREATE: contains DELETE and CREATE operations
}

// OperationGraph manages operation dependencies and execution order
type OperationGraph struct {
	operations map[string]*Operation
	order      []string
}

// NewOperationGraph creates a new operation graph
func NewOperationGraph() *OperationGraph {
	return &OperationGraph{
		operations: make(map[string]*Operation),
		order:      make([]string, 0),
	}
}

// AddOperation adds an operation to the graph
func (g *OperationGraph) AddOperation(op *Operation) {
	if op.ID == "" {
		op.ID = fmt.Sprintf("%s/%s", op.ResourceType, op.ResourceName)
	}
	g.operations[op.ID] = op
	g.order = append(g.order, op.ID)
}

// GetOperation retrieves an operation by ID
func (g *OperationGraph) GetOperation(id string) (*Operation, bool) {
	op, ok := g.operations[id]
	return op, ok
}

// GetExecutionOrder returns operations in dependency-respecting order
// Operations are grouped into levels where each level can be executed in parallel
func (g *OperationGraph) GetExecutionOrder() ([][]string, error) {
	if len(g.operations) == 0 {
		return nil, nil
	}

	inDegree := make(map[string]int)
	for id := range g.operations {
		inDegree[id] = 0
	}

	for _, op := range g.operations {
		for _, dep := range op.DependsOn {
			if _, exists := g.operations[dep]; exists {
				inDegree[op.ID]++
			}
		}
	}

	var levels [][]string
	remaining := len(g.operations)

	for remaining > 0 {
		var level []string
		for id, degree := range inDegree {
			if degree == 0 {
				level = append(level, id)
			}
		}

		if len(level) == 0 && remaining > 0 {
			return nil, fmt.Errorf("circular dependency detected in operation graph")
		}

		sort.Strings(level)
		levels = append(levels, level)

		for _, id := range level {
			delete(inDegree, id)
			remaining--

			for depID, op := range g.operations {
				for _, dep := range op.DependsOn {
					if dep == id {
						if _, ok := inDegree[depID]; ok {
							inDegree[depID]--
						}
					}
				}
			}
		}
	}

	return levels, nil
}

// Operations returns all operations
func (g *OperationGraph) Operations() map[string]*Operation {
	return g.operations
}

// HasChanges returns true if there are any non-NOOP operations
func (g *OperationGraph) HasChanges() bool {
	for _, op := range g.operations {
		if op.Type != OpNoop {
			return true
		}
	}
	return false
}

// Summary returns a count of operations by type
func (g *OperationGraph) Summary() OperationSummary {
	summary := OperationSummary{}
	for _, op := range g.operations {
		switch op.Type {
		case OpCreate:
			summary.Creates++
		case OpUpdate:
			summary.Updates++
		case OpDelete:
			summary.Deletes++
		case OpRecreate:
			summary.Recreates++
		case OpNoop:
			summary.Noops++
		}
	}
	return summary
}

// OperationSummary holds counts of operations by type
type OperationSummary struct {
	Creates   int
	Updates   int
	Deletes   int
	Recreates int
	Noops     int
}

// ExpandRecreateOperations expands RECREATE operations into DELETE + CREATE pairs
// This is useful for execution where DELETE must complete before CREATE
func (g *OperationGraph) ExpandRecreateOperations() *OperationGraph {
	expanded := NewOperationGraph()

	for _, op := range g.operations {
		if op.Type == OpRecreate {
			deleteOp := &Operation{
				ID:           op.ID + "/delete",
				Type:         OpDelete,
				ResourceType: op.ResourceType,
				ResourceName: op.ResourceName,
				DependsOn:    op.DependsOn,
				Reason:       op.Reason,
			}
			expanded.AddOperation(deleteOp)

			createOp := &Operation{
				ID:           op.ID + "/create",
				Type:         OpCreate,
				ResourceType: op.ResourceType,
				ResourceName: op.ResourceName,
				DependsOn:    []string{deleteOp.ID},
				Reason:       op.Reason,
			}
			expanded.AddOperation(createOp)
		} else {
			opCopy := *op
			expanded.AddOperation(&opCopy)
		}
	}

	return expanded
}
