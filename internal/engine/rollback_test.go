package engine

import (
	"testing"
)

func TestParseTaskDefArn(t *testing.T) {
	tests := []struct {
		arn            string
		expectedFamily string
		expectedRev    int
	}{
		{
			"arn:aws:ecs:us-east-1:123456789:task-definition/my-app-php:42",
			"my-app-php",
			42,
		},
		{
			"arn:aws:ecs:us-east-1:123456789:task-definition/my-app-php",
			"my-app-php",
			0,
		},
		{
			"my-app-php:42",
			"my-app-php",
			42,
		},
		{
			"my-app-php",
			"my-app-php",
			0,
		},
		{
			"complex-name-with-dashes:123",
			"complex-name-with-dashes",
			123,
		},
		{
			"family-with:invalid:colons:5",
			"family-with:invalid:colons",
			5,
		},
	}

	for _, tt := range tests {
		family, rev := parseTaskDefArn(tt.arn)
		if family != tt.expectedFamily {
			t.Errorf("parseTaskDefArn(%q) family = %q, expected %q", tt.arn, family, tt.expectedFamily)
		}
		if rev != tt.expectedRev {
			t.Errorf("parseTaskDefArn(%q) revision = %d, expected %d", tt.arn, rev, tt.expectedRev)
		}
	}
}

func TestRollbackResult(t *testing.T) {
	result := &RollbackResult{
		ServiceName:     "web",
		PreviousTaskDef: "my-app:41",
		TargetTaskDef:   "my-app:40",
		TargetRevision:  40,
		Success:         true,
		Message:         "rolled back successfully",
	}

	if result.ServiceName != "web" {
		t.Error("ServiceName not set correctly")
	}

	if result.TargetRevision != 40 {
		t.Errorf("TargetRevision = %d, expected 40", result.TargetRevision)
	}

	if !result.Success {
		t.Error("Success should be true")
	}
}

func TestRevisionInfo(t *testing.T) {
	info := RevisionInfo{
		Arn:       "arn:aws:ecs:us-east-1:123:task-definition/my-app:42",
		Revision:  42,
		IsCurrent: true,
	}

	if info.Revision != 42 {
		t.Errorf("Revision = %d, expected 42", info.Revision)
	}

	if !info.IsCurrent {
		t.Error("IsCurrent should be true")
	}
}

func TestRelativeRevisionCalculation(t *testing.T) {
	// Test relative revision calculation logic
	currentRev := 42

	tests := []struct {
		inputRev  int
		expected  int
	}{
		{-1, 41},  // Previous revision
		{-2, 40},  // Two revisions back
		{-5, 37},  // Five revisions back
		{40, 40},  // Absolute revision (positive)
		{42, 42},  // Same revision
	}

	for _, tt := range tests {
		var targetRev int
		if tt.inputRev < 0 {
			targetRev = currentRev + tt.inputRev
		} else {
			targetRev = tt.inputRev
		}

		if targetRev != tt.expected {
			t.Errorf("with current=%d, input=%d: target=%d, expected=%d",
				currentRev, tt.inputRev, targetRev, tt.expected)
		}
	}
}
