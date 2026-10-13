package resources

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	awsclient "github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/config"
	"github.com/x-qdo/ecsmate/internal/log"
)

type TargetGroupAction string

const (
	TargetGroupActionCreate   TargetGroupAction = "CREATE"
	TargetGroupActionUpdate   TargetGroupAction = "UPDATE"
	TargetGroupActionRecreate TargetGroupAction = "RECREATE"
	TargetGroupActionDelete   TargetGroupAction = "DELETE"
	TargetGroupActionNoop     TargetGroupAction = "NOOP"
)

// TargetGroupSpec represents the desired state for a target group (derived from IngressRule)
type TargetGroupSpec struct {
	Name                string
	RuleIndex           int
	Port                int
	Protocol            string
	TargetType          string
	HealthCheck         *config.TargetGroupHealthCheck
	DeregistrationDelay int
	Tags                map[string]string
}

type TargetGroupResource struct {
	Name            string
	ManifestName    string // Original rule identifier
	Desired         *TargetGroupSpec
	Current         *types.TargetGroup
	Action          TargetGroupAction
	VpcID           string
	Arn             string
	RecreateReasons []string
}

type TargetGroupManager struct {
	client awsclient.IngressAPI
}

func NewTargetGroupManager(client awsclient.IngressAPI) *TargetGroupManager {
	return &TargetGroupManager{
		client: client,
	}
}

// ExtractTargetGroups extracts target group specs from ingress rules with service backends
func ExtractTargetGroups(manifest *config.Manifest, manifestName string) map[string]*TargetGroupSpec {
	targetGroups := make(map[string]*TargetGroupSpec)

	if manifest.Ingress == nil {
		return targetGroups
	}

	for i, rule := range manifest.Ingress.Rules {
		if rule.Service == nil {
			continue
		}

		// Generate unique target group name from rule priority
		tgName := targetGroupName(manifestName, rule.Priority)

		spec := &TargetGroupSpec{
			Name:                tgName,
			RuleIndex:           i,
			Port:                rule.Service.ContainerPort,
			Protocol:            "HTTP", // Default
			TargetType:          "ip",   // Default for awsvpc network mode
			HealthCheck:         rule.HealthCheck,
			DeregistrationDelay: rule.DeregistrationDelay,
			Tags:                ingressTags(manifest.Tags, rule.Tags),
		}

		// Store with index as key for later reference
		targetGroups[fmt.Sprintf("rule-%d", i)] = spec
	}

	return targetGroups
}

func (resource *TargetGroupResource) determineAction() {
	if resource.Desired == nil {
		if resource.Current != nil {
			resource.Action = TargetGroupActionDelete
		} else {
			resource.Action = TargetGroupActionNoop
		}
		return
	}

	if resource.Current == nil {
		resource.Action = TargetGroupActionCreate
		return
	}

	if reasons := resource.immutableChangeReasons(); len(reasons) > 0 {
		resource.Action = TargetGroupActionRecreate
		resource.RecreateReasons = reasons
		return
	}

	// Check if mutable configuration needs update
	if resource.mutableConfigChanged() {
		resource.Action = TargetGroupActionUpdate
		return
	}

	resource.Action = TargetGroupActionNoop
}

func (resource *TargetGroupResource) immutableChangeReasons() []string {
	if resource.Current == nil || resource.Desired == nil {
		return nil
	}

	var reasons []string

	// Port changed
	if aws.ToInt32(resource.Current.Port) != int32(resource.Desired.Port) {
		reasons = append(reasons, "port changed (requires recreation)")
	}

	// Protocol changed
	if string(resource.Current.Protocol) != resource.Desired.Protocol {
		reasons = append(reasons, "protocol changed (requires recreation)")
	}

	// Target type changed
	if resource.Desired.TargetType != "" && string(resource.Current.TargetType) != resource.Desired.TargetType {
		reasons = append(reasons, "targetType changed (requires recreation)")
	}

	// VPC changed
	if resource.VpcID != "" && aws.ToString(resource.Current.VpcId) != resource.VpcID {
		reasons = append(reasons, "vpcId changed (requires recreation)")
	}

	return reasons
}

func (resource *TargetGroupResource) mutableConfigChanged() bool {
	if resource.Current == nil || resource.Desired == nil {
		return false
	}

	// Health check changes require recreation (most settings)
	if resource.Desired.HealthCheck != nil {
		hc := resource.Desired.HealthCheck
		if hc.Path != "" && aws.ToString(resource.Current.HealthCheckPath) != hc.Path {
			return true
		}
		if hc.Protocol != "" && string(resource.Current.HealthCheckProtocol) != hc.Protocol {
			return true
		}
		if hc.Port != "" && aws.ToString(resource.Current.HealthCheckPort) != hc.Port {
			return true
		}
		if hc.Interval != 0 && aws.ToInt32(resource.Current.HealthCheckIntervalSeconds) != int32(hc.Interval) {
			return true
		}
		if hc.Timeout != 0 && aws.ToInt32(resource.Current.HealthCheckTimeoutSeconds) != int32(hc.Timeout) {
			return true
		}
		if hc.HealthyThreshold != 0 && aws.ToInt32(resource.Current.HealthyThresholdCount) != int32(hc.HealthyThreshold) {
			return true
		}
		if hc.UnhealthyThreshold != 0 && aws.ToInt32(resource.Current.UnhealthyThresholdCount) != int32(hc.UnhealthyThreshold) {
			return true
		}
		if hc.Matcher != "" {
			if resource.Current.Matcher == nil || resource.Current.Matcher.HttpCode == nil {
				return true
			}
			if aws.ToString(resource.Current.Matcher.HttpCode) != hc.Matcher {
				return true
			}
		}
	}

	return false
}

