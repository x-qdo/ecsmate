package engine

import (
	"testing"

	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/diff"
	"github.com/qdo/ecsmate/internal/resources"
)

func TestPlanner_GeneratePlan_Empty(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs:       make(map[string]*resources.TaskDefResource),
		Services:       make(map[string]*resources.ServiceResource),
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	planner := NewPlanner()
	plan := planner.GeneratePlan(state)

	if len(plan.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(plan.Entries))
	}

	if plan.HasChanges() {
		t.Error("expected no changes")
	}
}

func TestPlanner_GeneratePlan_WithChanges(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: map[string]*resources.TaskDefResource{
			"php": {
				Name:   "php",
				Type:   "managed",
				Action: resources.TaskDefActionCreate,
				Desired: &config.TaskDefinition{
					Name:   "php",
					Type:   "managed",
					Family: "myapp-php",
					CPU:    "256",
					Memory: "512",
				},
			},
			"nginx": {
				Name:   "nginx",
				Type:   "managed",
				Action: resources.TaskDefActionUpdate,
				Desired: &config.TaskDefinition{
					Name:   "nginx",
					Type:   "managed",
					Family: "myapp-nginx",
				},
			},
			"cron": {
				Name:   "cron",
				Type:   "remote",
				Action: resources.TaskDefActionNoop,
			},
		},
		Services: map[string]*resources.ServiceResource{
			"web": {
				Name:   "web",
				Action: resources.ServiceActionCreate,
				Desired: &config.Service{
					Name:    "web",
					Cluster: "test-cluster",
				},
			},
		},
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	planner := NewPlanner()
	plan := planner.GeneratePlan(state)

	if len(plan.Entries) != 4 {
		t.Errorf("expected 4 entries, got %d", len(plan.Entries))
	}

	if !plan.HasChanges() {
		t.Error("expected changes")
	}

	if plan.Summary.Creates != 2 {
		t.Errorf("expected 2 creates, got %d", plan.Summary.Creates)
	}

	if plan.Summary.Updates != 1 {
		t.Errorf("expected 1 update, got %d", plan.Summary.Updates)
	}

	if plan.Summary.Noops != 1 {
		t.Errorf("expected 1 noop, got %d", plan.Summary.Noops)
	}
}

func TestPlanner_GeneratePlan_EntryTypes(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: map[string]*resources.TaskDefResource{
			"create-me": {
				Name:   "create-me",
				Type:   "managed",
				Action: resources.TaskDefActionCreate,
				Desired: &config.TaskDefinition{
					Name: "create-me",
					Type: "managed",
				},
			},
			"update-me": {
				Name:   "update-me",
				Type:   "managed",
				Action: resources.TaskDefActionUpdate,
				Desired: &config.TaskDefinition{
					Name: "update-me",
					Type: "managed",
				},
			},
			"unchanged": {
				Name:   "unchanged",
				Type:   "managed",
				Action: resources.TaskDefActionNoop,
			},
		},
		Services:       make(map[string]*resources.ServiceResource),
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	planner := NewPlanner()
	plan := planner.GeneratePlan(state)

	typeCount := map[diff.DiffType]int{}
	for _, entry := range plan.Entries {
		typeCount[entry.Type]++
	}

	if typeCount[diff.DiffTypeCreate] != 1 {
		t.Errorf("expected 1 CREATE, got %d", typeCount[diff.DiffTypeCreate])
	}

	if typeCount[diff.DiffTypeUpdate] != 1 {
		t.Errorf("expected 1 UPDATE, got %d", typeCount[diff.DiffTypeUpdate])
	}

	if typeCount[diff.DiffTypeNoop] != 1 {
		t.Errorf("expected 1 NOOP, got %d", typeCount[diff.DiffTypeNoop])
	}
}

