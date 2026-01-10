package config

import (
	"context"
	"testing"
)

type mockSSMResolver struct {
	params map[string]string
}

func (m *mockSSMResolver) GetParameter(ctx context.Context, name string) (string, error) {
	return m.params[name], nil
}

func (m *mockSSMResolver) GetParameters(ctx context.Context, names []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, name := range names {
		if v, ok := m.params[name]; ok {
			result[name] = v
		}
	}
	return result, nil
}

func TestHasSSMReferences(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"simple string", false},
		{"{{ssm:/my/param}}", true},
		{"prefix-{{ssm:/my/param}}-suffix", true},
		{"{{ssm:/param1}}-{{ssm:/param2}}", true},
		{"{{ssm: /spaced/param }}", true},
		{"{{notssm:/param}}", false},
		{"", false},
	}

	for _, tt := range tests {
		result := HasSSMReferences(tt.input)
		if result != tt.expected {
			t.Errorf("HasSSMReferences(%q) = %v, expected %v", tt.input, result, tt.expected)
		}
	}
}

func TestExtractSSMReferences(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"simple string", []string{}},
		{"{{ssm:/my/param}}", []string{"/my/param"}},
		{"prefix-{{ssm:/my/param}}-suffix", []string{"/my/param"}},
		{"{{ssm:/param1}}-{{ssm:/param2}}", []string{"/param1", "/param2"}},
		{"{{ssm: /spaced/param }}", []string{"/spaced/param"}},
	}

	for _, tt := range tests {
		result := ExtractSSMReferences(tt.input)
		if len(result) != len(tt.expected) {
			t.Errorf("ExtractSSMReferences(%q) = %v, expected %v", tt.input, result, tt.expected)
			continue
		}
		for i, r := range result {
			if r != tt.expected[i] {
				t.Errorf("ExtractSSMReferences(%q)[%d] = %q, expected %q", tt.input, i, r, tt.expected[i])
			}
		}
	}
}

func TestResolveSSMReferences(t *testing.T) {
	ctx := context.Background()
	resolver := &mockSSMResolver{
		params: map[string]string{
			"/app/role-arn":  "arn:aws:iam::123:role/MyRole",
			"/app/cluster":   "production-cluster",
			"/app/subnet-1":  "subnet-abc123",
			"/app/sg-1":      "sg-def456",
			"/app/image-tag": "v1.2.3",
			"/app/db-secret": "arn:aws:secretsmanager:us-east-1:123:secret:db",
		},
	}

	manifest := &Manifest{
		Name: "test-app",
		TaskDefinitions: map[string]TaskDefinition{
			"php": {
				Name:             "php",
				Type:             "managed",
				ExecutionRoleArn: "{{ssm:/app/role-arn}}",
				ContainerDefinitions: []ContainerDefinition{
					{
						Name:  "php",
						Image: "myapp:{{ssm:/app/image-tag}}",
						Environment: []KeyValuePair{
							{Name: "STATIC", Value: "static-value"},
						},
						Secrets: []Secret{
							{Name: "DB_PASSWORD", ValueFrom: "{{ssm:/app/db-secret}}"},
						},
					},
				},
			},
		},
		Services: map[string]Service{
			"web": {
				Name:    "web",
				Cluster: "{{ssm:/app/cluster}}",
				NetworkConfiguration: &NetworkConfiguration{
					Subnets:        []string{"{{ssm:/app/subnet-1}}"},
					SecurityGroups: []string{"{{ssm:/app/sg-1}}"},
				},
			},
		},
	}

	err := ResolveSSMReferences(ctx, manifest, resolver)
	if err != nil {
		t.Fatalf("ResolveSSMReferences failed: %v", err)
	}

	td := manifest.TaskDefinitions["php"]
	if td.ExecutionRoleArn != "arn:aws:iam::123:role/MyRole" {
		t.Errorf("ExecutionRoleArn not resolved, got %q", td.ExecutionRoleArn)
	}

	if td.ContainerDefinitions[0].Image != "myapp:v1.2.3" {
		t.Errorf("Image not resolved, got %q", td.ContainerDefinitions[0].Image)
	}

	if td.ContainerDefinitions[0].Secrets[0].ValueFrom != "arn:aws:secretsmanager:us-east-1:123:secret:db" {
		t.Errorf("Secret ValueFrom not resolved, got %q", td.ContainerDefinitions[0].Secrets[0].ValueFrom)
	}

	svc := manifest.Services["web"]
	if svc.Cluster != "production-cluster" {
		t.Errorf("Cluster not resolved, got %q", svc.Cluster)
	}

	if svc.NetworkConfiguration.Subnets[0] != "subnet-abc123" {
		t.Errorf("Subnet not resolved, got %q", svc.NetworkConfiguration.Subnets[0])
	}

	if svc.NetworkConfiguration.SecurityGroups[0] != "sg-def456" {
		t.Errorf("SecurityGroup not resolved, got %q", svc.NetworkConfiguration.SecurityGroups[0])
	}
}

func TestResolveSSMReferences_NoReferences(t *testing.T) {
	ctx := context.Background()
	resolver := &mockSSMResolver{params: map[string]string{}}

	manifest := &Manifest{
		Name: "test-app",
		TaskDefinitions: map[string]TaskDefinition{
			"php": {
				Name: "php",
				Type: "managed",
				ContainerDefinitions: []ContainerDefinition{
					{Name: "php", Image: "myapp:latest"},
				},
			},
		},
	}

	err := ResolveSSMReferences(ctx, manifest, resolver)
	if err != nil {
		t.Fatalf("ResolveSSMReferences failed: %v", err)
	}
}

