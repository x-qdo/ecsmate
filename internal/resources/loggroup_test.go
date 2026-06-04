package resources

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"

	awsclient "github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/config"
)

type mockLogGroupClient struct {
	logGroups           map[string]*types.LogGroup
	tags                map[string]map[string]string
	subscriptionFilters map[string][]types.SubscriptionFilter

	calls                 []string
	putSubscriptionInputs []awsclient.PutSubscriptionFilterInput
	deletedSubscriptions  []string
}

func newMockLogGroupClient() *mockLogGroupClient {
	return &mockLogGroupClient{
		logGroups:           make(map[string]*types.LogGroup),
		tags:                make(map[string]map[string]string),
		subscriptionFilters: make(map[string][]types.SubscriptionFilter),
	}
}

func (m *mockLogGroupClient) CreateLogGroup(ctx context.Context, input *awsclient.CreateLogGroupInput) error {
	m.calls = append(m.calls, "create-log-group:"+input.Name)
	m.logGroups[input.Name] = &types.LogGroup{LogGroupName: aws.String(input.Name)}
	m.tags[input.Name] = cloneStringMap(input.Tags)
	return nil
}

func (m *mockLogGroupClient) SetRetentionPolicy(ctx context.Context, logGroupName string, retentionDays int) error {
	m.calls = append(m.calls, "set-retention:"+logGroupName)
	return nil
}

func (m *mockLogGroupClient) DescribeLogGroup(ctx context.Context, logGroupName string) (*types.LogGroup, error) {
	m.calls = append(m.calls, "describe-log-group:"+logGroupName)
	return m.logGroups[logGroupName], nil
}

func (m *mockLogGroupClient) DeleteLogGroup(ctx context.Context, logGroupName string) error {
	m.calls = append(m.calls, "delete-log-group:"+logGroupName)
	delete(m.logGroups, logGroupName)
	return nil
}

func (m *mockLogGroupClient) TagLogGroup(ctx context.Context, logGroupName string, tags map[string]string) error {
	m.calls = append(m.calls, "tag-log-group:"+logGroupName)
	if m.tags[logGroupName] == nil {
		m.tags[logGroupName] = make(map[string]string)
	}
	for k, v := range tags {
		m.tags[logGroupName][k] = v
	}
	return nil
}

func (m *mockLogGroupClient) ListLogGroupTags(ctx context.Context, logGroupName string) (map[string]string, error) {
	m.calls = append(m.calls, "list-tags:"+logGroupName)
	return cloneStringMap(m.tags[logGroupName]), nil
}

func (m *mockLogGroupClient) DescribeSubscriptionFilters(ctx context.Context, logGroupName string) ([]types.SubscriptionFilter, error) {
	m.calls = append(m.calls, "describe-subscriptions:"+logGroupName)
	return m.subscriptionFilters[logGroupName], nil
}

func (m *mockLogGroupClient) PutSubscriptionFilter(ctx context.Context, input *awsclient.PutSubscriptionFilterInput) error {
	m.calls = append(m.calls, "put-subscription:"+input.LogGroupName+"/"+input.Name)
	m.putSubscriptionInputs = append(m.putSubscriptionInputs, *input)
	return nil
}

