package resources

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	ecstypes "github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	awsclient "github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/config"
)

// Unimplemented methods panic, so discovery tests also enforce zero writes.
type ingressFake struct {
	awsclient.IngressAPI
	rules            []types.Rule
	groups           map[string]*types.TargetGroup
	tags             map[string]map[string]string
	fail             string
	created          *types.TargetGroup
	createdInput     *awsclient.CreateTargetGroupInput
	createdRuleInput *awsclient.CreateListenerRuleInput
	modified         bool
	nameReads        int
	arnReads         int
}

func (f *ingressFake) DescribeListenerRules(context.Context, string) ([]types.Rule, error) {
	if f.fail == "rules" {
		return nil, errors.New("rules read denied")
	}
	return append([]types.Rule(nil), f.rules...), nil
}
func (f *ingressFake) DescribeTargetGroup(_ context.Context, name string) (*types.TargetGroup, error) {
	f.nameReads++
	if f.fail == "name" {
		return nil, errors.New("name read denied")
	}
	for _, tg := range f.groups {
		if aws.ToString(tg.TargetGroupName) == name {
			copy := *tg
			return &copy, nil
		}
	}
	return nil, nil
}
func (f *ingressFake) DescribeTargetGroupByArn(_ context.Context, arn string) (*types.TargetGroup, error) {
	f.arnReads++
	if f.fail == "arn" {
		return nil, errors.New("arn read denied")
	}
	if tg := f.groups[arn]; tg != nil {
		copy := *tg
		return &copy, nil
	}
	return nil, nil
}
func (f *ingressFake) DescribeTags(_ context.Context, arns []string) (map[string]map[string]string, error) {
	if f.fail == "tags" {
		return nil, errors.New("tags read denied")
	}
	tags := make(map[string]map[string]string, len(arns))
	for _, arn := range arns {
		tags[arn] = f.tags[arn]
	}
	return tags, nil
}
func (f *ingressFake) CreateTargetGroup(_ context.Context, input *awsclient.CreateTargetGroupInput) (*types.TargetGroup, error) {
	f.createdInput = input
	return f.created, nil
}
func (f *ingressFake) ModifyTargetGroup(context.Context, string, *awsclient.ModifyTargetGroupInput) error {
	f.modified = true
	return nil
}
func (f *ingressFake) CreateListenerRule(_ context.Context, input *awsclient.CreateListenerRuleInput) (*types.Rule, error) {
	f.createdRuleInput = input
	return &types.Rule{RuleArn: aws.String("arn:created-rule")}, nil
}

func ingressFixture() (*config.Manifest, *ingressFake) {
	m := &config.Manifest{
		Name: "enrolment-nzpa",
		Tags: map[string]string{"Environment": "stage", "Tenant": "enrolment-nzpa"},
		Ingress: &config.Ingress{ListenerArn: "arn:listener", VpcID: "vpc-1", Rules: []config.IngressRule{
			testIngressRule(211, "enrolment-nzpoliceassn.stage.cloudinsurance.io"),
		}},
	}
	return m, &ingressFake{groups: make(map[string]*types.TargetGroup), tags: make(map[string]map[string]string)}
}

func (f *ingressFake) addGroup(name string, tags map[string]string) string {
	arn := "arn:aws:elasticloadbalancing:ap-southeast-2:123:targetgroup/" + name + "/id"
	f.groups[arn] = &types.TargetGroup{
		TargetGroupArn: aws.String(arn), TargetGroupName: aws.String(name),
		Port: aws.Int32(80), Protocol: types.ProtocolEnumHttp, TargetType: types.TargetTypeEnumIp, VpcId: aws.String("vpc-1"),
	}
	f.tags[arn] = tags
	return arn
}

func requireIngressError(t *testing.T, err error, contains string) {
	t.Helper()
	var ingressError *IngressError
	if !errors.As(err, &ingressError) || !strings.Contains(err.Error(), contains) {
		t.Fatalf("expected ingress error containing %q, got %v", contains, err)
	}
}

