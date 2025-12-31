package engine

import (
	"bytes"
	"testing"
	"time"

	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/resources"
)

func TestNewExecutor(t *testing.T) {
	var buf bytes.Buffer
	cfg := ExecutorConfig{
		Output:  &buf,
		NoColor: true,
		NoWait:  true,
		Timeout: 10 * time.Minute,
	}

	executor := NewExecutor(cfg)

	if executor == nil {
		t.Fatal("expected executor to be created")
	}
	if executor.noWait != true {
		t.Error("expected noWait to be true")
	}
	if executor.timeout != 10*time.Minute {
		t.Errorf("expected timeout 10m, got %v", executor.timeout)
	}
}

func TestNewExecutor_DefaultTimeout(t *testing.T) {
	var buf bytes.Buffer
	cfg := ExecutorConfig{
		Output:  &buf,
		NoColor: true,
	}

	executor := NewExecutor(cfg)

	if executor.timeout != 15*time.Minute {
		t.Errorf("expected default timeout 15m, got %v", executor.timeout)
	}
}

func TestExecutor_Tracker(t *testing.T) {
	var buf bytes.Buffer
	cfg := ExecutorConfig{
		Output:  &buf,
		NoColor: true,
	}

	executor := NewExecutor(cfg)

	tracker := executor.Tracker()
	if tracker == nil {
		t.Error("expected tracker to be returned")
	}
}

func TestBuildExecutionPlan_Empty(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs:       make(map[string]*resources.TaskDefResource),
		Services:       make(map[string]*resources.ServiceResource),
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	plan, err := BuildExecutionPlan(state)
	if err != nil {
		t.Fatalf("failed to build execution plan: %v", err)
	}

	if len(plan.TaskDefs) != 0 {
		t.Errorf("expected 0 task defs, got %d", len(plan.TaskDefs))
	}
	if len(plan.ServiceLevels) != 0 {
		t.Errorf("expected 0 service levels, got %d", len(plan.ServiceLevels))
	}
	if len(plan.ScheduledTasks) != 0 {
		t.Errorf("expected 0 scheduled tasks, got %d", len(plan.ScheduledTasks))
	}
}

func TestBuildExecutionPlan_WithTaskDefs(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: map[string]*resources.TaskDefResource{
			"web": {
				Name:   "web",
				Type:   "managed",
				Action: resources.TaskDefActionCreate,
				Desired: &config.TaskDefinition{
					Family: "web",
					Type:   "managed",
				},
			},
			"worker": {
				Name:   "worker",
				Type:   "managed",
				Action: resources.TaskDefActionUpdate,
				Desired: &config.TaskDefinition{
					Family: "worker",
					Type:   "managed",
				},
			},
		},
		Services:       make(map[string]*resources.ServiceResource),
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	plan, err := BuildExecutionPlan(state)
	if err != nil {
		t.Fatalf("failed to build execution plan: %v", err)
	}

	if len(plan.TaskDefs) != 2 {
		t.Errorf("expected 2 task defs, got %d", len(plan.TaskDefs))
	}
}

func TestResolveIngressTargetGroups_AddsLoadBalancer(t *testing.T) {
	services := map[string]config.Service{
		"api": {
			Name: "api",
		},
	}
	ingress := &config.Ingress{
		Rules: []config.IngressRule{
			{
				Service: &config.IngressServiceBackend{
					Name:          "api",
					ContainerName: "app",
					ContainerPort: 8080,
				},
			},
		},
	}
	targetGroupArns := map[int]string{0: "arn:aws:elasticloadbalancing:targetgroup/api/123"}

	ResolveIngressTargetGroups(services, ingress, targetGroupArns)

	svc := services["api"]
	if len(svc.LoadBalancers) != 1 {
		t.Fatalf("expected 1 load balancer, got %d", len(svc.LoadBalancers))
	}
	lb := svc.LoadBalancers[0]
	if lb.TargetGroupArn != targetGroupArns[0] {
		t.Errorf("expected target group ARN %q, got %q", targetGroupArns[0], lb.TargetGroupArn)
	}
	if lb.ContainerName != "app" {
		t.Errorf("expected container name 'app', got %q", lb.ContainerName)
	}
	if lb.ContainerPort != 8080 {
		t.Errorf("expected container port 8080, got %d", lb.ContainerPort)
	}
}