func (m *mockLogGroupClient) DeleteSubscriptionFilter(ctx context.Context, logGroupName, filterName string) error {
	m.calls = append(m.calls, "delete-subscription:"+logGroupName+"/"+filterName)
	m.deletedSubscriptions = append(m.deletedSubscriptions, logGroupName+"/"+filterName)
	return nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func TestExtractLogGroups_SubscriptionFilters(t *testing.T) {
	manifest := &config.Manifest{
		TaskDefinitions: map[string]config.TaskDefinition{
			"web": {
				ContainerDefinitions: []config.ContainerDefinition{
					{
						Name: "app",
						LogConfiguration: &config.LogConfiguration{
							LogDriver:       "awslogs",
							CreateLogGroup:  true,
							RetentionInDays: 14,
							KMSKeyID:        "alias/logs",
							LogGroupTags:    map[string]string{"env": "test"},
							Options:         map[string]string{"awslogs-group": "/ecs/app"},
							SubscriptionFilters: []config.SubscriptionFilter{
								{
									Name:           "slack-error-forwarder",
									DestinationArn: "arn:aws:lambda:eu-west-1:123456789012:function:slack",
									FilterPattern:  "?ERROR ?Error ?error ?Exception ?CRITICAL ?Critical ?Fatal ?fatal",
								},
								{
									Name:           "audit-forwarder",
									DestinationArn: "arn:aws:logs:eu-west-1:123456789012:destination:audit",
									FilterPattern:  "",
									RoleArn:        "arn:aws:iam::123456789012:role/log-delivery",
									Distribution:   "Random",
								},
							},
						},
					},
					{
						Name: "sidecar",
						LogConfiguration: &config.LogConfiguration{
							LogDriver:      "awslogs",
							CreateLogGroup: true,
							Options:        map[string]string{"awslogs-group": "/ecs/app"},
							SubscriptionFilters: []config.SubscriptionFilter{
								{
									Name:           "slack-error-forwarder",
									DestinationArn: "arn:aws:lambda:eu-west-1:123456789012:function:different",
									FilterPattern:  "ignored duplicate",
								},
							},
						},
					},
					{
						Name: "disabled",
						LogConfiguration: &config.LogConfiguration{
							LogDriver:      "awslogs",
							CreateLogGroup: false,
							Options:        map[string]string{"awslogs-group": "/ecs/ignored"},
							SubscriptionFilters: []config.SubscriptionFilter{
								{Name: "ignored", DestinationArn: "arn:aws:lambda:eu-west-1:123456789012:function:ignored"},
							},
						},
					},
					{
						Name: "non-awslogs",
						LogConfiguration: &config.LogConfiguration{
							LogDriver:      "awsfirelens",
							CreateLogGroup: true,
							Options:        map[string]string{"awslogs-group": "/ecs/ignored-too"},
						},
					},
				},
			},
		},
	}

	logGroups := ExtractLogGroups(manifest)
	if len(logGroups) != 1 {
		t.Fatalf("expected 1 log group, got %d", len(logGroups))
	}

	spec := logGroups["/ecs/app"]
	if spec == nil {
		t.Fatal("expected /ecs/app log group")
	}
	if spec.RetentionInDays != 14 {
		t.Errorf("expected retention 14, got %d", spec.RetentionInDays)
	}
	if spec.KMSKeyID != "alias/logs" {
		t.Errorf("expected KMS key alias/logs, got %s", spec.KMSKeyID)
	}
	if spec.Tags["env"] != "test" {
		t.Errorf("expected env tag test, got %s", spec.Tags["env"])
	}

	if len(spec.SubscriptionFilters) != 2 {
		t.Fatalf("expected 2 subscription filters, got %d", len(spec.SubscriptionFilters))
	}
	if spec.SubscriptionFilters[0].Name != "slack-error-forwarder" {
		t.Errorf("expected first filter slack-error-forwarder, got %s", spec.SubscriptionFilters[0].Name)
	}
	if spec.SubscriptionFilters[0].DestinationArn != "arn:aws:lambda:eu-west-1:123456789012:function:slack" {
		t.Errorf("duplicate filter overwrote first destination: %s", spec.SubscriptionFilters[0].DestinationArn)
	}
	if spec.SubscriptionFilters[1].RoleArn == "" || spec.SubscriptionFilters[1].Distribution != "Random" {
		t.Errorf("expected role and Random distribution on second filter, got role=%q distribution=%q", spec.SubscriptionFilters[1].RoleArn, spec.SubscriptionFilters[1].Distribution)
	}
}

func TestLogGroupManagerApply_ReconcilesSubscriptionFiltersOnNoop(t *testing.T) {
	client := newMockLogGroupClient()
	client.logGroups["/ecs/app"] = &types.LogGroup{LogGroupName: aws.String("/ecs/app")}
	client.subscriptionFilters["/ecs/app"] = []types.SubscriptionFilter{
		{
			FilterName:     aws.String("matching"),
			DestinationArn: aws.String("arn:aws:lambda:eu-west-1:123456789012:function:slack"),
			FilterPattern:  aws.String("?ERROR"),
			Distribution:   types.DistributionByLogStream,
		},
		{
			FilterName:     aws.String("changed"),
			DestinationArn: aws.String("arn:aws:kinesis:eu-west-1:123456789012:stream:old"),
			FilterPattern:  aws.String("old"),
			Distribution:   types.DistributionByLogStream,
		},
		{
			FilterName:     aws.String("unrelated"),
			DestinationArn: aws.String("arn:aws:lambda:eu-west-1:123456789012:function:other"),
			FilterPattern:  aws.String(""),
		},
	}

	manager := NewLogGroupManager(client)
	spec := &LogGroupSpec{
		Name: "/ecs/app",
		SubscriptionFilters: []SubscriptionFilterSpec{
			{
				Name:           "matching",
				DestinationArn: "arn:aws:lambda:eu-west-1:123456789012:function:slack",
				FilterPattern:  "?ERROR",
			},
			{
				Name:           "changed",
				DestinationArn: "arn:aws:kinesis:eu-west-1:123456789012:stream:new",
				FilterPattern:  "?WARN",
				RoleArn:        "arn:aws:iam::123456789012:role/log-delivery",
				Distribution:   "Random",
			},
			{
				Name:           "missing",
				DestinationArn: "arn:aws:lambda:eu-west-1:123456789012:function:new-slack",
				FilterPattern:  "?Fatal",
			},
		},
	}

	resource, err := manager.BuildResource(context.Background(), spec)
	if err != nil {
		t.Fatalf("failed to build log group resource: %v", err)
	}
	if resource.Action != LogGroupActionNoop {
		t.Fatalf("expected NOOP log group action, got %s", resource.Action)
	}

	if err := manager.Apply(context.Background(), resource); err != nil {
		t.Fatalf("failed to apply log group resource: %v", err)
	}

	if len(client.putSubscriptionInputs) != 2 {
		t.Fatalf("expected 2 put subscription calls, got %d: %#v", len(client.putSubscriptionInputs), client.putSubscriptionInputs)
	}
	if client.putSubscriptionInputs[0].Name != "changed" {
		t.Errorf("expected changed filter to be updated first, got %s", client.putSubscriptionInputs[0].Name)
	}
	if client.putSubscriptionInputs[0].RoleArn == "" {
		t.Error("expected role ARN to be passed for changed filter")
	}
	if client.putSubscriptionInputs[0].Distribution != "Random" {
		t.Errorf("expected Random distribution, got %s", client.putSubscriptionInputs[0].Distribution)
	}
	if client.putSubscriptionInputs[1].Name != "missing" {
		t.Errorf("expected missing filter to be created second, got %s", client.putSubscriptionInputs[1].Name)
	}
	if client.putSubscriptionInputs[1].RoleArn != "" {
		t.Errorf("expected Lambda subscription to remain role-less, got %s", client.putSubscriptionInputs[1].RoleArn)
	}
	if len(client.deletedSubscriptions) != 0 {
		t.Fatalf("expected no delete calls for unrelated filters, got %v", client.deletedSubscriptions)
	}
	expectedManagedTag := mustEncodeManagedSubscriptionFilterNames(t, []string{"changed", "matching", "missing"})
	if client.tags["/ecs/app"][managedSubscriptionFiltersTag] != expectedManagedTag {
		t.Fatalf("unexpected managed subscription tag: %q", client.tags["/ecs/app"][managedSubscriptionFiltersTag])
	}
}

func TestLogGroupManagerApply_CreatesSubscriptionFiltersAfterLogGroupCreate(t *testing.T) {
	client := newMockLogGroupClient()
	manager := NewLogGroupManager(client)
	resource := &LogGroupResource{
		Name: "/ecs/new",
		Desired: &LogGroupSpec{
			Name: "/ecs/new",
			SubscriptionFilters: []SubscriptionFilterSpec{
				{
					Name:           "slack-error-forwarder",
					DestinationArn: "arn:aws:lambda:eu-west-1:123456789012:function:slack",
					FilterPattern:  "?ERROR ?Error ?error ?Exception ?CRITICAL ?Critical ?Fatal ?fatal",
				},
			},
		},
		Action: LogGroupActionCreate,
	}

	if err := manager.Apply(context.Background(), resource); err != nil {
		t.Fatalf("failed to apply log group resource: %v", err)
	}

	expectedCalls := []string{
		"create-log-group:/ecs/new",
		"describe-subscriptions:/ecs/new",
		"put-subscription:/ecs/new/slack-error-forwarder",
		"tag-log-group:/ecs/new",
	}
	if !reflect.DeepEqual(client.calls, expectedCalls) {
		t.Fatalf("unexpected calls:\n got: %#v\nwant: %#v", client.calls, expectedCalls)
	}
	expectedManagedTag := mustEncodeManagedSubscriptionFilterNames(t, []string{"slack-error-forwarder"})
	if client.tags["/ecs/new"][managedSubscriptionFiltersTag] != expectedManagedTag {
		t.Fatalf("unexpected managed subscription tag: %q", client.tags["/ecs/new"][managedSubscriptionFiltersTag])
	}
}

func TestLogGroupManagerApply_DeletesPreviouslyManagedSubscriptionFilters(t *testing.T) {
	client := newMockLogGroupClient()
	client.logGroups["/ecs/app"] = &types.LogGroup{LogGroupName: aws.String("/ecs/app")}
	client.tags["/ecs/app"] = map[string]string{
		managedSubscriptionFiltersTag: mustEncodeManagedSubscriptionFilterNames(t, []string{"removed", "already-gone"}),
	}
	client.subscriptionFilters["/ecs/app"] = []types.SubscriptionFilter{
		{
			FilterName:     aws.String("removed"),
			DestinationArn: aws.String("arn:aws:lambda:eu-west-1:123456789012:function:old"),
			FilterPattern:  aws.String("?ERROR"),
		},
		{
			FilterName:     aws.String("unrelated"),
			DestinationArn: aws.String("arn:aws:lambda:eu-west-1:123456789012:function:other"),
		},
	}

	manager := NewLogGroupManager(client)
	spec := &LogGroupSpec{Name: "/ecs/app"}

	resource, err := manager.BuildResource(context.Background(), spec)
	if err != nil {
		t.Fatalf("failed to build log group resource: %v", err)
	}
	if resource.Action != LogGroupActionNoop {
		t.Fatalf("expected NOOP log group action, got %s", resource.Action)
	}

	needsReconcile, err := manager.NeedsSubscriptionReconcile(context.Background(), resource)
	if err != nil {
		t.Fatalf("failed to check subscription reconcile need: %v", err)
	}
	if !needsReconcile {
		t.Fatal("expected previously managed subscription filters to require reconciliation")
	}

	if err := manager.Apply(context.Background(), resource); err != nil {
		t.Fatalf("failed to apply log group resource: %v", err)
	}

	if !reflect.DeepEqual(client.deletedSubscriptions, []string{"/ecs/app/removed"}) {
		t.Fatalf("unexpected deleted subscriptions: %#v", client.deletedSubscriptions)
	}
	expectedManagedTag := mustEncodeManagedSubscriptionFilterNames(t, nil)
	if client.tags["/ecs/app"][managedSubscriptionFiltersTag] != expectedManagedTag {
		t.Fatalf("expected managed subscription tag to be cleared, got %q", client.tags["/ecs/app"][managedSubscriptionFiltersTag])
	}
}

func TestLogGroupManagerCreate_FiltersReservedStateTagFromUserTags(t *testing.T) {
	client := newMockLogGroupClient()
	manager := NewLogGroupManager(client)
	resource := &LogGroupResource{
		Name: "/ecs/new",
		Desired: &LogGroupSpec{
			Name: "/ecs/new",
			Tags: map[string]string{
				"env":                         "test",
				managedSubscriptionFiltersTag: `["not-owned"]`,
			},
		},
		Action: LogGroupActionCreate,
	}

	if err := manager.Apply(context.Background(), resource); err != nil {
		t.Fatalf("failed to apply log group resource: %v", err)
	}

	if client.tags["/ecs/new"]["env"] != "test" {
		t.Fatalf("expected user tag env=test, got %q", client.tags["/ecs/new"]["env"])
	}
	if _, exists := client.tags["/ecs/new"][managedSubscriptionFiltersTag]; exists {
		t.Fatalf("reserved state tag should not be copied from user tags: %#v", client.tags["/ecs/new"])
	}
}

func TestLogGroupManagerBuildResource_RejectsInvalidTagValueBeforeDiscovery(t *testing.T) {
	client := newMockLogGroupClient()
	manager := NewLogGroupManager(client)
	spec := &LogGroupSpec{
		Name: "/ecs/app",
		Tags: map[string]string{
			"bad": `["json-is-not-a-valid-cloudwatch-tag-value"]`,
		},
	}

	_, err := manager.BuildResource(context.Background(), spec)
	if err == nil {
		t.Fatal("expected invalid tag value to fail validation")
	}
	if !strings.Contains(err.Error(), "CloudWatch Logs tag character constraints") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(client.calls) != 0 {
		t.Fatalf("expected no AWS discovery calls before tag validation, got %#v", client.calls)
	}
}

func TestParseManagedSubscriptionFilterNames_RequiresEncodedTagValue(t *testing.T) {
	parsed := parseManagedSubscriptionFilterNames(map[string]string{
		managedSubscriptionFiltersTag: `["raw-json-is-invalid"]`,
	})
	if len(parsed) != 0 {
		t.Fatalf("raw JSON state must not be parsed, got %#v", parsed)
	}
}

func TestValidateLogGroupTags(t *testing.T) {
	if err := validateLogGroupTags(map[string]string{"ok": "Letters numbers 123 _.:/=+-@"}); err != nil {
		t.Fatalf("expected valid CloudWatch tag to pass: %v", err)
	}
	if err := validateLogGroupTags(map[string]string{"bad": `["json"]`}); err == nil {
		t.Fatal("expected JSON-looking tag value to fail validation")
	}
	if err := validateLogGroupTags(map[string]string{"": "value"}); err == nil {
		t.Fatal("expected empty tag key to fail validation")
	}
}

func mustEncodeManagedSubscriptionFilterNames(t *testing.T, names []string) string {
	t.Helper()

	value, err := encodeManagedSubscriptionFilterNames(names)
	if err != nil {
		t.Fatalf("failed to encode managed subscription filter names: %v", err)
	}
	return value
}
