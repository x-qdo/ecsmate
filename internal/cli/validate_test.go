package cli

import (
	"testing"

	"github.com/qdo/ecsmate/internal/config"
)

func TestValidateManifestContent_Valid(t *testing.T) {
	manifest := &config.Manifest{
		Name: "test-app",
		TaskDefinitions: map[string]config.TaskDefinition{
			"php": {
				Type:   "managed",
				Family: "my-app-php",
				ContainerDefinitions: []config.ContainerDefinition{
					{Name: "php", Image: "php:latest"},
				},
			},
			"remote-task": {
				Type: "remote",
				Arn:  "arn:aws:ecs:us-east-1:123:task-definition/remote:1",
			},
		},
		Services: map[string]config.Service{
			"web": {
				Cluster:        "my-cluster",
				TaskDefinition: "php",
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) > 0 {
		t.Errorf("expected no errors, got: %v", errors)
	}
}

func TestValidateManifestContent_MissingTaskDefFamily(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"php": {
				Type: "managed",
				// Missing Family
				ContainerDefinitions: []config.ContainerDefinition{
					{Name: "php", Image: "php:latest"},
				},
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errors), errors)
	}
}

func TestValidateManifestContent_MissingContainerImage(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"php": {
				Type:   "managed",
				Family: "my-app-php",
				ContainerDefinitions: []config.ContainerDefinition{
					{Name: "php", Image: ""}, // Missing image
				},
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errors), errors)
	}
}

func TestValidateManifestContent_MergedMissingBaseArn(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"merged-task": {
				Type:    "merged",
				BaseArn: "", // Missing BaseArn
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errors), errors)
	}
}

func TestValidateManifestContent_RemoteMissingArn(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"remote-task": {
				Type: "remote",
				Arn:  "", // Missing Arn
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errors), errors)
	}
}

func TestValidateManifestContent_ServiceMissingCluster(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"php": {
				Type:   "managed",
				Family: "my-app-php",
				ContainerDefinitions: []config.ContainerDefinition{
					{Name: "php", Image: "php:latest"},
				},
			},
		},
		Services: map[string]config.Service{
			"web": {
				Cluster:        "", // Missing cluster
				TaskDefinition: "php",
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errors), errors)
	}
}

func TestValidateManifestContent_ServiceUnknownTaskDef(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{},
		Services: map[string]config.Service{
			"web": {
				Cluster:        "my-cluster",
				TaskDefinition: "nonexistent", // Unknown task def
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errors), errors)
	}
}

func TestValidateManifestContent_ServiceUnknownDependency(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"php": {
				Type:   "managed",
				Family: "my-app-php",
				ContainerDefinitions: []config.ContainerDefinition{
					{Name: "php", Image: "php:latest"},
				},
			},
		},
		Services: map[string]config.Service{
			"web": {
				Cluster:        "my-cluster",
				TaskDefinition: "php",
				DependsOn:      []string{"unknown-service"}, // Unknown dependency
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errors), errors)
	}
}

func TestValidateManifestContent_ScheduledTaskValid(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"cron": {
				Type:   "managed",
				Family: "my-app-cron",
				ContainerDefinitions: []config.ContainerDefinition{
					{Name: "cron", Image: "cron:latest"},
				},
			},
		},
		ScheduledTasks: map[string]config.ScheduledTask{
			"daily": {
				Cluster:            "my-cluster",
				TaskDefinition:     "cron",
				ScheduleExpression: "0 2 * * ? *",
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) > 0 {
		t.Errorf("expected no errors, got: %v", errors)
	}
}

func TestValidateManifestContent_ScheduledTaskMissingSchedule(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"cron": {
				Type:   "managed",
				Family: "my-app-cron",
				ContainerDefinitions: []config.ContainerDefinition{
					{Name: "cron", Image: "cron:latest"},
				},
			},
		},
		ScheduledTasks: map[string]config.ScheduledTask{
			"daily": {
				Cluster:            "my-cluster",
				TaskDefinition:     "cron",
				ScheduleExpression: "", // Missing schedule
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errors), errors)
	}
}

func TestValidateManifestContent_InvalidType(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"invalid": {
				Type: "unknown", // Invalid type
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(errors), errors)
	}
}

func TestValidateManifestContent_MultipleErrors(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"invalid": {
				Type: "managed",
				// Missing family and containers
			},
		},
		Services: map[string]config.Service{
			"web": {
				Cluster:        "", // Missing cluster
				TaskDefinition: "", // Missing task def
			},
		},
	}

	errors := validateManifestContent(manifest)
	if len(errors) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(errors), errors)
	}
}
