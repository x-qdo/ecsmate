package aws

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/scheduler"
)

type schedulerHTTPClient func(*http.Request) (*http.Response, error)

func (f schedulerHTTPClient) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestSchedulerCleanupErrors(t *testing.T) {
	for _, operation := range []string{"list", "delete"} {
		for _, missing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/missing=%v", operation, missing), func(t *testing.T) {
				calls := 0
				client := &SchedulerClient{client: scheduler.NewFromConfig(aws.Config{
					Region: "eu-west-1",
					Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
						return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
					}),
					HTTPClient: schedulerHTTPClient(func(r *http.Request) (*http.Response, error) {
						calls++
						groupKey := "groupName"
						if operation == "list" {
							groupKey = "ScheduleGroup"
						}
						if r.URL.Query().Get(groupKey) != "app" {
							t.Fatalf("wrong group: %s", r.URL)
						}
						if operation == "list" && (r.Method != http.MethodGet || r.URL.Query().Get("NamePrefix") != "app-stage_") {
							t.Fatalf("wrong list scope: %s %s", r.Method, r.URL)
						}
						if operation == "delete" && (r.Method != http.MethodDelete || r.URL.Path != "/schedules/app-stage_old") {
							t.Fatalf("wrong delete: %s %s", r.Method, r.URL)
						}
						status, code := http.StatusForbidden, "AccessDeniedException"
						if missing {
							status, code = http.StatusNotFound, "ResourceNotFoundException"
						}
						return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}, "X-Amzn-Errortype": {code}}, Body: io.NopCloser(strings.NewReader(`{"Message":"test error"}`))}, nil
					}),
				})}
				var err error
				if operation == "list" {
					schedules, listErr := client.ListSchedules(context.Background(), "app", "app-stage_")
					err = listErr
					if len(schedules) != 0 {
						t.Fatalf("expected no schedules, got %v", schedules)
					}
				} else {
					err = client.DeleteSchedule(context.Background(), "app-stage_old", "app")
				}
				if (err == nil) != missing {
					t.Fatalf("missing=%v, err=%v", missing, err)
				}
				if calls != 1 {
					t.Fatalf("expected one request, got %d", calls)
				}
			})
		}
	}
}