func TestDiscoverIngress_RejectsClashes(t *testing.T) {
	cases := []struct {
		name   string
		change func(*config.Manifest, *ingressFake)
		want   string
	}{
		{"original sandbox stage incident", func(m *config.Manifest, f *ingressFake) {
			arn := f.addGroup("enrolment-nzpa-r211", map[string]string{"ManagedBy": "ecsmate", "Environment": "sandbox", "Tenant": "enrolment-nzpa"})
			f.rules = []types.Rule{testForwardRule("arn:sandbox-rule", 211, "enrolment-nzpoliceassn.sandbox.cloudinsurance.io", arn)}
		}, `tag "Environment" is "sandbox" (expected "stage")`},
		{"priority belongs to other tenant", func(m *config.Manifest, f *ingressFake) {
			arn := f.addGroup("other-r211", map[string]string{"ManagedBy": "ecsmate", "Environment": "stage", "Tenant": "other"})
			f.rules = []types.Rule{testForwardRule("arn:other-rule", 211, "other.example.com", arn)}
		}, "priority 211"},
		{"unowned group without listener", func(m *config.Manifest, f *ingressFake) { f.addGroup("enrolment-nzpa-r211", nil) }, "missing tag"},
		{"same host different priority", func(m *config.Manifest, f *ingressFake) {
			arn := f.addGroup("other-r220", map[string]string{"ManagedBy": "ecsmate", "Tenant": "other"})
			f.rules = []types.Rule{testForwardRule("arn:other-rule", 220, m.Ingress.Rules[0].Host, arn)}
		}, "overlap forwarding rule"},
		{"alias case insensitive", func(m *config.Manifest, f *ingressFake) {
			m.Ingress.Rules[0].Hosts = []string{"Alias.Example.Com"}
			f.rules = []types.Rule{testForwardRule("arn:unknown-rule", 220, "alias.example.com", "arn:tg/unknown/id")}
		}, "missing tag"},
		{"unowned requested priority", func(m *config.Manifest, f *ingressFake) {
			f.rules = []types.Rule{{RuleArn: aws.String("arn:deny"), Priority: aws.String("211"), Actions: []types.Action{{Type: types.ActionTypeEnumFixedResponse}}}}
		}, "priority 211"},
		{"contradictory rule ownership", func(m *config.Manifest, f *ingressFake) {
			arn := f.addGroup("enrolment-nzpa-r211", ingressTags(m.Tags, nil))
			f.rules = []types.Rule{testForwardRule("arn:rule", 211, m.Ingress.Rules[0].Host, arn)}
			f.tags["arn:rule"] = map[string]string{"ManagedBy": "ecsmate", "Environment": "sandbox", "Tenant": "enrolment-nzpa"}
		}, `tag "Environment" is "sandbox" (expected "stage")`},
		{"ambiguous owned matches", func(m *config.Manifest, f *ingressFake) {
			for _, priority := range []int{220, 221} {
				arn := f.addGroup(fmt.Sprintf("enrolment-nzpa-r%d", priority), ingressTags(m.Tags, nil))
				f.rules = append(f.rules, testForwardRule(fmt.Sprintf("arn:rule-%d", priority), priority, m.Ingress.Rules[0].Host, arn))
			}
		}, "ambiguously matches"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, f := ingressFixture()
			tc.change(m, f)
			_, err := DiscoverIngress(context.Background(), f, m)
			requireIngressError(t, err, tc.want)
		})
	}
}

