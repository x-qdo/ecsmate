package config

import (
	"testing"
)

func TestManagedSecrets_BuildARNMap(t *testing.T) {
	ms := &ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": "secret123",
			"api_key":     "key456",
		},
		SSMPrefix: "/myapp/prod",
		Region:    "us-east-1",
		AccountID: "123456789012",
	}

	arns := ms.BuildARNMap()

	if len(arns) != 2 {
		t.Fatalf("expected 2 ARNs, got %d", len(arns))
	}

	expectedDBArn := "arn:aws:ssm:us-east-1:123456789012:parameter/myapp/prod/db_password"
	if arns["db_password"] != expectedDBArn {
		t.Errorf("expected db_password ARN %s, got %s", expectedDBArn, arns["db_password"])
	}

	expectedAPIArn := "arn:aws:ssm:us-east-1:123456789012:parameter/myapp/prod/api_key"
	if arns["api_key"] != expectedAPIArn {
		t.Errorf("expected api_key ARN %s, got %s", expectedAPIArn, arns["api_key"])
	}
}

func TestManagedSecrets_BuildARNMap_NilManaged(t *testing.T) {
	var ms *ManagedSecrets = nil
	arns := ms.BuildARNMap()

	if arns != nil {
		t.Errorf("expected nil for nil managed secrets, got %v", arns)
	}
}

func TestManagedSecrets_BuildARNMap_EmptyDecrypted(t *testing.T) {
	ms := &ManagedSecrets{
		Decrypted: map[string]string{},
		SSMPrefix: "/myapp/prod",
		Region:    "us-east-1",
		AccountID: "123456789012",
	}

	arns := ms.BuildARNMap()

	if arns != nil {
		t.Errorf("expected nil for empty decrypted, got %v", arns)
	}
}

func TestManagedSecrets_BuildARNMap_DifferentRegion(t *testing.T) {
	ms := &ManagedSecrets{
		Decrypted: map[string]string{
			"secret": "value",
		},
		SSMPrefix: "/app",
		Region:    "eu-west-1",
		AccountID: "987654321012",
	}

	arns := ms.BuildARNMap()

	expectedArn := "arn:aws:ssm:eu-west-1:987654321012:parameter/app/secret"
	if arns["secret"] != expectedArn {
		t.Errorf("expected ARN %s, got %s", expectedArn, arns["secret"])
	}
}

func TestManagedSecretsKMSRegion_UsesConfiguredKeyRegion(t *testing.T) {
	cfg := &ManagedSecretsConfig{
		KMSKeyRegion: "us-east-1",
	}

	region := managedSecretsKMSRegion(cfg, "eu-west-1")
	if region != "us-east-1" {
		t.Fatalf("expected configured KMS key region us-east-1, got %q", region)
	}
}

func TestManagedSecretsKMSRegion_DefaultsToDeploymentRegion(t *testing.T) {
	cfg := &ManagedSecretsConfig{}

	region := managedSecretsKMSRegion(cfg, "eu-west-1")
	if region != "eu-west-1" {
		t.Fatalf("expected deployment region eu-west-1, got %q", region)
	}
}

func TestResolveManagedSecrets_NilManaged(t *testing.T) {
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				Name: "web",
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "DB_PASS", ValueFrom: "db_password"},
						},
					},
				},
			},
		},
	}

	manifest.ResolveManagedSecrets(nil)

	if manifest.TaskDefinitions["web"].ContainerDefinitions[0].Secrets[0].ValueFrom != "db_password" {
		t.Error("secrets should remain unchanged when managed is nil")
	}
}

func TestResolveManagedSecrets_ReplacesKeyWithArn(t *testing.T) {
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				Name: "web",
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "DB_PASSWORD", ValueFrom: "db_password"},
							{Name: "API_KEY", ValueFrom: "api_key"},
						},
					},
				},
			},
		},
	}

	managed := &ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": "secret123",
			"api_key":     "key456",
		},
		SSMPrefix: "/myapp",
		Region:    "us-east-1",
		AccountID: "123456789012",
	}

	manifest.ResolveManagedSecrets(managed)

	secrets := manifest.TaskDefinitions["web"].ContainerDefinitions[0].Secrets

	expectedDBArn := "arn:aws:ssm:us-east-1:123456789012:parameter/myapp/db_password"
	if secrets[0].ValueFrom != expectedDBArn {
		t.Errorf("expected DB_PASSWORD to be resolved to ARN %s, got %s", expectedDBArn, secrets[0].ValueFrom)
	}

	expectedAPIArn := "arn:aws:ssm:us-east-1:123456789012:parameter/myapp/api_key"
	if secrets[1].ValueFrom != expectedAPIArn {
		t.Errorf("expected API_KEY to be resolved to ARN %s, got %s", expectedAPIArn, secrets[1].ValueFrom)
	}
}