func TestPlanner_GeneratePlan_ScheduledTasks(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: make(map[string]*resources.TaskDefResource),
		Services: make(map[string]*resources.ServiceResource),
		ScheduledTasks: map[string]*resources.ScheduledTaskResource{
			"daily-job": {
				Name:   "daily-job",
				Action: resources.ScheduledTaskActionCreate,
				Desired: &config.ScheduledTask{
					Name:               "daily-job",
					ScheduleType:       "cron",
					ScheduleExpression: "0 2 * * ? *",
				},
			},
			"hourly-job": {
				Name:   "hourly-job",
				Action: resources.ScheduledTaskActionUpdate,
				Desired: &config.ScheduledTask{
					Name:               "hourly-job",
					ScheduleType:       "rate",
					ScheduleExpression: "1 hour",
				},
			},
		},
	}

	planner := NewPlanner()
	plan := planner.GeneratePlan(state)

	if len(plan.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(plan.Entries))
	}

	scheduledCount := 0
	for _, entry := range plan.Entries {
		if entry.Resource == "ScheduledTask" {
			scheduledCount++
		}
	}

	if scheduledCount != 2 {
		t.Errorf("expected 2 scheduled task entries, got %d", scheduledCount)
	}
}

func TestBuildTaskDefView(t *testing.T) {
	td := &resources.TaskDefResource{
		Name: "php",
		Type: "managed",
		Desired: &config.TaskDefinition{
			Name:        "php",
			Type:        "managed",
			Family:      "myapp-php",
			CPU:         "256",
			Memory:      "512",
			NetworkMode: "awsvpc",
			ContainerDefinitions: []config.ContainerDefinition{
				{
					Name:      "php",
					Image:     "123456789.dkr.ecr.us-east-1.amazonaws.com/php:latest",
					CPU:       256,
					Essential: true,
					Environment: []config.KeyValuePair{
						{Name: "APP_ENV", Value: "production"},
					},
				},
			},
		},
	}

	view := buildTaskDefView(td)

	if view.Type != "managed" {
		t.Errorf("expected type 'managed', got '%s'", view.Type)
	}

	if view.Family != "myapp-php" {
		t.Errorf("expected family 'myapp-php', got '%s'", view.Family)
	}

	if view.CPU != "256" {
		t.Errorf("expected CPU '256', got '%s'", view.CPU)
	}

	if len(view.ContainerDefinitions) != 1 {
		t.Errorf("expected 1 container definition, got %d", len(view.ContainerDefinitions))
	}

	if len(view.ContainerDefinitions) > 0 {
		cd := view.ContainerDefinitions[0]
		if cd.Name != "php" {
			t.Errorf("expected container name 'php', got '%s'", cd.Name)
		}
		if cd.Environment["APP_ENV"] != "production" {
			t.Errorf("expected APP_ENV='production', got '%s'", cd.Environment["APP_ENV"])
		}
	}
}

func TestBuildServiceView(t *testing.T) {
	svc := &resources.ServiceResource{
		Name:              "web",
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/myapp-php:42",
		Desired: &config.Service{
			Name:         "web",
			Cluster:      "production-cluster",
			DesiredCount: 3,
			LaunchType:   "FARGATE",
			NetworkConfiguration: &config.NetworkConfiguration{
				Subnets:        []string{"subnet-1", "subnet-2"},
				SecurityGroups: []string{"sg-1"},
				AssignPublicIp: "DISABLED",
			},
			Deployment: config.DeploymentConfig{
				Strategy:              "rolling",
				MinimumHealthyPercent: 50,
				MaximumPercent:        200,
				CircuitBreakerEnable:  true,
			},
		},
	}

	view := buildServiceView(svc, nil, nil)

	if view.Cluster != "production-cluster" {
		t.Errorf("expected cluster 'production-cluster', got '%s'", view.Cluster)
	}

	if view.DesiredCount != 3 {
		t.Errorf("expected desiredCount 3, got %d", view.DesiredCount)
	}

	if view.NetworkConfiguration == nil {
		t.Fatal("expected network configuration, got nil")
	}

	if len(view.NetworkConfiguration.Subnets) != 2 {
		t.Errorf("expected 2 subnets, got %d", len(view.NetworkConfiguration.Subnets))
	}

	if view.Deployment == nil {
		t.Fatal("expected deployment config, got nil")
	}

	if view.Deployment.Strategy != "rolling" {
		t.Errorf("expected strategy 'rolling', got '%s'", view.Deployment.Strategy)
	}

	if !view.Deployment.CircuitBreakerEnable {
		t.Error("expected circuit breaker to be enabled")
	}
}