func TestDiscoverIngress_PreservesOwnedPriorityAndARN(t *testing.T) {
	for _, occupied := range []bool{false, true} {
		t.Run(fmt.Sprintf("requested occupied=%v", occupied), func(t *testing.T) {
			m, f := ingressFixture()
			arn := f.addGroup("enrolment-nzpa-r220", ingressTags(m.Tags, nil))
			f.rules = []types.Rule{testForwardRule("arn:owned", 220, m.Ingress.Rules[0].Host, arn)}
			// AWS returns a one-target ForwardConfig, including disabled stickiness.
			f.rules[0].Actions[0].ForwardConfig = &types.ForwardActionConfig{
				TargetGroups:                []types.TargetGroupTuple{{TargetGroupArn: aws.String(arn), Weight: aws.Int32(1)}},
				TargetGroupStickinessConfig: &types.TargetGroupStickinessConfig{Enabled: aws.Bool(false)},
			}
			if occupied {
				f.rules = append(f.rules, testForwardRule("arn:foreign", 211, "other.example.com", "arn:tg/other-r211/id"))
			}
			plan, err := DiscoverIngress(context.Background(), f, m)
			if occupied {
				requireIngressError(t, err, "priority 211")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			rule, tg := plan.ListenerRules[0], plan.TargetGroups["rule-0"]
			if rule.Priority != 220 || rule.Arn != "arn:owned" || rule.Action != ListenerRuleActionNoop || tg.Arn != arn || tg.Name != "enrolment-nzpa-r220" || tg.Action != TargetGroupActionNoop {
				t.Fatalf("did not preserve checked resources: rule=%+v tg=%+v", rule, tg)
			}
		})
	}
}

func TestDiscoverIngress_ProtectedRules(t *testing.T) {
	mutations := map[string]func(*types.Rule){
		"source ip": func(r *types.Rule) {
			r.Conditions = append(r.Conditions, types.RuleCondition{Field: aws.String("source-ip"), SourceIpConfig: &types.SourceIpConditionConfig{Values: []string{"203.0.113.0/24"}}})
		},
		"header": func(r *types.Rule) {
			r.Conditions = append(r.Conditions, types.RuleCondition{Field: aws.String("http-header")})
		},
		"query": func(r *types.Rule) {
			r.Conditions = append(r.Conditions, types.RuleCondition{Field: aws.String("query-string")})
		},
		"method": func(r *types.Rule) {
			r.Conditions = append(r.Conditions, types.RuleCondition{Field: aws.String("http-request-method")})
		},
		"auth": func(r *types.Rule) {
			r.Actions = append(r.Actions, types.Action{Type: types.ActionTypeEnumAuthenticateOidc})
		},
		"weighted": func(r *types.Rule) {
			r.Actions[0].ForwardConfig = &types.ForwardActionConfig{TargetGroups: []types.TargetGroupTuple{{TargetGroupArn: r.Actions[0].TargetGroupArn}, {TargetGroupArn: aws.String("arn:tg/other/id")}}}
		},
		"regex":     func(r *types.Rule) { r.Conditions[0].HostHeaderConfig.RegexValues = []string{"example.*"} },
		"transform": func(r *types.Rule) { r.Transforms = []types.RuleTransform{{}} },
		"stickiness": func(r *types.Rule) {
			r.Actions[0].ForwardConfig = &types.ForwardActionConfig{TargetGroups: []types.TargetGroupTuple{{TargetGroupArn: r.Actions[0].TargetGroupArn}}, TargetGroupStickinessConfig: &types.TargetGroupStickinessConfig{Enabled: aws.Bool(true)}}
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			m, f := ingressFixture()
			arn := f.addGroup("enrolment-nzpa-r220", ingressTags(m.Tags, nil))
			rule := testForwardRule("arn:protected", 220, m.Ingress.Rules[0].Host, arn)
			mutate(&rule)
			f.rules = []types.Rule{rule}
			_, err := DiscoverIngress(context.Background(), f, m)
			requireIngressError(t, err, "overlap forwarding rule")
			m.Ingress.Rules = nil
			plan, err := DiscoverIngress(context.Background(), f, m)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.ListenerRules) != 0 || len(plan.TargetGroups) != 0 {
				t.Fatal("protected resources must not be deleted")
			}
		})
	}
}