func TestResolveManagedSecrets_KeepsExistingArns(t *testing.T) {
	externalArn := "arn:aws:secretsmanager:us-east-1:123:secret:external"
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				Name: "web",
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "EXTERNAL_SECRET", ValueFrom: externalArn},
						},
					},
				},
			},
		},
	}

	managed := &ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": "secret123",
		},
		SSMPrefix: "/myapp",
		Region:    "us-east-1",
		AccountID: "123456789012",
	}

	manifest.ResolveManagedSecrets(managed)

	secrets := manifest.TaskDefinitions["web"].ContainerDefinitions[0].Secrets
	if secrets[0].ValueFrom != externalArn {
		t.Errorf("external ARN should remain unchanged, got %s", secrets[0].ValueFrom)
	}
}

func TestResolveManagedSecrets_KeepsUnknownKeys(t *testing.T) {
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				Name: "web",
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "UNKNOWN", ValueFrom: "unknown_key"},
						},
					},
				},
			},
		},
	}

	managed := &ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": "secret123",
		},
		SSMPrefix: "/myapp",
		Region:    "us-east-1",
		AccountID: "123456789012",
	}

	manifest.ResolveManagedSecrets(managed)

	secrets := manifest.TaskDefinitions["web"].ContainerDefinitions[0].Secrets
	if secrets[0].ValueFrom != "unknown_key" {
		t.Errorf("unknown key should remain unchanged, got %s", secrets[0].ValueFrom)
	}
}

func TestResolveManagedSecrets_MultipleContainers(t *testing.T) {
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				Name: "web",
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "DB_PASSWORD", ValueFrom: "db_password"},
						},
					},
					{
						Name: "sidecar",
						Secrets: []Secret{
							{Name: "API_KEY", ValueFrom: "api_key"},
						},
					},
				},
			},
		},
	}

	managed := &ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": "secret123",
			"api_key":     "key456",
		},
		SSMPrefix: "/myapp",
		Region:    "us-east-1",
		AccountID: "123456789012",
	}

	manifest.ResolveManagedSecrets(managed)

	containers := manifest.TaskDefinitions["web"].ContainerDefinitions

	expectedDBArn := "arn:aws:ssm:us-east-1:123456789012:parameter/myapp/db_password"
	if containers[0].Secrets[0].ValueFrom != expectedDBArn {
		t.Errorf("expected first container secret to be resolved, got %s", containers[0].Secrets[0].ValueFrom)
	}

	expectedAPIArn := "arn:aws:ssm:us-east-1:123456789012:parameter/myapp/api_key"
	if containers[1].Secrets[0].ValueFrom != expectedAPIArn {
		t.Errorf("expected second container secret to be resolved, got %s", containers[1].Secrets[0].ValueFrom)
	}
}

func TestResolveManagedSecrets_MultipleTaskDefinitions(t *testing.T) {
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				Name: "web",
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "DB_PASSWORD", ValueFrom: "db_password"},
						},
					},
				},
			},
			"worker": {
				Name: "worker",
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "worker",
						Secrets: []Secret{
							{Name: "QUEUE_PASS", ValueFrom: "db_password"},
						},
					},
				},
			},
		},
	}

	managed := &ManagedSecrets{
		Decrypted: map[string]string{
			"db_password": "secret123",
		},
		SSMPrefix: "/myapp",
		Region:    "us-east-1",
		AccountID: "123456789012",
	}

	manifest.ResolveManagedSecrets(managed)

	expectedArn := "arn:aws:ssm:us-east-1:123456789012:parameter/myapp/db_password"

	webSecrets := manifest.TaskDefinitions["web"].ContainerDefinitions[0].Secrets
	if webSecrets[0].ValueFrom != expectedArn {
		t.Errorf("expected web task secret to be resolved, got %s", webSecrets[0].ValueFrom)
	}

	workerSecrets := manifest.TaskDefinitions["worker"].ContainerDefinitions[0].Secrets
	if workerSecrets[0].ValueFrom != expectedArn {
		t.Errorf("expected worker task secret to be resolved, got %s", workerSecrets[0].ValueFrom)
	}
}

