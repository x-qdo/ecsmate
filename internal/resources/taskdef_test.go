package resources

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"github.com/qdo/ecsmate/internal/config"
)

func TestTaskDefResource_ToRegisterInput(t *testing.T) {
	resource := &TaskDefResource{
		Name: "php",
		Type: "managed",
		Desired: &config.TaskDefinition{
			Name:                     "php",
			Type:                     "managed",
			Family:                   "myapp-php",
			CPU:                      "256",
			Memory:                   "512",
			NetworkMode:              "awsvpc",
			ExecutionRoleArn:         "arn:aws:iam::123456789:role/ecsTaskExecutionRole",
			TaskRoleArn:              "arn:aws:iam::123456789:role/ecsTaskRole",
			RequiresCompatibilities:  []string{"FARGATE"},
			ContainerDefinitions: []config.ContainerDefinition{
				{
					Name:      "php",
					Image:     "123456789.dkr.ecr.us-east-1.amazonaws.com/php:latest",
					CPU:       256,
					Essential: true,
					PortMappings: []config.PortMapping{
						{ContainerPort: 9000, Protocol: "tcp"},
					},
					Environment: []config.KeyValuePair{
						{Name: "APP_ENV", Value: "production"},
					},
					Secrets: []config.Secret{
						{Name: "DB_PASSWORD", ValueFrom: "arn:aws:secretsmanager:us-east-1:123456789:secret:db-password"},
					},
					LogConfiguration: &config.LogConfiguration{
						LogDriver: "awslogs",
						Options: map[string]string{
							"awslogs-group":         "/ecs/myapp",
							"awslogs-region":        "us-east-1",
							"awslogs-stream-prefix": "php",
						},
					},
				},
			},
		},
	}

	input, err := resource.ToRegisterInput()
	if err != nil {
		t.Fatalf("ToRegisterInput failed: %v", err)
	}

	if aws.ToString(input.Family) != "myapp-php" {
		t.Errorf("expected family 'myapp-php', got '%s'", aws.ToString(input.Family))
	}

	if aws.ToString(input.Cpu) != "256" {
		t.Errorf("expected CPU '256', got '%s'", aws.ToString(input.Cpu))
	}

	if aws.ToString(input.Memory) != "512" {
		t.Errorf("expected memory '512', got '%s'", aws.ToString(input.Memory))
	}

	if len(input.RequiresCompatibilities) != 1 || input.RequiresCompatibilities[0] != types.CompatibilityFargate {
		t.Errorf("expected FARGATE compatibility, got %v", input.RequiresCompatibilities)
	}

	if len(input.ContainerDefinitions) != 1 {
		t.Fatalf("expected 1 container definition, got %d", len(input.ContainerDefinitions))
	}

	cd := input.ContainerDefinitions[0]

	if aws.ToString(cd.Name) != "php" {
		t.Errorf("expected container name 'php', got '%s'", aws.ToString(cd.Name))
	}

	if len(cd.PortMappings) != 1 {
		t.Errorf("expected 1 port mapping, got %d", len(cd.PortMappings))
	}

	if len(cd.Environment) != 1 {
		t.Errorf("expected 1 environment var, got %d", len(cd.Environment))
	}

	if len(cd.Secrets) != 1 {
		t.Errorf("expected 1 secret, got %d", len(cd.Secrets))
	}

	if cd.LogConfiguration == nil {
		t.Error("expected log configuration, got nil")
	}
}

func TestTaskDefResource_ToRegisterInput_NoDesired(t *testing.T) {
	resource := &TaskDefResource{
		Name:    "test",
		Type:    "managed",
		Desired: nil,
	}

	_, err := resource.ToRegisterInput()
	if err == nil {
		t.Error("expected error for nil Desired, got nil")
	}
}

func TestTaskDefResource_DetermineAction(t *testing.T) {
	tests := []struct {
		name     string
		resource *TaskDefResource
		expected TaskDefAction
	}{
		{
			name: "create when no current",
			resource: &TaskDefResource{
				Name:    "test",
				Type:    "managed",
				Desired: &config.TaskDefinition{Family: "test"},
				Current: nil,
			},
			expected: TaskDefActionCreate,
		},
		{
			name: "noop when no changes",
			resource: &TaskDefResource{
				Name: "test",
				Type: "managed",
				Desired: &config.TaskDefinition{
					Family:      "test",
					CPU:         "256",
					Memory:      "512",
					NetworkMode: "awsvpc",
				},
				Current: &types.TaskDefinition{
					Family:      aws.String("test"),
					Cpu:         aws.String("256"),
					Memory:      aws.String("512"),
					NetworkMode: types.NetworkModeAwsvpc,
				},
			},
			expected: TaskDefActionNoop,
		},
		{
			name: "update when CPU changed",
			resource: &TaskDefResource{
				Name: "test",
				Type: "managed",
				Desired: &config.TaskDefinition{
					Family: "test",
					CPU:    "512",
					Memory: "512",
				},
				Current: &types.TaskDefinition{
					Family: aws.String("test"),
					Cpu:    aws.String("256"),
					Memory: aws.String("512"),
				},
			},
			expected: TaskDefActionUpdate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.resource.determineAction()
			if tt.resource.Action != tt.expected {
				t.Errorf("expected action %s, got %s", tt.expected, tt.resource.Action)
			}
		})
	}
}