func TestDiscoverIngress_CleanupAndInfrastructureRules(t *testing.T) {
	m, f := ingressFixture()
	desiredTG := f.addGroup("enrolment-nzpa-r211", ingressTags(m.Tags, nil))
	orphanTG := f.addGroup("enrolment-nzpa-r220", ingressTags(m.Tags, nil))
	f.rules = []types.Rule{
		testForwardRule("arn:desired", 211, m.Ingress.Rules[0].Host, desiredTG),
		testForwardRule("arn:orphan", 220, "old.example.com", orphanTG),
		testForwardRule("arn:unknown", 230, "unrelated.example.com", "arn:tg/other/id"),
		{RuleArn: aws.String("arn:default"), Priority: aws.String("default"), IsDefault: aws.Bool(true)},
	}
	// A NZPA-style infrastructure access-control deny is never adopted/deleted.
	deny := testForwardRule("arn:deny", 10, m.Ingress.Rules[0].Host, "")
	deny.Actions = []types.Action{{Type: types.ActionTypeEnumFixedResponse, FixedResponseConfig: &types.FixedResponseActionConfig{StatusCode: aws.String("403")}}}
	f.rules = append(f.rules, deny)
	plan, err := DiscoverIngress(context.Background(), f, m)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.ListenerRules) != 2 || plan.ListenerRules[1].Arn != "arn:orphan" || plan.ListenerRules[1].Action != ListenerRuleActionDelete || plan.TargetGroups["orphan-enrolment-nzpa-r220"].Action != TargetGroupActionDelete {
		t.Fatalf("unexpected orphan plan: %+v", plan)
	}
	// A protected rule referencing the orphan TG prevents its deletion.
	protected := testForwardRule("arn:source-ip", 240, "old.example.com", orphanTG)
	protected.Conditions = append(protected.Conditions, types.RuleCondition{Field: aws.String("source-ip")})
	f.rules = append(f.rules, protected)
	plan, err = DiscoverIngress(context.Background(), f, m)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plan.TargetGroups["orphan-enrolment-nzpa-r220"]; ok {
		t.Fatal("deleted TG used by protected rule")
	}
}

func TestDiscoverIngress_OwnHostChangeAndPathSharing(t *testing.T) {
	m, f := ingressFixture()
	arn := f.addGroup("enrolment-nzpa-r211", ingressTags(m.Tags, nil))
	f.rules = []types.Rule{testForwardRule("arn:owned", 211, "old.example.com", arn)}
	m.Ingress.Rules = append(m.Ingress.Rules, testIngressRule(212, m.Ingress.Rules[0].Host))
	m.Ingress.Rules[1].Paths = []string{"/jaeger/*"}
	plan, err := DiscoverIngress(context.Background(), f, m)
	if err != nil {
		t.Fatal(err)
	}
	if plan.ListenerRules[0].Action != ListenerRuleActionUpdate || plan.ListenerRules[0].Arn != "arn:owned" || plan.ListenerRules[1].Action != ListenerRuleActionCreate {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestIngressReadErrors(t *testing.T) {
	for _, operation := range []string{"rules", "name", "tags", "arn"} {
		t.Run(operation, func(t *testing.T) {
			m, f := ingressFixture()
			// A drifted group requires a separate ARN lookup after the name lookup.
			arn := f.addGroup("enrolment-nzpa-r220", ingressTags(m.Tags, nil))
			f.rules = []types.Rule{testForwardRule("arn:owned", 220, m.Ingress.Rules[0].Host, arn)}
			f.fail = operation
			_, err := DiscoverIngress(context.Background(), f, m)
			requireIngressError(t, err, operation+" read denied")
		})
	}
	m, _ := ingressFixture()
	_, err := DiscoverIngress(context.Background(), nil, m)
	requireIngressError(t, err, "client unavailable")
}

func TestDiscoverIngress_ReusesTargetGroupReads(t *testing.T) {
	for _, tc := range []struct {
		name     string
		priority int
		arnReads int
	}{
		{"new", 0, 0},
		{"existing", 211, 0},
		{"priority drift", 220, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, f := ingressFixture()
			var arn string
			if tc.priority != 0 {
				arn = f.addGroup(targetGroupName(m.Name, tc.priority), ingressTags(m.Tags, nil))
				f.rules = []types.Rule{testForwardRule("arn:owned", tc.priority, m.Ingress.Rules[0].Host, arn)}
			}
			plan, err := DiscoverIngress(context.Background(), f, m)
			if err != nil {
				t.Fatal(err)
			}
			if f.nameReads != 1 || f.arnReads != tc.arnReads {
				t.Fatalf("expected 1 name read and %d ARN reads; got %d and %d", tc.arnReads, f.nameReads, f.arnReads)
			}
			if plan.TargetGroups["rule-0"].Arn != arn {
				t.Fatalf("expected target group %q, got %q", arn, plan.TargetGroups["rule-0"].Arn)
			}
		})
	}
}

func TestDiscoverIngress_ExplainsStrictOwnershipFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*config.Manifest, map[string]string)
		want   string
	}{
		{"added metadata tag", func(m *config.Manifest, _ map[string]string) { m.Tags["CostCenter"] = "insurance" }, `missing tag "CostCenter" (expected "insurance")`},
		{"missing environment", func(_ *config.Manifest, tags map[string]string) { delete(tags, "Environment") }, `missing tag "Environment" (expected "stage")`},
		{"mismatched environment", func(_ *config.Manifest, tags map[string]string) { tags["Environment"] = "sandbox" }, `tag "Environment" is "sandbox" (expected "stage")`},
		{"missing manager", func(_ *config.Manifest, tags map[string]string) { delete(tags, "ManagedBy") }, `missing tag "ManagedBy" (expected "ecsmate")`},
		{"mismatched manager", func(_ *config.Manifest, tags map[string]string) { tags["ManagedBy"] = "terraform" }, `tag "ManagedBy" is "terraform" (expected "ecsmate")`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, f := ingressFixture()
			tags := ingressTags(m.Tags, nil)
			arn := f.addGroup(targetGroupName(m.Name, 211), tags)
			f.rules = []types.Rule{testForwardRule("arn:owned", 211, m.Ingress.Rules[0].Host, arn)}
			tc.change(m, tags)
			_, err := DiscoverIngress(context.Background(), f, m)
			requireIngressError(t, err, tc.want)
			if strings.Count(err.Error(), fmt.Sprintf("manifest %q", m.Name)) != 1 {
				t.Fatalf("expected one manifest context: %v", err)
			}
			if strings.Contains(err.Error(), "belongs to another instance") {
				t.Fatalf("ownership failure must not claim another owner: %v", err)
			}
		})
	}
}

