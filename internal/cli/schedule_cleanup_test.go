package cli

import (
	"context"
	"testing"

	"github.com/x-qdo/ecsmate/internal/config"
)

func TestValidateManifestContent_ScheduleCleanup(t *testing.T) {
	m := &config.Manifest{Name: "app", ScheduleCleanupPrefix: "other-"}
	if len(validateManifestContent(m)) == 0 {
		t.Fatal("wrong cleanup namespace must fail offline validation")
	}
	m.ScheduleCleanupPrefix = "app-stage_"
	if errs := validateManifestContent(m); len(errs) != 0 {
		t.Fatalf("valid cleanup scope: %v", errs)
	}
}

func TestInitAWSClients_CleanupWithoutTasks(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	for _, prefix := range []string{"", "app-stage_"} {
		clients, err := initAWSClients(context.Background(), &GlobalOptions{Region: "eu-west-1"}, &config.Manifest{Name: "app", ScheduleCleanupPrefix: prefix})
		if err != nil {
			t.Fatal(err)
		}
		if (clients.Scheduler != nil) != (prefix != "") {
			t.Fatalf("incorrect Scheduler initialization for prefix %q", prefix)
		}
		if clients.IAM != nil {
			t.Fatal("cleanup-only does not need an IAM client")
		}
	}
}
