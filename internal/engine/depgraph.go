package engine

import (
	"fmt"
	"sort"

	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
	"github.com/qdo/ecsmate/internal/resources"
)

type DependencyGraph struct {
	nodes        map[string]*Node
	edges        map[string][]string // dependencies: what does X depend on?
	reverseEdges map[string][]string // dependents: what depends on X?
	resolved     []string
}

type Node struct {
	ID       string // Unique identifier: "Type/Name" e.g., "Service/web", "TaskDef/api"
	Type     string // TaskDef, Service, TargetGroup, ListenerRule, ScheduledTask
	Name     string
	Resource interface{} // The actual resource (*ServiceResource, *TaskDefResource, etc.)
	Action   string      // CREATE, UPDATE, DELETE, RECREATE, NOOP
	InDegree int
}

// ServiceResource returns the resource as *ServiceResource if applicable
func (n *Node) ServiceResource() *resources.ServiceResource {
	if svc, ok := n.Resource.(*resources.ServiceResource); ok {
		return svc
	}
	return nil
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes:        make(map[string]*Node),
		edges:        make(map[string][]string),
		reverseEdges: make(map[string][]string),
	}
}

// AddNode adds a service node (legacy method for backwards compatibility)
func (g *DependencyGraph) AddNode(name string, resource *resources.ServiceResource) {
	g.nodes[name] = &Node{
		ID:       name,
		Type:     "Service",
		Name:     name,
		Resource: resource,
		InDegree: 0,
	}
}

// AddNodeWithType adds a node with explicit type information
func (g *DependencyGraph) AddNodeWithType(id, nodeType, name string, resource interface{}) {
	g.nodes[id] = &Node{
		ID:       id,
		Type:     nodeType,
		Name:     name,
		Resource: resource,
		InDegree: 0,
	}
}

func (g *DependencyGraph) AddEdge(from, to string) error {
	if _, ok := g.nodes[from]; !ok {
		return fmt.Errorf("unknown node: %s", from)
	}
	if _, ok := g.nodes[to]; !ok {
		return fmt.Errorf("unknown node: %s", to)
	}

	g.edges[from] = append(g.edges[from], to)
	g.reverseEdges[to] = append(g.reverseEdges[to], from)
	g.nodes[to].InDegree++
	return nil
}

// GetDependents returns nodes that depend on the given node (reverse lookup)
func (g *DependencyGraph) GetDependents(nodeID string) []string {
	return g.edges[nodeID]
}

// GetDependencies returns nodes that the given node depends on (forward lookup)
func (g *DependencyGraph) GetDependencies(nodeID string) []string {
	return g.reverseEdges[nodeID]
}

// Change represents a propagated change to a resource
type Change struct {
	NodeID string
	Action string
	Reason string
}

// PropagateChanges propagates changes through the dependency graph
func (g *DependencyGraph) PropagateChanges() map[string]*Change {
	changes := make(map[string]*Change)

	// First pass: collect direct changes from node actions
	for nodeID, node := range g.nodes {
		action := g.getNodeAction(node)
		if action != "" && action != "NOOP" {
			changes[nodeID] = &Change{
				NodeID: nodeID,
				Action: action,
			}
		}
	}

	// Second pass: propagate to dependents
	processed := make(map[string]bool)
	for nodeID, change := range changes {
		g.propagateToDependent(nodeID, change, changes, processed)
	}

	return changes
}

// getNodeAction extracts the action string from a node's resource
func (g *DependencyGraph) getNodeAction(node *Node) string {
	if node.Action != "" {
		return node.Action
	}

	switch r := node.Resource.(type) {
	case *resources.TaskDefResource:
		return string(r.Action)
	case *resources.ServiceResource:
		return string(r.Action)
	case *resources.TargetGroupResource:
		return string(r.Action)
	case *resources.ListenerRuleResource:
		return string(r.Action)
	case *resources.ScheduledTaskResource:
		return string(r.Action)
	}
	return ""
}

