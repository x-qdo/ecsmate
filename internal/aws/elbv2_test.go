package aws

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
)

type elbv2HTTPClient func(*http.Request) (*http.Response, error)

func (f elbv2HTTPClient) Do(r *http.Request) (*http.Response, error) { return f(r) }

func TestELBV2CreatesWithTags(t *testing.T) {
	for _, operation := range []string{"CreateTargetGroup", "CreateRule"} {
		for _, denied := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/denied=%v", operation, denied), func(t *testing.T) {
				calls := 0
				transport := elbv2HTTPClient(func(r *http.Request) (*http.Response, error) {
					calls++
					body, err := io.ReadAll(r.Body)
					if err != nil {
						t.Fatal(err)
					}
					form, err := url.ParseQuery(string(body))
					if err != nil {
						t.Fatal(err)
					}
					if form.Get("Action") != operation {
						t.Fatalf("unexpected request %s", form.Get("Action"))
					}
					got := make(map[string]string)
					for i := 1; i <= 2; i++ {
						got[form.Get(fmt.Sprintf("Tags.member.%d.Key", i))] = form.Get(fmt.Sprintf("Tags.member.%d.Value", i))
					}
					if got["ManagedBy"] != "ecsmate" || got["Environment"] != "stage" {
						t.Fatalf("creation missing tags: %v", form)
					}
					response := "<CreateTargetGroupResponse><CreateTargetGroupResult><TargetGroups><member><TargetGroupArn>arn:tg/app-r211/id</TargetGroupArn><TargetGroupName>app-r211</TargetGroupName></member></TargetGroups></CreateTargetGroupResult></CreateTargetGroupResponse>"
					if operation == "CreateRule" {
						response = "<CreateRuleResponse><CreateRuleResult><Rules><member><RuleArn>arn:rule</RuleArn></member></Rules></CreateRuleResult></CreateRuleResponse>"
					}
					status := http.StatusOK
					if denied {
						status = http.StatusForbidden
						response = "<ErrorResponse><Error><Code>AccessDenied</Code><Message>tagging denied</Message></Error></ErrorResponse>"
					}
					return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/xml"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
				})
				client := &ELBV2Client{client: elasticloadbalancingv2.NewFromConfig(aws.Config{
					Region: "ap-southeast-2", HTTPClient: transport,
					Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
						return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
					}),
				})}
				tags := map[string]string{"ManagedBy": "ecsmate", "Environment": "stage"}
				var err error
				if operation == "CreateRule" {
					_, err = client.CreateListenerRule(context.Background(), &CreateListenerRuleInput{ListenerArn: "arn:listener", Priority: 211, Tags: tags})
				} else {
					_, err = client.CreateTargetGroup(context.Background(), &CreateTargetGroupInput{Name: "app-r211", Protocol: "HTTP", Port: 80, VpcID: "vpc-1", TargetType: "ip", Tags: tags})
				}
				if (err != nil) != denied {
					t.Fatalf("denied=%v, err=%v", denied, err)
				}
				if calls != 1 {
					t.Fatalf("expected only creation request, got %d calls", calls)
				}
			})
		}
	}
}

func TestModifyTargetGroupSkipsEmptyHealthCheckUpdate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		input ModifyTargetGroupInput
		want  []string
	}{
		{"empty", ModifyTargetGroupInput{}, nil},
		{"delay only", ModifyTargetGroupInput{DeregistrationDelay: 30}, []string{"ModifyTargetGroupAttributes"}},
		{"health check only", ModifyTargetGroupInput{HealthCheckPath: "/health"}, []string{"ModifyTargetGroup"}},
		{"both", ModifyTargetGroupInput{HealthCheckPath: "/health", DeregistrationDelay: 30}, []string{"ModifyTargetGroup", "ModifyTargetGroupAttributes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			transport := elbv2HTTPClient(func(r *http.Request) (*http.Response, error) {
				if err := r.ParseForm(); err != nil {
					t.Fatal(err)
				}
				action := r.Form.Get("Action")
				calls = append(calls, action)
				if r.Form.Get("TargetGroupArn") != "arn:tg" {
					t.Fatalf("unexpected target group: %v", r.Form)
				}
				switch action {
				case "ModifyTargetGroup":
					if r.Form.Get("HealthCheckPath") != "/health" {
						t.Fatalf("unexpected health-check update: %v", r.Form)
					}
				case "ModifyTargetGroupAttributes":
					if r.Form.Get("Attributes.member.1.Key") != "deregistration_delay.timeout_seconds" || r.Form.Get("Attributes.member.1.Value") != "30" {
						t.Fatalf("unexpected attributes: %v", r.Form)
					}
				default:
					t.Fatalf("unexpected request %s", action)
				}
				response := fmt.Sprintf("<%sResponse><%sResult/></%sResponse>", action, action, action)
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/xml"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
			})
			client := &ELBV2Client{client: elasticloadbalancingv2.NewFromConfig(aws.Config{
				Region: "ap-southeast-2", HTTPClient: transport,
				Credentials: aws.CredentialsProviderFunc(func(context.Context) (aws.Credentials, error) {
					return aws.Credentials{AccessKeyID: "test", SecretAccessKey: "test"}, nil
				}),
			})}
			if err := client.ModifyTargetGroup(context.Background(), "arn:tg", &tc.input); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, tc.want) {
				t.Fatalf("expected requests %v, got %v", tc.want, calls)
			}
		})
	}
}
