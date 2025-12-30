package engine

import (
	"fmt"
	"sort"

	"github.com/qdo/ecsmate/internal/log"
	"github.com/qdo/ecsmate/internal/resources"
)

type DependencyGraph struct {
	nodes    map[string]*Node
	edges    map[string][]string
	resolved []string
}

type Node struct {
	Name     string
	Resource *resources.ServiceResource
	InDegree int
}

func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes: make(map[string]*Node),
		edges: make(map[string][]string),
	}
}

func (g *DependencyGraph) AddNode(name string, resource *resources.ServiceResource) {
	g.nodes[name] = &Node{
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
	g.nodes[to].InDegree++
	return nil
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
	TaskDefs       []*resources.TaskDefResource
	ServiceLevels  [][]string
	ScheduledTasks []*resources.ScheduledTaskResource
	Graph          *DependencyGraph
}

func BuildExecutionPlan(state *resources.DesiredState) (*ExecutionPlan, error) {
	plan := &ExecutionPlan{
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