func TestDiscoverIngress_RejectsForeignWildcardRegardlessOfPriority(t *testing.T) {
	for _, priority := range []int{1, 50000} {
		t.Run(fmt.Sprint(priority), func(t *testing.T) {
			m, f := ingressFixture()
			arn := f.addGroup("other-r1", map[string]string{"ManagedBy": "ecsmate", "Environment": "stage", "Tenant": "other"})
			f.rules = []types.Rule{testForwardRule("arn:wildcard", priority, "*.stage.cloudinsurance.io", arn)}
			_, err := DiscoverIngress(context.Background(), f, m)
			requireIngressError(t, err, "overlap forwarding rule")
		})
	}
}

func TestRevalidateIngress_RejectsStalePlan(t *testing.T) {
	for _, change := range []string{"priority", "healthcheck", "tags", "new overlapping rule"} {
		t.Run(change, func(t *testing.T) {
			m, f := ingressFixture()
			arn := f.addGroup("enrolment-nzpa-r211", ingressTags(m.Tags, nil))
			f.rules = []types.Rule{testForwardRule("arn:owned", 211, m.Ingress.Rules[0].Host, arn)}
			plan, err := DiscoverIngress(context.Background(), f, m)
			if err != nil {
				t.Fatal(err)
			}
			if err = RevalidateIngress(context.Background(), f, m, plan.TargetGroups, plan.ListenerRules); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "priority":
				f.rules[0].Priority = aws.String("220")
			case "healthcheck":
				f.groups[arn].HealthCheckPath = aws.String("/changed")
			case "tags":
				f.tags[arn]["Environment"] = "sandbox"
			case "new overlapping rule":
				f.rules = append(f.rules, testForwardRule("arn:new", 230, m.Ingress.Rules[0].Host, "arn:tg/other/id"))
			}
			err = RevalidateIngress(context.Background(), f, m, plan.TargetGroups, plan.ListenerRules)
			requireIngressError(t, err, "ingress guard")
		})
	}
}