func TestResolveSSMReferences_NilResolver(t *testing.T) {
	ctx := context.Background()

	manifest := &Manifest{
		Name: "test-app",
		TaskDefinitions: map[string]TaskDefinition{
			"php": {
				Name:             "php",
				ExecutionRoleArn: "{{ssm:/app/role}}",
			},
		},
	}

	err := ResolveSSMReferences(ctx, manifest, nil)
	if err != nil {
		t.Fatalf("ResolveSSMReferences with nil resolver should not error: %v", err)
	}

	// Reference should remain unchanged
	if manifest.TaskDefinitions["php"].ExecutionRoleArn != "{{ssm:/app/role}}" {
		t.Error("SSM reference should not be resolved with nil resolver")
	}
}

func TestCollectSSMReferences_AllTypes(t *testing.T) {
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"managed": {
				Type:             "managed",
				ExecutionRoleArn: "{{ssm:/td/exec-role}}",
				TaskRoleArn:      "{{ssm:/td/task-role}}",
				ContainerDefinitions: []ContainerDefinition{
					{
						Image: "{{ssm:/td/image}}",
						Environment: []KeyValuePair{
							{Value: "{{ssm:/td/env}}"},
						},
						Secrets: []Secret{
							{ValueFrom: "{{ssm:/td/secret}}"},
						},
						LogConfiguration: &LogConfiguration{
							Options: map[string]string{
								"key": "{{ssm:/td/log-opt}}",
							},
						},
					},
				},
			},
			"merged": {
				Type:    "merged",
				BaseArn: "{{ssm:/merged/base}}",
				Overrides: &TaskDefOverrides{
					ExecutionRoleArn: "{{ssm:/merged/exec-role}}",
					ContainerDefinitions: []ContainerOverride{
						{
							Image: "{{ssm:/merged/image}}",
							Environment: []KeyValuePair{
								{Value: "{{ssm:/merged/env}}"},
							},
						},
					},
				},
			},
			"remote": {
				Type: "remote",
				Arn:  "{{ssm:/remote/arn}}",
			},
		},
		Services: map[string]Service{
			"web": {
				Cluster: "{{ssm:/svc/cluster}}",
				NetworkConfiguration: &NetworkConfiguration{
					Subnets:        []string{"{{ssm:/svc/subnet}}"},
					SecurityGroups: []string{"{{ssm:/svc/sg}}"},
				},
				LoadBalancers: []LoadBalancer{
					{TargetGroupArn: "{{ssm:/svc/tg}}"},
				},
			},
		},
		ScheduledTasks: map[string]ScheduledTask{
			"cron": {
				Cluster: "{{ssm:/st/cluster}}",
				Group:   "{{ssm:/st/group}}",
				NetworkConfiguration: &NetworkConfiguration{
					Subnets: []string{"{{ssm:/st/subnet}}"},
				},
				Overrides: &TaskOverrides{
					TaskRoleArn:      "{{ssm:/st/task-role}}",
					ExecutionRoleArn: "{{ssm:/st/exec-role}}",
				},
				Tags: []Tag{
					{Key: "env", Value: "{{ssm:/st/tag-env}}"},
				},
				DeadLetterConfig: &DeadLetterConfig{
					Arn: "{{ssm:/st/dlq}}",
				},
			},
		},
	}

	refs := collectSSMReferences(manifest)

	expectedRefs := []string{
		"/td/exec-role", "/td/task-role", "/td/image", "/td/env", "/td/secret", "/td/log-opt",
		"/merged/base", "/merged/exec-role", "/merged/image", "/merged/env",
		"/remote/arn",
		"/svc/cluster", "/svc/subnet", "/svc/sg", "/svc/tg",
		"/st/cluster", "/st/group", "/st/subnet", "/st/task-role", "/st/exec-role", "/st/tag-env", "/st/dlq",
	}

	refMap := make(map[string]bool)
	for _, r := range refs {
		refMap[r] = true
	}

	for _, expected := range expectedRefs {
		if !refMap[expected] {
			t.Errorf("expected reference %q not found", expected)
		}
	}

	if len(refs) != len(expectedRefs) {
		t.Errorf("expected %d references, got %d", len(expectedRefs), len(refs))
	}
}

func TestReplaceInString(t *testing.T) {
	values := map[string]string{
		"/param1": "value1",
		"/param2": "value2",
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"no refs", "no refs"},
		{"{{ssm:/param1}}", "value1"},
		{"prefix-{{ssm:/param1}}", "prefix-value1"},
		{"{{ssm:/param1}}-suffix", "value1-suffix"},
		{"{{ssm:/param1}}-{{ssm:/param2}}", "value1-value2"},
		{"{{ssm:/unknown}}", "{{ssm:/unknown}}"}, // Unknown refs stay unchanged
		{"mixed {{ssm:/param1}} text", "mixed value1 text"},
	}

	for _, tt := range tests {
		result := replaceInString(tt.input, values)
		if result != tt.expected {
			t.Errorf("replaceInString(%q) = %q, expected %q", tt.input, result, tt.expected)
		}
	}
}