func (m *TargetGroupManager) Create(ctx context.Context, resource *TargetGroupResource) error {
	log.Info("creating target group", "name", resource.Name)

	input := &awsclient.CreateTargetGroupInput{
		Name:       resource.Name,
		Protocol:   resource.Desired.Protocol,
		Port:       resource.Desired.Port,
		VpcID:      resource.VpcID,
		TargetType: resource.Desired.TargetType,
		Tags:       resource.Desired.Tags,
	}

	if input.Protocol == "" {
		input.Protocol = "HTTP"
	}
	if input.TargetType == "" {
		input.TargetType = "ip"
	}

	if resource.Desired.HealthCheck != nil {
		hc := resource.Desired.HealthCheck
		input.HealthCheckPath = hc.Path
		input.HealthCheckProtocol = hc.Protocol
		input.HealthCheckPort = hc.Port
		input.HealthyThreshold = hc.HealthyThreshold
		input.UnhealthyThreshold = hc.UnhealthyThreshold
		input.Timeout = hc.Timeout
		input.Interval = hc.Interval
		input.Matcher = hc.Matcher
	}

	tg, err := m.client.CreateTargetGroup(ctx, input)
	if err != nil {
		return err
	}

	if tg == nil || aws.ToString(tg.TargetGroupArn) == "" {
		return &IngressError{fmt.Errorf("create returned no target group for %s", resource.Name)}
	}
	arn := aws.ToString(tg.TargetGroupArn)
	tags, err := m.client.DescribeTags(ctx, []string{arn})
	if err != nil {
		return &IngressError{fmt.Errorf("cannot verify created target group %s: %w", arn, err)}
	}
	if aws.ToString(tg.TargetGroupName) != resource.Name || !matchesManifestTags(tags[arn], resource.Desired.Tags) {
		return &IngressError{fmt.Errorf("create returned target group %s with unexpected ownership tags %v; expected %v", arn, tags[arn], resource.Desired.Tags)}
	}
	resource.Arn = arn
	if resource.Desired.DeregistrationDelay > 0 {
		return m.client.ModifyTargetGroup(ctx, arn, &awsclient.ModifyTargetGroupInput{DeregistrationDelay: resource.Desired.DeregistrationDelay})
	}
	return nil
}

func (m *TargetGroupManager) Update(ctx context.Context, resource *TargetGroupResource) error {
	log.Info("updating target group", "name", resource.Name)

	input := &awsclient.ModifyTargetGroupInput{}
	if resource.Desired != nil {
		hc := resource.Desired.HealthCheck
		if hc != nil {
			input.HealthCheckPath = hc.Path
			input.HealthCheckProtocol = hc.Protocol
			input.HealthCheckPort = hc.Port
			input.HealthyThreshold = hc.HealthyThreshold
			input.UnhealthyThreshold = hc.UnhealthyThreshold
			input.Timeout = hc.Timeout
			input.Interval = hc.Interval
			input.Matcher = hc.Matcher
		}
		input.DeregistrationDelay = resource.Desired.DeregistrationDelay
	}

	return m.client.ModifyTargetGroup(ctx, resource.Arn, input)
}

func (m *TargetGroupManager) Delete(ctx context.Context, resource *TargetGroupResource) error {
	log.Info("deleting target group", "name", resource.Name)
	return m.client.DeleteTargetGroup(ctx, resource.Arn)
}

func (m *TargetGroupManager) Apply(ctx context.Context, resource *TargetGroupResource) error {
	switch resource.Action {
	case TargetGroupActionCreate:
		return m.Create(ctx, resource)
	case TargetGroupActionUpdate:
		return m.Update(ctx, resource)
	case TargetGroupActionRecreate:
		return m.recreate(ctx, resource)
	case TargetGroupActionDelete:
		return m.Delete(ctx, resource)
	case TargetGroupActionNoop:
		log.Debug("no changes detected, skipping target group", "name", resource.Name)
		return nil
	default:
		return fmt.Errorf("unknown action: %s", resource.Action)
	}
}

func (m *TargetGroupManager) recreate(ctx context.Context, resource *TargetGroupResource) error {
	// For immutable changes, we have to delete and recreate the target group.
	log.Info("target group recreation required", "name", resource.Name)

	if err := m.client.DeleteTargetGroup(ctx, resource.Arn); err != nil {
		return fmt.Errorf("failed to delete old target group: %w", err)
	}

	return m.Create(ctx, resource)
}