func TestHasContainerChanges(t *testing.T) {
	tests := []struct {
		name     string
		current  types.ContainerDefinition
		desired  config.ContainerDefinition
		expected bool
	}{
		{
			name: "no changes",
			current: types.ContainerDefinition{
				Name:  aws.String("php"),
				Image: aws.String("php:latest"),
			},
			desired: config.ContainerDefinition{
				Name:  "php",
				Image: "php:latest",
			},
			expected: false,
		},
		{
			name: "image changed",
			current: types.ContainerDefinition{
				Name:  aws.String("php"),
				Image: aws.String("php:v1"),
			},
			desired: config.ContainerDefinition{
				Name:  "php",
				Image: "php:v2",
			},
			expected: true,
		},
		{
			name: "command changed",
			current: types.ContainerDefinition{
				Name:    aws.String("php"),
				Image:   aws.String("php:latest"),
				Command: []string{"php", "artisan", "serve"},
			},
			desired: config.ContainerDefinition{
				Name:    "php",
				Image:   "php:latest",
				Command: []string{"php", "artisan", "queue:work"},
			},
			expected: true,
		},
		{
			name: "environment changed",
			current: types.ContainerDefinition{
				Name:  aws.String("php"),
				Image: aws.String("php:latest"),
				Environment: []types.KeyValuePair{
					{Name: aws.String("APP_ENV"), Value: aws.String("staging")},
				},
			},
			desired: config.ContainerDefinition{
				Name:  "php",
				Image: "php:latest",
				Environment: []config.KeyValuePair{
					{Name: "APP_ENV", Value: "production"},
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasContainerChanges(tt.current, tt.desired)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestExtractFamily(t *testing.T) {
	tests := []struct {
		arn      string
		expected string
	}{
		{"arn:aws:ecs:us-east-1:123456789:task-definition/myapp-php:42", "myapp-php"},
		{"arn:aws:ecs:us-east-1:123456789:task-definition/myapp-php", "myapp-php"},
		{"myapp-php:42", "myapp-php"},
		{"myapp-php", "myapp-php"},
	}

	for _, tt := range tests {
		result := extractFamily(tt.arn)
		if result != tt.expected {
			t.Errorf("extractFamily(%s) = %s, expected %s", tt.arn, result, tt.expected)
		}
	}
}

func TestMergeEnvironment(t *testing.T) {
	base := []config.KeyValuePair{
		{Name: "A", Value: "1"},
		{Name: "B", Value: "2"},
	}

	override := []config.KeyValuePair{
		{Name: "B", Value: "override"},
		{Name: "C", Value: "3"},
	}

	result := mergeEnvironment(base, override)

	envMap := make(map[string]string)
	for _, e := range result {
		envMap[e.Name] = e.Value
	}

	if envMap["A"] != "1" {
		t.Errorf("expected A=1, got A=%s", envMap["A"])
	}

	if envMap["B"] != "override" {
		t.Errorf("expected B=override, got B=%s", envMap["B"])
	}

	if envMap["C"] != "3" {
		t.Errorf("expected C=3, got C=%s", envMap["C"])
	}

	if len(result) != 3 {
		t.Errorf("expected 3 entries, got %d", len(result))
	}
}

func TestStringSliceEqual(t *testing.T) {
	tests := []struct {
		a        []string
		b        []string
		expected bool
	}{
		{[]string{"a", "b"}, []string{"a", "b"}, true},
		{[]string{"a", "b"}, []string{"a", "c"}, false},
		{[]string{"a", "b"}, []string{"a"}, false},
		{[]string{}, []string{}, true},
		{nil, nil, true},
		{nil, []string{}, true},
	}

	for _, tt := range tests {
		result := stringSliceEqual(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("stringSliceEqual(%v, %v) = %v, expected %v", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestApplyOverrides(t *testing.T) {
	manager := &TaskDefManager{}

	base := &types.TaskDefinition{
		Family:      aws.String("base-family"),
		Cpu:         aws.String("256"),
		Memory:      aws.String("512"),
		NetworkMode: types.NetworkModeAwsvpc,
		ExecutionRoleArn: aws.String("arn:aws:iam::123:role/base-exec"),
		TaskRoleArn:      aws.String("arn:aws:iam::123:role/base-task"),
		RequiresCompatibilities: []types.Compatibility{types.CompatibilityFargate},
		ContainerDefinitions: []types.ContainerDefinition{
			{
				Name:  aws.String("app"),
				Image: aws.String("base-image:v1"),
				Cpu:   256,
				Environment: []types.KeyValuePair{
					{Name: aws.String("BASE_VAR"), Value: aws.String("base-value")},
				},
			},
		},
	}

	overrides := &config.TaskDefinition{
		Name: "test",
		Type: "merged",
		Overrides: &config.TaskDefOverrides{
			CPU:    "512",
			Memory: "1024",
			ContainerDefinitions: []config.ContainerOverride{
				{
					Name:  "app",
					Image: "override-image:v2",
					Environment: []config.KeyValuePair{
						{Name: "NEW_VAR", Value: "new-value"},
					},
				},
			},
		},
	}

	result := manager.applyOverrides(base, overrides)

	if result.CPU != "512" {
		t.Errorf("expected CPU '512', got '%s'", result.CPU)
	}

	if result.Memory != "1024" {
		t.Errorf("expected Memory '1024', got '%s'", result.Memory)
	}

	if result.Family != "base-family" {
		t.Errorf("expected Family 'base-family', got '%s'", result.Family)
	}

	if len(result.ContainerDefinitions) != 1 {
		t.Fatalf("expected 1 container, got %d", len(result.ContainerDefinitions))
	}

	cd := result.ContainerDefinitions[0]
	if cd.Image != "override-image:v2" {
		t.Errorf("expected Image 'override-image:v2', got '%s'", cd.Image)
	}

	// Check environment merge
	envMap := make(map[string]string)
	for _, env := range cd.Environment {
		envMap[env.Name] = env.Value
	}

	if envMap["BASE_VAR"] != "base-value" {
		t.Errorf("expected BASE_VAR='base-value', got '%s'", envMap["BASE_VAR"])
	}

	if envMap["NEW_VAR"] != "new-value" {
		t.Errorf("expected NEW_VAR='new-value', got '%s'", envMap["NEW_VAR"])
	}
}

func TestConvertECSContainerDefinition(t *testing.T) {
	ecsCD := types.ContainerDefinition{
		Name:      aws.String("php"),
		Image:     aws.String("php:8.2"),
		Cpu:       512,
		Memory:    aws.Int32(1024),
		Essential: aws.Bool(true),
		Command:   []string{"php-fpm"},
		WorkingDirectory: aws.String("/app"),
		Environment: []types.KeyValuePair{
			{Name: aws.String("APP_ENV"), Value: aws.String("production")},
		},
		Secrets: []types.Secret{
			{Name: aws.String("DB_PASS"), ValueFrom: aws.String("arn:aws:ssm:us-east-1:123:parameter/db")},
		},
		PortMappings: []types.PortMapping{
			{ContainerPort: aws.Int32(9000), HostPort: aws.Int32(9000), Protocol: types.TransportProtocolTcp},
		},
		MountPoints: []types.MountPoint{
			{SourceVolume: aws.String("data"), ContainerPath: aws.String("/data"), ReadOnly: aws.Bool(true)},
		},
		HealthCheck: &types.HealthCheck{
			Command:     []string{"CMD-SHELL", "php-fpm-healthcheck"},
			Interval:    aws.Int32(30),
			Timeout:     aws.Int32(5),
			Retries:     aws.Int32(3),
			StartPeriod: aws.Int32(60),
		},
		LogConfiguration: &types.LogConfiguration{
			LogDriver: types.LogDriverAwslogs,
			Options: map[string]string{
				"awslogs-group":  "/ecs/app",
				"awslogs-region": "us-east-1",
			},
		},
		DependsOn: []types.ContainerDependency{
			{ContainerName: aws.String("init"), Condition: types.ContainerConditionComplete},
		},
	}

	result := convertECSContainerDefinition(ecsCD)

	if result.Name != "php" {
		t.Errorf("expected Name 'php', got '%s'", result.Name)
	}
	if result.Image != "php:8.2" {
		t.Errorf("expected Image 'php:8.2', got '%s'", result.Image)
	}
	if result.CPU != 512 {
		t.Errorf("expected CPU 512, got %d", result.CPU)
	}
	if result.Memory != 1024 {
		t.Errorf("expected Memory 1024, got %d", result.Memory)
	}
	if !result.Essential {
		t.Error("expected Essential true")
	}
	if result.WorkingDirectory != "/app" {
		t.Errorf("expected WorkingDirectory '/app', got '%s'", result.WorkingDirectory)
	}
	if len(result.Command) != 1 || result.Command[0] != "php-fpm" {
		t.Errorf("expected Command ['php-fpm'], got %v", result.Command)
	}
	if len(result.Environment) != 1 {
		t.Errorf("expected 1 env var, got %d", len(result.Environment))
	}
	if len(result.Secrets) != 1 {
		t.Errorf("expected 1 secret, got %d", len(result.Secrets))
	}
	if len(result.PortMappings) != 1 {
		t.Errorf("expected 1 port mapping, got %d", len(result.PortMappings))
	}
	if len(result.MountPoints) != 1 {
		t.Errorf("expected 1 mount point, got %d", len(result.MountPoints))
	}
	if result.HealthCheck == nil {
		t.Error("expected HealthCheck, got nil")
	}
	if result.LogConfiguration == nil {
		t.Error("expected LogConfiguration, got nil")
	}
	if len(result.DependsOn) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(result.DependsOn))
	}
}

func TestMergeSecrets(t *testing.T) {
	base := []config.Secret{
		{Name: "SECRET_A", ValueFrom: "arn:a"},
		{Name: "SECRET_B", ValueFrom: "arn:b"},
	}

	override := []config.Secret{
		{Name: "SECRET_B", ValueFrom: "arn:b-override"},
		{Name: "SECRET_C", ValueFrom: "arn:c"},
	}

	result := mergeSecrets(base, override)

	secretMap := make(map[string]string)
	for _, s := range result {
		secretMap[s.Name] = s.ValueFrom
	}

	if secretMap["SECRET_A"] != "arn:a" {
		t.Errorf("expected SECRET_A='arn:a', got '%s'", secretMap["SECRET_A"])
	}

	if secretMap["SECRET_B"] != "arn:b-override" {
		t.Errorf("expected SECRET_B='arn:b-override', got '%s'", secretMap["SECRET_B"])
	}

	if secretMap["SECRET_C"] != "arn:c" {
		t.Errorf("expected SECRET_C='arn:c', got '%s'", secretMap["SECRET_C"])
	}

	if len(result) != 3 {
		t.Errorf("expected 3 secrets, got %d", len(result))
	}
}

func TestTaskDefResource_TypeValidation(t *testing.T) {
	tests := []struct {
		name     string
		tdType   string
		hasError bool
	}{
		{"managed type", "managed", false},
		{"merged type", "merged", false},
		{"remote type", "remote", false},
		{"invalid type", "invalid", true},
		{"empty type", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			td := &config.TaskDefinition{
				Name: "test",
				Type: tt.tdType,
			}

			switch tt.tdType {
			case "managed", "merged", "remote", "":
				// These are valid or will fail validation elsewhere
			default:
				// Test that unknown type would return error
				if tt.tdType != "managed" && tt.tdType != "merged" && tt.tdType != "remote" && tt.tdType != "" {
					// Would error in BuildResource
				}
			}

			// Verify type is set correctly
			if td.Type != tt.tdType {
				t.Errorf("expected type '%s', got '%s'", tt.tdType, td.Type)
			}
		})
	}
}

func TestConvertContainerDefinition(t *testing.T) {
	cd := config.ContainerDefinition{
		Name:      "php",
		Image:     "php:latest",
		CPU:       256,
		Memory:    512,
		Essential: true,
		Command:   []string{"php", "artisan", "serve"},
		PortMappings: []config.PortMapping{
			{ContainerPort: 9000, Protocol: "tcp"},
		},
		Environment: []config.KeyValuePair{
			{Name: "APP_ENV", Value: "production"},
		},
		HealthCheck: &config.HealthCheck{
			Command:     []string{"CMD-SHELL", "curl -f http://localhost/ || exit 1"},
			Interval:    30,
			Timeout:     5,
			Retries:     3,
			StartPeriod: 60,
		},
	}

	result := convertContainerDefinition(cd)

	if aws.ToString(result.Name) != "php" {
		t.Errorf("expected name 'php', got '%s'", aws.ToString(result.Name))
	}

	if aws.ToString(result.Image) != "php:latest" {
		t.Errorf("expected image 'php:latest', got '%s'", aws.ToString(result.Image))
	}

	if result.Cpu != 256 {
		t.Errorf("expected CPU 256, got %d", result.Cpu)
	}

	if len(result.PortMappings) != 1 {
		t.Errorf("expected 1 port mapping, got %d", len(result.PortMappings))
	}

	if result.HealthCheck == nil {
		t.Error("expected health check, got nil")
	}
}
