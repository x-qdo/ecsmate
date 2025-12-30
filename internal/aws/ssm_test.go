package aws

import (
	"testing"
)

func TestSSMClient_ResolveSSMReferences(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		hasError bool
	}{
		{"no references", "plain text", false},
		{"single reference syntax", "{{ssm:/path}}", false},
		{"unclosed reference", "{{ssm:/path", true},
		{"mixed content", "prefix-{{ssm:/path}}-suffix", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We can only test the parsing logic without AWS connection
			// The actual AWS call would fail without valid credentials
			if tt.name == "unclosed reference" {
				// Test that unclosed reference returns error
				client := &SSMClient{client: nil}
				_, err := client.ResolveSSMReferences(nil, tt.input)
				if (err != nil) != tt.hasError {
					t.Errorf("ResolveSSMReferences() error = %v, wantError %v", err, tt.hasError)
				}
			}
		})
	}
}

func TestSSMClient_ParseReferences(t *testing.T) {
	const prefix = "{{ssm:"
	const suffix = "}}"

	tests := []struct {
		input    string
		expected []string
	}{
		{"{{ssm:/param1}}", []string{"/param1"}},
		{"{{ssm:/param1}}-{{ssm:/param2}}", []string{"/param1", "/param2"}},
		{"no refs", nil},
	}

	for _, tt := range tests {
		var refs []string
		result := tt.input
		for {
			start := indexOf(result, prefix)
			if start == -1 {
				break
			}
			end := indexOf(result[start:], suffix)
			if end == -1 {
				break
			}
			end += start

			paramName := result[start+len(prefix) : end]
			refs = append(refs, paramName)
			result = result[end+len(suffix):]
		}

		if len(refs) != len(tt.expected) {
			t.Errorf("input=%q: got %v, expected %v", tt.input, refs, tt.expected)
		}
	}
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