func TestBuildServiceView_IngressPlaceholder(t *testing.T) {
	svc := &resources.ServiceResource{
		Name:              "telemetry",
		TaskDefinitionArn: "arn:aws:ecs:us-east-1:123456789:task-definition/telemetry:1",
		Desired: &config.Service{
			Name:         "telemetry",
			Cluster:      "stage-cluster",
			DesiredCount: 1,
		},
	}

	ingress := &config.Ingress{
		Rules: []config.IngressRule{
			{
				Service: &config.IngressServiceBackend{
					Name:          "telemetry",
					ContainerName: "app",
					ContainerPort: 8080,
				},
			},
		},
	}

	view := buildServiceView(svc, ingress, nil)

	if len(view.LoadBalancers) != 1 {
		t.Fatalf("expected 1 load balancer, got %d", len(view.LoadBalancers))
	}
	if view.LoadBalancers[0].TargetGroupArn != pendingTargetGroupArn {
		t.Errorf("expected pending target group placeholder, got %q", view.LoadBalancers[0].TargetGroupArn)
	}
}

func TestBuildServiceView_TaskDefinitionPlaceholder(t *testing.T) {
	taskDefs := map[string]*resources.TaskDefResource{
		"web": {
			Name:   "web",
			Action: resources.TaskDefActionUpdate,
			Desired: &config.TaskDefinition{
				Family: "cal-web",
				Type:   "managed",
			},
		},
	}

	svc := &resources.ServiceResource{
		Name:              "web",
		TaskDefinitionArn: "arn:aws:ecs:eu-west-1:570129572534:task-definition/cal-web:3",
		Desired: &config.Service{
			Name:           "web",
			Cluster:        "stage-cluster",
			TaskDefinition: "web",
		},
	}

	view := buildServiceView(svc, nil, taskDefs)

	expected := "arn:aws:ecs:eu-west-1:570129572534:task-definition/cal-web:(new revision after apply)"
	if view.TaskDefinition != expected {
		t.Errorf("expected taskDefinition %q, got %q", expected, view.TaskDefinition)
	}
}

func TestPlanner_GeneratePlan_ServiceRecreate(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: make(map[string]*resources.TaskDefResource),
		Services: map[string]*resources.ServiceResource{
			"web": {
				Name:   "web",
				Action: resources.ServiceActionRecreate,
				RecreateReasons: []string{
					"launchType changed from EC2 to FARGATE",
				},
				Desired: &config.Service{
					Name:       "web",
					Cluster:    "test-cluster",
					LaunchType: "FARGATE",
				},
			},
		},
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	planner := NewPlanner()
	plan := planner.GeneratePlan(state)

	if len(plan.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(plan.Entries))
	}

	if !plan.HasChanges() {
		t.Error("expected changes (recreate)")
	}

	if plan.Summary.Recreates != 1 {
		t.Errorf("expected 1 recreate, got %d", plan.Summary.Recreates)
	}

	entry := plan.Entries[0]
	if entry.Type != diff.DiffTypeRecreate {
		t.Errorf("expected entry type RECREATE, got %s", entry.Type)
	}

	if len(entry.RecreateReasons) != 1 {
		t.Errorf("expected 1 recreate reason, got %d", len(entry.RecreateReasons))
	}

	if entry.RecreateReasons[0] != "launchType changed from EC2 to FARGATE" {
		t.Errorf("unexpected recreate reason: %s", entry.RecreateReasons[0])
	}
}

func TestPlanner_GeneratePlan_ServiceDelete(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: make(map[string]*resources.TaskDefResource),
		Services: map[string]*resources.ServiceResource{
			"old-service": {
				Name:   "old-service",
				Action: resources.ServiceActionDelete,
				Desired: &config.Service{
					Name:    "old-service",
					Cluster: "test-cluster",
				},
			},
		},
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	planner := NewPlanner()
	plan := planner.GeneratePlan(state)

	if len(plan.Entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(plan.Entries))
	}

	if !plan.HasChanges() {
		t.Error("expected changes (delete)")
	}

	if plan.Summary.Deletes != 1 {
		t.Errorf("expected 1 delete, got %d", plan.Summary.Deletes)
	}

	entry := plan.Entries[0]
	if entry.Type != diff.DiffTypeDelete {
		t.Errorf("expected entry type DELETE, got %s", entry.Type)
	}
}

