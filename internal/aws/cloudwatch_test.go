package aws

import (
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
)

func TestLogGroupTagResourceArn(t *testing.T) {
	tests := []struct {
		name     string
		logGroup types.LogGroup
		want     string
	}{
		{
			name: "uses LogGroupArn when available",
			logGroup: types.LogGroup{
				Arn:         awssdk.String("arn:aws:logs:eu-west-1:123456789012:log-group:/ecs/app:*"),
				LogGroupArn: awssdk.String("arn:aws:logs:eu-west-1:123456789012:log-group:/ecs/app"),
			},
			want: "arn:aws:logs:eu-west-1:123456789012:log-group:/ecs/app",
		},
		{
			name: "strips wildcard suffix from Arn fallback",
			logGroup: types.LogGroup{
				Arn: awssdk.String("arn:aws:logs:eu-west-1:123456789012:log-group:/ecs/app:*"),
			},
			want: "arn:aws:logs:eu-west-1:123456789012:log-group:/ecs/app",
		},
		{
			name: "keeps already tag-compatible Arn fallback",
			logGroup: types.LogGroup{
				Arn: awssdk.String("arn:aws:logs:eu-west-1:123456789012:log-group:/ecs/app"),
			},
			want: "arn:aws:logs:eu-west-1:123456789012:log-group:/ecs/app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := logGroupTagResourceArn(&tt.logGroup)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLogGroupTagResourceArnRejectsMissingArn(t *testing.T) {
	if _, err := logGroupTagResourceArn(&types.LogGroup{}); err == nil {
		t.Fatal("expected missing ARN to return an error")
	}
}