func TestBuildExecutionPlan_WithServicesNoDeps(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: make(map[string]*resources.TaskDefResource),
		Services: map[string]*resources.ServiceResource{
			"api": {
				Name:   "api",
				Action: resources.ServiceActionCreate,
				Desired: &config.Service{
					Name:    "api",
					Cluster: "test",
				},
			},
			"web": {
				Name:   "web",
				Action: resources.ServiceActionUpdate,
				Desired: &config.Service{
					Name:    "web",
					Cluster: "test",
				},
			},
		},
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	plan, err := BuildExecutionPlan(state)
	if err != nil {
		t.Fatalf("failed to build execution plan: %v", err)
	}

	if len(plan.ServiceLevels) != 1 {
		t.Errorf("expected 1 service level (all parallel), got %d", len(plan.ServiceLevels))
	}
	if len(plan.ServiceLevels[0]) != 2 {
		t.Errorf("expected 2 services in level 0, got %d", len(plan.ServiceLevels[0]))
	}
}

func TestBuildExecutionPlan_WithServicesDeps(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: make(map[string]*resources.TaskDefResource),
		Services: map[string]*resources.ServiceResource{
			"db": {
				Name:   "db",
				Action: resources.ServiceActionCreate,
				Desired: &config.Service{
					Name:      "db",
					Cluster:   "test",
					DependsOn: []string{},
				},
			},
			"api": {
				Name:   "api",
				Action: resources.ServiceActionCreate,
				Desired: &config.Service{
					Name:      "api",
					Cluster:   "test",
					DependsOn: []string{"db"},
				},
			},
			"web": {
				Name:   "web",
				Action: resources.ServiceActionUpdate,
				Desired: &config.Service{
					Name:      "web",
					Cluster:   "test",
					DependsOn: []string{"api"},
				},
			},
		},
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	plan, err := BuildExecutionPlan(state)
	if err != nil {
		t.Fatalf("failed to build execution plan: %v", err)
	}

	if len(plan.ServiceLevels) != 3 {
		t.Errorf("expected 3 service levels (chain), got %d", len(plan.ServiceLevels))
	}

	if plan.ServiceLevels[0][0] != "db" {
		t.Errorf("expected first level to be 'db', got '%s'", plan.ServiceLevels[0][0])
	}
	if plan.ServiceLevels[1][0] != "api" {
		t.Errorf("expected second level to be 'api', got '%s'", plan.ServiceLevels[1][0])
	}
	if plan.ServiceLevels[2][0] != "web" {
		t.Errorf("expected third level to be 'web', got '%s'", plan.ServiceLevels[2][0])
	}
}

func TestBuildExecutionPlan_ParallelDeps(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: make(map[string]*resources.TaskDefResource),
		Services: map[string]*resources.ServiceResource{
			"db": {
				Name:   "db",
				Action: resources.ServiceActionNoop,
				Desired: &config.Service{
					Name:      "db",
					Cluster:   "test",
					DependsOn: []string{},
				},
			},
			"api": {
				Name:   "api",
				Action: resources.ServiceActionUpdate,
				Desired: &config.Service{
					Name:      "api",
					Cluster:   "test",
					DependsOn: []string{"db"},
				},
			},
			"worker": {
				Name:   "worker",
				Action: resources.ServiceActionUpdate,
				Desired: &config.Service{
					Name:      "worker",
					Cluster:   "test",
					DependsOn: []string{"db"},
				},
			},
		},
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	plan, err := BuildExecutionPlan(state)
	if err != nil {
		t.Fatalf("failed to build execution plan: %v", err)
	}

	if len(plan.ServiceLevels) != 2 {
		t.Errorf("expected 2 service levels, got %d", len(plan.ServiceLevels))
	}

	if len(plan.ServiceLevels[0]) != 1 || plan.ServiceLevels[0][0] != "db" {
		t.Errorf("expected first level to be ['db'], got %v", plan.ServiceLevels[0])
	}

	if len(plan.ServiceLevels[1]) != 2 {
		t.Errorf("expected second level to have 2 services, got %d", len(plan.ServiceLevels[1]))
	}
}