func (g *DependencyGraph) propagateToDependent(nodeID string, change *Change, changes map[string]*Change, processed map[string]bool) {
	if processed[nodeID] {
		return
	}
	processed[nodeID] = true

	dependents := g.GetDependents(nodeID)
	node, _ := g.GetNode(nodeID)

	for _, depID := range dependents {
		if _, exists := changes[depID]; exists {
			continue
		}

		depNode, _ := g.GetNode(depID)
		if depNode == nil {
			continue
		}

		switch change.Action {
		case "DELETE":
			// Dependent loses its dependency - delete if ListenerRule, update otherwise
			action := "UPDATE"
			if depNode.Type == "ListenerRule" {
				action = "DELETE"
			}
			changes[depID] = &Change{
				NodeID: depID,
				Action: action,
				Reason: fmt.Sprintf("dependency %s deleted", nodeID),
			}
		case "RECREATE":
			changes[depID] = &Change{
				NodeID: depID,
				Action: "UPDATE",
				Reason: fmt.Sprintf("%s recreated", node.Type),
			}
		case "UPDATE", "CREATE":
			// TaskDef updates propagate to services and scheduled tasks
			if node.Type == "TaskDef" {
				changes[depID] = &Change{
					NodeID: depID,
					Action: "UPDATE",
					Reason: "task definition updated",
				}
			}
		}

		// Recursively propagate
		if c, ok := changes[depID]; ok {
			g.propagateToDependent(depID, c, changes, processed)
		}
	}
}

// DeletionOrder returns nodes in reverse topological order for safe deletion
func (g *DependencyGraph) DeletionOrder() []string {
	levels, err := g.TopologicalSort()
	if err != nil {
		return nil
	}

	var order []string
	for i := len(levels) - 1; i >= 0; i-- {
		order = append(order, levels[i]...)
	}
	return order
}

func (g *DependencyGraph) TopologicalSort() ([][]string, error) {
	inDegree := make(map[string]int)
	for name, node := range g.nodes {
		inDegree[name] = node.InDegree
	}

	var levels [][]string
	remaining := len(g.nodes)

	for remaining > 0 {
		var level []string
		for name, degree := range inDegree {
			if degree == 0 {
				level = append(level, name)
			}
		}

		if len(level) == 0 {
			return nil, fmt.Errorf("circular dependency detected")
		}

		sort.Strings(level)
		levels = append(levels, level)

		for _, name := range level {
			delete(inDegree, name)
			remaining--

			for _, dependent := range g.edges[name] {
				if _, ok := inDegree[dependent]; ok {
					inDegree[dependent]--
				}
			}
		}
	}

	return levels, nil
}

func (g *DependencyGraph) GetNode(name string) (*Node, bool) {
	node, ok := g.nodes[name]
	return node, ok
}

