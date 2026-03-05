package cli

import (
	"testing"

	"github.com/x-qdo/ecsmate/internal/config"
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

func TestValidateManifestContent_SecretsWithoutManagedConfig(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"web": {
				Type:   "managed",
				Family: "my-app-web",
				ContainerDefinitions: []config.ContainerDefinition{
					{
						Name:  "app",
						Image: "nginx:latest",
						Secrets: []config.Secret{
							{Name: "GOOD", ValueFrom: "arn:aws:ssm:us-east-1:123:parameter/x"},
							{Name: "BAD", ValueFrom: "KREDINOR_CI_PASSWORD"},
						},
					},
				},
			},
		},
		Services: map[string]config.Service{
			"web": {Cluster: "test", TaskDefinition: "web"},
		},
	}

	errors := validateManifestContent(manifest)
	hasSecretError := false
	for _, e := range errors {
		if contains(e, "KREDINOR_CI_PASSWORD") {
			hasSecretError = true
		}
	}
	if !hasSecretError {
		t.Errorf("expected validation error for bare secret name, got: %v", errors)
	}
}

func TestValidateManifestContent_SecretsWithManagedConfig(t *testing.T) {
	manifest := &config.Manifest{
		Secrets: &config.SecretsConfig{
			Managed: &config.ManagedSecretsConfig{
				File:      "secrets.enc.yaml",
				KMSKeyArn: "arn:aws:kms:us-east-1:123:key/abc",
				SSMPrefix: "/myapp/prod",
			},
		},
		TaskDefinitions: map[string]config.TaskDefinition{
			"web": {
				Type:   "managed",
				Family: "my-app-web",
				ContainerDefinitions: []config.ContainerDefinition{
					{
						Name:  "app",
						Image: "nginx:latest",
						Secrets: []config.Secret{
							{Name: "DB_PASS", ValueFrom: "db_password"},
						},
					},
				},
			},
		},
		Services: map[string]config.Service{
			"web": {Cluster: "test", TaskDefinition: "web"},
		},
	}

	errors := validateManifestContent(manifest)
	for _, e := range errors {
		if contains(e, "db_password") {
			t.Errorf("bare key names should be allowed when managed secrets configured, got error: %s", e)
		}
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