func TestValidateIngressManifest(t *testing.T) {
	cases := map[string]func(*config.Manifest){
		"no tags":                func(m *config.Manifest) { m.Tags = nil },
		"only ManagedBy":         func(m *config.Manifest) { m.Tags = map[string]string{"ManagedBy": "ecsmate"} },
		"wrong manager":          func(m *config.Manifest) { m.Tags["ManagedBy"] = "terraform" },
		"contradictory rule tag": func(m *config.Manifest) { m.Ingress.Rules[0].Tags = map[string]string{"Environment": "sandbox"} },
		"rule manager":           func(m *config.Manifest) { m.Ingress.Rules[0].Tags = map[string]string{"ManagedBy": "terraform"} },
		"too long":               func(m *config.Manifest) { m.Name = strings.Repeat("a", 30) },
		"leading hyphen":         func(m *config.Manifest) { m.Name = "-app" },
		"invalid name":           func(m *config.Manifest) { m.Name = "app_name" },
		"duplicate priority":     func(m *config.Manifest) { m.Ingress.Rules = append(m.Ingress.Rules, m.Ingress.Rules[0]) },
		"zero priority":          func(m *config.Manifest) { m.Ingress.Rules[0].Priority = 0 },
		"priority too high":      func(m *config.Manifest) { m.Ingress.Rules[0].Priority = 50001 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m, _ := ingressFixture()
			mutate(m)
			if ValidateIngressManifest(m) == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	if err := ValidateIngressManifest(&config.Manifest{}); err != nil {
		t.Fatal(err)
	}
	m, _ := ingressFixture()
	if err := ValidateIngressManifest(m); err != nil {
		t.Fatal(err)
	}
}

func TestHostsOverlap(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"a.example.com", "a.example.com", true}, {"a.example.com", "b.example.com", false},
		{"*.example.com", "a.example.com", true}, {"a.example.com", "?.example.com", true},
		{"*.example.com", "*.other.com", true}, {"*.example.com", "other.com", false},
	} {
		if got := hostsOverlap([]string{tc.a}, []string{tc.b}); got != tc.want {
			t.Errorf("%s vs %s: got %v", tc.a, tc.b, got)
		}
	}
	if !hostsOverlap(nil, []string{"example.com"}) {
		t.Fatal("catch-all must overlap")
	}
}

func TestIngressTagsAtCreationAndReturnedOwnership(t *testing.T) {
	for _, owner := range []string{"ours", "foreign", "missing", "read failure"} {
		t.Run(owner, func(t *testing.T) {
			m, f := ingressFixture()
			m.Ingress.Rules[0].Tags = map[string]string{"Team": "insurance"}
			m.Ingress.Rules[0].DeregistrationDelay = 30
			plan, err := DiscoverIngress(context.Background(), f, m)
			if err != nil {
				t.Fatal(err)
			}
			arn := f.addGroup("enrolment-nzpa-r211", ingressTags(m.Tags, m.Ingress.Rules[0].Tags))
			f.created = f.groups[arn]
			switch owner {
			case "foreign":
				f.tags[arn]["Environment"] = "sandbox"
			case "missing":
				f.tags[arn] = nil
			case "read failure":
				f.fail = "tags"
			}
			err = NewTargetGroupManager(f).Create(context.Background(), plan.TargetGroups["rule-0"])
			if owner != "ours" {
				requireIngressError(t, err, "target group")
				if f.modified {
					t.Fatal("modified group before verifying ownership")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tags := f.createdInput.Tags
			if tags["ManagedBy"] != "ecsmate" || tags["Environment"] != "stage" || tags["Team"] != "insurance" {
				t.Fatalf("missing inherited tags: %v", tags)
			}
			rule := plan.ListenerRules[0]
			rule.TargetGroupArn = arn
			if err := NewListenerRuleManager(f).Create(context.Background(), rule); err != nil {
				t.Fatal(err)
			}
			if f.createdRuleInput.Tags["Environment"] != "stage" || f.createdRuleInput.Tags["ManagedBy"] != "ecsmate" {
				t.Fatalf("missing rule tags: %v", f.createdRuleInput.Tags)
			}
		})
	}
}

func TestValidateIngressServices(t *testing.T) {
	m, f := ingressFixture()
	arn := f.addGroup("enrolment-nzpa-r211", ingressTags(m.Tags, nil))
	services := map[string]*ServiceResource{"web": {Name: "enrolment-nzpa-web", Current: &ecstypes.Service{LoadBalancers: []ecstypes.LoadBalancer{{TargetGroupArn: aws.String(arn), ContainerName: aws.String("nginx"), ContainerPort: aws.Int32(80)}}}}}
	if err := ValidateIngressServices(context.Background(), f, m, services); err != nil {
		t.Fatal(err)
	}
	f.tags[arn]["Environment"] = "sandbox"
	requireIngressError(t, ValidateIngressServices(context.Background(), f, m, services), "unverified target-group binding")
}