func TestValidateSecretReferences_AllValid(t *testing.T) {
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "DB_PASS", ValueFrom: "arn:aws:ssm:us-east-1:123:parameter/myapp/db_password"},
							{Name: "API_KEY", ValueFrom: "arn:aws:secretsmanager:us-east-1:123:secret:api"},
						},
					},
				},
			},
		},
	}

	errors := manifest.ValidateSecretReferences()
	if len(errors) != 0 {
		t.Errorf("expected no errors for valid ARNs, got: %v", errors)
	}
}

func TestValidateSecretReferences_BareNameFails(t *testing.T) {
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "KREDINOR_CI_PASSWORD", ValueFrom: "KREDINOR_CI_PASSWORD"},
						},
					},
				},
			},
		},
	}

	errors := manifest.ValidateSecretReferences()
	if len(errors) != 1 {
		t.Errorf("expected 1 error for bare name, got %d: %v", len(errors), errors)
	}
}

func TestValidateSecretReferences_MixedValidAndInvalid(t *testing.T) {
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "DB_PASS", ValueFrom: "arn:aws:ssm:us-east-1:123:parameter/x"},
							{Name: "BAD_ONE", ValueFrom: "not_an_arn"},
							{Name: "BAD_TWO", ValueFrom: "/root/level/param"},
						},
					},
				},
			},
		},
	}

	errors := manifest.ValidateSecretReferences()
	if len(errors) != 2 {
		t.Errorf("expected 2 errors, got %d: %v", len(errors), errors)
	}
}

func TestValidateSecretReferences_NoSecrets(t *testing.T) {
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				ContainerDefinitions: []ContainerDefinition{
					{Name: "app"},
				},
			},
		},
	}

	errors := manifest.ValidateSecretReferences()
	if len(errors) != 0 {
		t.Errorf("expected no errors for no secrets, got: %v", errors)
	}
}

func TestValidateSecretReferencesOffline_WithManagedSecrets_AllowsBareKeys(t *testing.T) {
	manifest := &Manifest{
		Secrets: &SecretsConfig{
			Managed: &ManagedSecretsConfig{
				File:      "secrets.enc.yaml",
				KMSKeyArn: "arn:aws:kms:us-east-1:123:key/abc",
				SSMPrefix: "/myapp/prod",
			},
		},
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "DB_PASS", ValueFrom: "db_password"},
							{Name: "API_KEY", ValueFrom: "arn:aws:ssm:us-east-1:123:parameter/x"},
						},
					},
				},
			},
		},
	}

	errors := manifest.ValidateSecretReferencesOffline()
	if len(errors) != 0 {
		t.Errorf("bare key names should be allowed with managed secrets, got: %v", errors)
	}
}

func TestValidateSecretReferencesOffline_WithManagedSecrets_FlagsSuspiciousValues(t *testing.T) {
	manifest := &Manifest{
		Secrets: &SecretsConfig{
			Managed: &ManagedSecretsConfig{
				File:      "secrets.enc.yaml",
				KMSKeyArn: "arn:aws:kms:us-east-1:123:key/abc",
				SSMPrefix: "/myapp/prod",
			},
		},
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "BAD", ValueFrom: "/KREDINOR_CI_PASSWORD"},
						},
					},
				},
			},
		},
	}

	errors := manifest.ValidateSecretReferencesOffline()
	if len(errors) != 1 {
		t.Errorf("path-like value should be flagged with managed secrets, got %d: %v", len(errors), errors)
	}
}

func TestValidateSecretReferencesOffline_NoManagedSecrets_RequiresArns(t *testing.T) {
	manifest := &Manifest{
		TaskDefinitions: map[string]TaskDefinition{
			"web": {
				ContainerDefinitions: []ContainerDefinition{
					{
						Name: "app",
						Secrets: []Secret{
							{Name: "GOOD", ValueFrom: "arn:aws:ssm:us-east-1:123:parameter/x"},
							{Name: "BAD", ValueFrom: "KREDINOR_CI_PASSWORD"},
						},
					},
				},
			},
		},
	}

	errors := manifest.ValidateSecretReferencesOffline()
	if len(errors) != 1 {
		t.Errorf("bare name without managed secrets should be an error, got %d: %v", len(errors), errors)
	}
}

func TestIsBareKeyName(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{"db_password", true},
		{"API_KEY", true},
		{"my-secret.key", true},
		{"/path/to/param", false},
		{"arn:aws:ssm:us-east-1:123:parameter/x", false},
		{"", false},
	}

	for _, tt := range tests {
		result := isBareKeyName(tt.value)
		if result != tt.expected {
			t.Errorf("isBareKeyName(%q) = %v, want %v", tt.value, result, tt.expected)
		}
	}
}