// BuildResourceGraph creates a unified graph of all resource types and their dependencies
func BuildResourceGraph(state *resources.DesiredState) (*DependencyGraph, error) {
	graph := NewDependencyGraph()

	if state == nil {
		return graph, nil
	}

	// Add all TaskDefs first (they have no dependencies)
	for name, td := range state.TaskDefs {
		nodeID := "TaskDef/" + name
		graph.AddNodeWithType(nodeID, "TaskDef", name, td)
	}

	// Add all TargetGroups (they may be referenced by services and listener rules)
	for name, tg := range state.TargetGroups {
		nodeID := "TargetGroup/" + name
		graph.AddNodeWithType(nodeID, "TargetGroup", name, tg)
	}

	// Add all Services with dependencies
	for name, svc := range state.Services {
		nodeID := "Service/" + name
		graph.AddNodeWithType(nodeID, "Service", name, svc)
	}

	// Add Service → TaskDef dependencies
	for name, svc := range state.Services {
		if svc.Desired == nil {
			continue
		}

		serviceID := "Service/" + name

		// Service depends on its task definition
		if svc.Desired.TaskDefinition != "" {
			tdID := "TaskDef/" + svc.Desired.TaskDefinition
			if _, ok := graph.GetNode(tdID); ok {
				if err := graph.AddEdge(tdID, serviceID); err != nil {
					log.Debug("failed to add TaskDef edge", "error", err)
				}
			}
		}

		// Service → Service dependencies (DependsOn)
		for _, dep := range svc.Desired.DependsOn {
			depID := "Service/" + dep
			if _, ok := graph.GetNode(depID); ok {
				if err := graph.AddEdge(depID, serviceID); err != nil {
					log.Debug("failed to add Service dependency edge", "error", err)
				}
			}
		}

		// Service → TargetGroup dependencies (via LoadBalancers)
		for _, lb := range svc.Desired.LoadBalancers {
			for tgName, tg := range state.TargetGroups {
				if tg.Arn == lb.TargetGroupArn {
					tgID := "TargetGroup/" + tgName
					if err := graph.AddEdge(tgID, serviceID); err != nil {
						log.Debug("failed to add TargetGroup edge", "error", err)
					}
					break
				}
			}
		}
	}

	// Add ListenerRules with TargetGroup dependency
	for _, rule := range state.ListenerRules {
		ruleID := fmt.Sprintf("ListenerRule/priority-%d", rule.Priority)
		graph.AddNodeWithType(ruleID, "ListenerRule", fmt.Sprintf("priority-%d", rule.Priority), rule)

		// ListenerRule depends on TargetGroup
		if rule.TargetGroupArn != "" {
			for tgName, tg := range state.TargetGroups {
				if tg.Arn == rule.TargetGroupArn {
					tgID := "TargetGroup/" + tgName
					if err := graph.AddEdge(tgID, ruleID); err != nil {
						log.Debug("failed to add ListenerRule -> TargetGroup edge", "error", err)
					}
					break
				}
			}
		}
	}

	// Add ScheduledTasks with TaskDef dependency
	for name, task := range state.ScheduledTasks {
		nodeID := "ScheduledTask/" + name
		graph.AddNodeWithType(nodeID, "ScheduledTask", name, task)

		if task.Desired != nil && task.Desired.TaskDefinition != "" {
			tdID := "TaskDef/" + task.Desired.TaskDefinition
			if _, ok := graph.GetNode(tdID); ok {
				if err := graph.AddEdge(tdID, nodeID); err != nil {
					log.Debug("failed to add ScheduledTask -> TaskDef edge", "error", err)
				}
			}
		}
	}

	return graph, nil
}

func BuildServiceGraph(state *resources.DesiredState) (*DependencyGraph, error) {
	graph := NewDependencyGraph()

	for name, svc := range state.Services {
		graph.AddNode(name, svc)
	}

	for name, svc := range state.Services {
		if svc.Desired == nil {
			continue
		}

		for _, dep := range svc.Desired.DependsOn {
			log.Debug("adding dependency edge", "from", dep, "to", name)
			if err := graph.AddEdge(dep, name); err != nil {
				return nil, fmt.Errorf("invalid dependency %s -> %s: %w", dep, name, err)
			}
		}
	}

	return graph, nil
}

type ExecutionPlan struct {
	Manifest       *config.Manifest
	TaskDefs       []*resources.TaskDefResource
	ServiceLevels  [][]string
	ScheduledTasks []*resources.ScheduledTaskResource
	Graph          *DependencyGraph
}

func BuildExecutionPlan(state *resources.DesiredState) (*ExecutionPlan, error) {
	plan := &ExecutionPlan{
		Manifest:       state.Manifest,
		TaskDefs:       make([]*resources.TaskDefResource, 0),
		ScheduledTasks: make([]*resources.ScheduledTaskResource, 0),
	}

	for _, td := range state.TaskDefs {
		plan.TaskDefs = append(plan.TaskDefs, td)
	}

	graph, err := BuildServiceGraph(state)
	if err != nil {
		return nil, err
	}
	plan.Graph = graph

	levels, err := graph.TopologicalSort()
	if err != nil {
		return nil, err
	}
	plan.ServiceLevels = levels

	for _, task := range state.ScheduledTasks {
		plan.ScheduledTasks = append(plan.ScheduledTasks, task)
	}

	log.Debug("built execution plan",
		"taskDefs", len(plan.TaskDefs),
		"serviceLevels", len(plan.ServiceLevels),
		"scheduledTasks", len(plan.ScheduledTasks))

	return plan, nil
}
