package resources

import "testing"

func TestIsTargetGroupOwnedByManifest(t *testing.T) {
	tests := []struct {
		name         string
		tgName       string
		manifestName string
		expected     bool
	}{
		{"owned by manifest", "myapp-r100", "myapp", true},
		{"owned by manifest with dashes", "my-app-r200", "my-app", true},
		{"not owned - different prefix", "otherapp-r100", "myapp", false},
		{"not owned - no -r suffix", "myapp-100", "myapp", false},
		{"not owned - r without number", "myapp-rabc", "myapp", false},
		{"empty tg name", "", "myapp", false},
		{"empty manifest name", "myapp-r100", "", false},
		{"both empty", "", "", false},
		{"partial match should fail", "myapp-r100-extra", "myapp", false},
		{"manifest name is prefix of tg", "myappservice-r100", "myapp", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isTargetGroupOwnedByManifest(tt.tgName, tt.manifestName)
			if result != tt.expected {
				t.Errorf("isTargetGroupOwnedByManifest(%q, %q) = %v, want %v",
					tt.tgName, tt.manifestName, result, tt.expected)
			}
		})
	}
}

func TestIsOwnedByEcsmate(t *testing.T) {
	tests := []struct {
		name     string
		tags     map[string]string
		expected bool
	}{
		{
			"owned with ManagedBy tag",
			map[string]string{TagKeyManagedBy: TagValueEcsmate},
			true,
		},
		{
			"owned with extra tags",
			map[string]string{TagKeyManagedBy: TagValueEcsmate, "Environment": "prod"},
			true,
		},
		{
			"not owned - different ManagedBy value",
			map[string]string{TagKeyManagedBy: "terraform"},
			false,
		},
		{
			"not owned - no ManagedBy tag",
			map[string]string{"Environment": "prod"},
			false,
		},
		{
			"not owned - nil tags",
			nil,
			false,
		},
		{
			"not owned - empty tags",
			map[string]string{},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isOwnedByEcsmate(tt.tags)
			if result != tt.expected {
				t.Errorf("isOwnedByEcsmate(%v) = %v, want %v", tt.tags, result, tt.expected)
			}
		})
	}
}