func TestPlanner_GeneratePlan_MixedActions(t *testing.T) {
	state := &resources.DesiredState{
		TaskDefs: map[string]*resources.TaskDefResource{
			"td-create": {
				Name:   "td-create",
				Type:   "managed",
				Action: resources.TaskDefActionCreate,
				Desired: &config.TaskDefinition{
					Name: "td-create",
					Type: "managed",
				},
			},
		},
		Services: map[string]*resources.ServiceResource{
			"svc-update": {
				Name:   "svc-update",
				Action: resources.ServiceActionUpdate,
				Desired: &config.Service{
					Name:    "svc-update",
					Cluster: "test-cluster",
				},
			},
			"svc-recreate": {
				Name:   "svc-recreate",
				Action: resources.ServiceActionRecreate,
				RecreateReasons: []string{
					"launchType changed",
					"loadBalancers changed",
				},
				Desired: &config.Service{
					Name:    "svc-recreate",
					Cluster: "test-cluster",
				},
			},
			"svc-delete": {
				Name:   "svc-delete",
				Action: resources.ServiceActionDelete,
				Desired: &config.Service{
					Name:    "svc-delete",
					Cluster: "test-cluster",
				},
			},
			"svc-noop": {
				Name:   "svc-noop",
				Action: resources.ServiceActionNoop,
				Desired: &config.Service{
					Name:    "svc-noop",
					Cluster: "test-cluster",
				},
			},
		},
		ScheduledTasks: make(map[string]*resources.ScheduledTaskResource),
	}

	planner := NewPlanner()
	plan := planner.GeneratePlan(state)

	if len(plan.Entries) != 5 {
		t.Errorf("expected 5 entries, got %d", len(plan.Entries))
	}

	if plan.Summary.Creates != 1 {
		t.Errorf("expected 1 create, got %d", plan.Summary.Creates)
	}
	if plan.Summary.Updates != 1 {
		t.Errorf("expected 1 update, got %d", plan.Summary.Updates)
	}
	if plan.Summary.Recreates != 1 {
		t.Errorf("expected 1 recreate, got %d", plan.Summary.Recreates)
	}
	if plan.Summary.Deletes != 1 {
		t.Errorf("expected 1 delete, got %d", plan.Summary.Deletes)
	}
	if plan.Summary.Noops != 1 {
		t.Errorf("expected 1 noop, got %d", plan.Summary.Noops)
	}
}

func TestPlanner_CalculateSummary_WithRecreates(t *testing.T) {
	entries := []diff.DiffEntry{
		{Type: diff.DiffTypeCreate, Name: "a"},
		{Type: diff.DiffTypeUpdate, Name: "b"},
		{Type: diff.DiffTypeRecreate, Name: "c"},
		{Type: diff.DiffTypeRecreate, Name: "d"},
		{Type: diff.DiffTypeDelete, Name: "e"},
		{Type: diff.DiffTypeNoop, Name: "f"},
	}

	planner := NewPlanner()
	summary := planner.calculateSummary(entries)

	if summary.Creates != 1 {
		t.Errorf("expected 1 create, got %d", summary.Creates)
	}
	if summary.Updates != 1 {
		t.Errorf("expected 1 update, got %d", summary.Updates)
	}
	if summary.Recreates != 2 {
		t.Errorf("expected 2 recreates, got %d", summary.Recreates)
	}
	if summary.Deletes != 1 {
		t.Errorf("expected 1 delete, got %d", summary.Deletes)
	}
	if summary.Noops != 1 {
		t.Errorf("expected 1 noop, got %d", summary.Noops)
	}
}

func TestPlan_HasChanges_WithRecreates(t *testing.T) {
	plan := &Plan{
		Summary: diff.DiffSummary{
			Creates:   0,
			Updates:   0,
			Recreates: 1,
			Deletes:   0,
			Noops:     5,
		},
	}

	if !plan.HasChanges() {
		t.Error("expected HasChanges to return true when recreates > 0")
	}
}

func TestPlan_HasChanges_OnlyNoops(t *testing.T) {
	plan := &Plan{
		Summary: diff.DiffSummary{
			Creates:   0,
			Updates:   0,
			Recreates: 0,
			Deletes:   0,
			Noops:     5,
		},
	}

	if plan.HasChanges() {
		t.Error("expected HasChanges to return false when only noops")
	}
}