func TestBuildExecutionPlan_WithScheduledTasks(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: make(map[string]*resources.TaskDefResource),
		Services: make(map[string]*resources.ServiceResource),
		ScheduledTasks: map[string]*resources.ScheduledTaskResource{
			"daily": {
				Name:   "daily",
				Action: resources.ScheduledTaskActionCreate,
				Desired: &config.ScheduledTask{
					TaskDefinition: "cron",
					Cluster:        "test",
				},
			},
			"hourly": {
				Name:   "hourly",
				Action: resources.ScheduledTaskActionUpdate,
				Desired: &config.ScheduledTask{
					TaskDefinition: "cron",
					Cluster:        "test",
				},
			},
		},
	}

	plan, err := BuildExecutionPlan(state)
	if err != nil {
		t.Fatalf("failed to build execution plan: %v", err)
	}

	if len(plan.ScheduledTasks) != 2 {
		t.Errorf("expected 2 scheduled tasks, got %d", len(plan.ScheduledTasks))
	}
}

func TestBuildExecutionPlan_CircularDependency(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: make(map[string]*resources.TaskDefResource),
		Services: map[string]*resources.ServiceResource{
			"a": {
				Name:   "a",
				Action: resources.ServiceActionCreate,
				Desired: &config.Service{
					Name:      "a",
					Cluster:   "test",
					DependsOn: []string{"b"},
				},
			},
			"b": {
				Name:   "b",
				Action: resources.ServiceActionCreate,
				Desired: &config.Service{
					Name:      "b",
					Cluster:   "test",
					DependsOn: []string{"a"},
				},
			},
		},
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	_, err := BuildExecutionPlan(state)
	if err == nil {
		t.Error("expected error for circular dependency")
	}
}

func TestBuildExecutionPlan_InvalidDependency(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: make(map[string]*resources.TaskDefResource),
		Services: map[string]*resources.ServiceResource{
			"api": {
				Name:   "api",
				Action: resources.ServiceActionCreate,
				Desired: &config.Service{
					Name:      "api",
					Cluster:   "test",
					DependsOn: []string{"nonexistent"},
				},
			},
		},
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	_, err := BuildExecutionPlan(state)
	if err == nil {
		t.Error("expected error for invalid dependency")
	}
}

func TestExecutionPlan_Graph(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: make(map[string]*resources.TaskDefResource),
		Services: map[string]*resources.ServiceResource{
			"web": {
				Name:   "web",
				Action: resources.ServiceActionCreate,
				Desired: &config.Service{
					Name:    "web",
					Cluster: "test",
				},
			},
		},
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	plan, err := BuildExecutionPlan(state)
	if err != nil {
		t.Fatalf("failed to build execution plan: %v", err)
	}

	if plan.Graph == nil {
		t.Fatal("expected graph to be set")
	}

	node, ok := plan.Graph.GetNode("web")
	if !ok {
		t.Error("expected to find 'web' node in graph")
	}
	if node.Name != "web" {
		t.Errorf("expected node name 'web', got '%s'", node.Name)
	}
}

func TestExecutorConfig_Fields(t *testing.T) {
	var buf bytes.Buffer
	cfg := ExecutorConfig{
		ECSClient:        nil,
		SchedulerClient:  nil,
		TaskDefManager:   nil,
		ServiceManager:   nil,
		ScheduledManager: nil,
		Output:           &buf,
		NoColor:          true,
		NoWait:           true,
		Timeout:          5 * time.Minute,
	}

	if cfg.NoColor != true {
		t.Error("expected NoColor to be true")
	}
	if cfg.NoWait != true {
		t.Error("expected NoWait to be true")
	}
	if cfg.Timeout != 5*time.Minute {
		t.Errorf("expected Timeout 5m, got %v", cfg.Timeout)
	}
}
