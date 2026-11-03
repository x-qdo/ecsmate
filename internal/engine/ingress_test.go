package engine

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	awsclient "github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/config"
	"github.com/x-qdo/ecsmate/internal/resources"
)

// Unexpected calls, including all writes, panic through the nil interface.
type executorIngressFake struct {
	awsclient.IngressAPI
	rules []types.Rule
	group *types.TargetGroup
	tags  map[string]map[string]string
}

func (f *executorIngressFake) DescribeListenerRules(context.Context, string) ([]types.Rule, error) {
	return append([]types.Rule(nil), f.rules...), nil
}
func (f *executorIngressFake) DescribeTargetGroup(_ context.Context, name string) (*types.TargetGroup, error) {
	if f.group != nil && aws.ToString(f.group.TargetGroupName) == name {
		copy := *f.group
		return &copy, nil
	}
	return nil, nil
}
func (f *executorIngressFake) DescribeTargetGroupByArn(_ context.Context, arn string) (*types.TargetGroup, error) {
	if f.group != nil && aws.ToString(f.group.TargetGroupArn) == arn {
		copy := *f.group
		return &copy, nil
	}
	return nil, nil
}
func (f *executorIngressFake) DescribeTags(_ context.Context, arns []string) (map[string]map[string]string, error) {
	tags := make(map[string]map[string]string, len(arns))
	for _, arn := range arns {
		tags[arn] = f.tags[arn]
	}
	return tags, nil
}

func executorIngressFixture() (*config.Manifest, *executorIngressFake) {
	m := &config.Manifest{Name: "app", Tags: map[string]string{"Environment": "stage", "Application": "app"}, Ingress: &config.Ingress{
		ListenerArn: "arn:listener", VpcID: "vpc-1", Rules: []config.IngressRule{{Priority: 211, Host: "stage.example.com", Service: &config.IngressServiceBackend{Name: "web", ContainerName: "nginx", ContainerPort: 80}}},
	}}
	arn := "arn:aws:elasticloadbalancing:ap-southeast-2:123:targetgroup/app-r220/id"
	f := &executorIngressFake{
		group: &types.TargetGroup{TargetGroupArn: aws.String(arn), TargetGroupName: aws.String("app-r220"), Port: aws.Int32(80), Protocol: types.ProtocolEnumHttp, TargetType: types.TargetTypeEnumIp, VpcId: aws.String("vpc-1")},
		rules: []types.Rule{{RuleArn: aws.String("arn:rule"), Priority: aws.String("220"), Conditions: []types.RuleCondition{{Field: aws.String("host-header"), HostHeaderConfig: &types.HostHeaderConditionConfig{Values: []string{"stage.example.com"}}}}, Actions: []types.Action{{Type: types.ActionTypeEnumForward, TargetGroupArn: aws.String(arn)}}}},
		tags:  map[string]map[string]string{arn: {"ManagedBy": "ecsmate", "Environment": "stage", "Application": "app"}},
	}
	return m, f
}

func TestExecutorIngressGuardRunsBeforeSSMAndOtherWrites(t *testing.T) {
	for _, mode := range []string{"ownership changed", "rule changed", "missing client"} {
		t.Run(mode, func(t *testing.T) {
			m, f := executorIngressFixture()
			ingress, err := resources.DiscoverIngress(context.Background(), f, m)
			if err != nil {
				t.Fatal(err)
			}
			cfg := ExecutorConfig{
				ELBV2Client: f, Output: io.Discard,
				// If execution reaches SSM, the nil client panics on the first read.
				SSMParamsManager: resources.NewSSMParamsManager(nil, &config.ManagedSecrets{Decrypted: map[string]string{"KEY": "value"}}),
			}
			switch mode {
			case "ownership changed":
				f.tags[aws.ToString(f.group.TargetGroupArn)]["Environment"] = "sandbox"
			case "rule changed":
				f.rules[0].Priority = aws.String("221")
			case "missing client":
				cfg.ELBV2Client = nil
			}
			plan := &ExecutionPlan{Manifest: m, TargetGroups: ingress.TargetGroups, ListenerRules: ingress.ListenerRules}
			err = NewExecutor(cfg).Execute(context.Background(), plan, "cluster")
			var guard *resources.IngressError
			if !errors.As(err, &guard) {
				t.Fatalf("expected pre-write guard failure, got %v", err)
			}
		})
	}
}

func TestExecutorUsesPlannedIngressIdentityForPriorityDrift(t *testing.T) {
	m, f := executorIngressFixture()
	ingress, err := resources.DiscoverIngress(context.Background(), f, m)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildExecutionPlan(&resources.DesiredState{Manifest: m, TargetGroups: ingress.TargetGroups, ListenerRules: ingress.ListenerRules})
	if err != nil {
		t.Fatal(err)
	}
	// Any create/modify/delete, including a lookup-driven replacement TG, panics.
	err = NewExecutor(ExecutorConfig{ELBV2Client: f, Output: io.Discard, NoWait: true}).Execute(context.Background(), plan, "cluster")
	if err != nil {
		t.Fatal(err)
	}
	if ingress.TargetGroups["rule-0"].Arn != aws.ToString(f.group.TargetGroupArn) || ingress.ListenerRules[0].Priority != 220 {
		t.Fatal("executor changed planned identities")
	}
}

func TestPlannerPreservesTargetGroupARNInDiffForPriorityDrift(t *testing.T) {
	m, f := executorIngressFixture() // Requested priority 211, live rule and group at 220.
	ingress, err := resources.DiscoverIngress(context.Background(), f, m)
	if err != nil {
		t.Fatal(err)
	}
	liveARN := aws.ToString(f.group.TargetGroupArn)
	svc := &resources.ServiceResource{
		Name: "web", Desired: &config.Service{Cluster: "cluster", TaskDefinition: "web"},
		Current: &ecstypes.Service{LoadBalancers: []ecstypes.LoadBalancer{{
			TargetGroupArn: aws.String(liveARN), ContainerName: aws.String("nginx"), ContainerPort: aws.Int32(80),
		}}},
		Action: resources.ServiceActionUpdate,
	}
	state := &resources.DesiredState{Manifest: m, Services: map[string]*resources.ServiceResource{"web": svc},
		TargetGroups: ingress.TargetGroups, ListenerRules: ingress.ListenerRules}
	for _, entry := range NewPlanner().GeneratePlan(state).Entries {
		if entry.Resource != "Service" {
			continue
		}
		view := entry.Desired.(ServiceView)
		if len(view.LoadBalancers) != 1 || view.LoadBalancers[0].TargetGroupArn != liveARN {
			t.Fatalf("diff must preserve live target group %s, got %+v", liveARN, view.LoadBalancers)
		}
		return
	}
	t.Fatal("missing service diff entry")
}
