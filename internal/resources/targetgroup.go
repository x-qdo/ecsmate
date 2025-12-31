package resources

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
)

type TargetGroupAction string

const (
	TargetGroupActionCreate TargetGroupAction = "CREATE"
	TargetGroupActionUpdate TargetGroupAction = "UPDATE"
	TargetGroupActionDelete TargetGroupAction = "DELETE"
	TargetGroupActionNoop   TargetGroupAction = "NOOP"
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
	Name         string
	ManifestName string // Original rule identifier
	Desired      *TargetGroupSpec
	Current      *types.TargetGroup
	Action       TargetGroupAction
	VpcID        string
	Arn          string
}

type TargetGroupManager struct {
	client *awsclient.ELBV2Client
}

func NewTargetGroupManager(client *awsclient.ELBV2Client) *TargetGroupManager {
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
		tgName := fmt.Sprintf("%s-r%d", manifestName, rule.Priority)
		if len(tgName) > 32 {
			tgName = tgName[:32]
		}

		spec := &TargetGroupSpec{
			Name:                tgName,
			RuleIndex:           i,
			Port:                rule.Service.ContainerPort,
			Protocol:            "HTTP", // Default
			TargetType:          "ip",   // Default for awsvpc network mode
			HealthCheck:         rule.HealthCheck,
			DeregistrationDelay: rule.DeregistrationDelay,
			Tags:                rule.Tags,
		}

		// Store with index as key for later reference
		targetGroups[fmt.Sprintf("rule-%d", i)] = spec
	}

	return targetGroups
}

func (m *TargetGroupManager) BuildResource(ctx context.Context, ruleKey string, spec *TargetGroupSpec, vpcID string) (*TargetGroupResource, error) {
	resource := &TargetGroupResource{
		Name:         spec.Name,
		ManifestName: ruleKey,
		Desired:      spec,
		VpcID:        vpcID,
	}

	if err := m.discoverTargetGroup(ctx, resource); err != nil {
		log.Debug("failed to discover target group", "name", spec.Name, "error", err)
	}

	resource.determineAction()

	return resource, nil
}

func (m *TargetGroupManager) BuildResourceWithExisting(ctx context.Context, ruleKey string, spec *TargetGroupSpec, vpcID, existingArn string) (*TargetGroupResource, error) {
	resource := &TargetGroupResource{
		Name:         spec.Name,
		ManifestName: ruleKey,
		Desired:      spec,
		VpcID:        vpcID,
		Arn:          existingArn,
	}

	if err := m.discoverTargetGroup(ctx, resource); err != nil {
		log.Debug("failed to discover target group", "name", spec.Name, "error", err)
	}

	resource.determineAction()

	return resource, nil
}

func (m *TargetGroupManager) discoverTargetGroup(ctx context.Context, resource *TargetGroupResource) error {
	log.Debug("discovering target group", "name", resource.Name)

	if resource.Arn != "" {
		tg, err := m.client.DescribeTargetGroupByArn(ctx, resource.Arn)
		if err != nil {
			return err
		}
		if tg != nil {
			resource.Current = tg
			return nil
		}
	}

	tg, err := m.client.DescribeTargetGroup(ctx, resource.Name)
	if err != nil {
		return err
	}

	resource.Current = tg
	if tg != nil {
		resource.Arn = aws.ToString(tg.TargetGroupArn)
	}
	return nil
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

	// Check if configuration needs update
	if resource.configChanged() {
		resource.Action = TargetGroupActionUpdate
		return
	}

	resource.Action = TargetGroupActionNoop
}

func (resource *TargetGroupResource) configChanged() bool {
	if resource.Current == nil || resource.Desired == nil {
		return false
	}

	// Port changed
	if aws.ToInt32(resource.Current.Port) != int32(resource.Desired.Port) {
		return true
	}

	// Protocol changed
	if string(resource.Current.Protocol) != resource.Desired.Protocol {
		return true
	}

	// Health check changes require recreation (most settings)
	if resource.Desired.HealthCheck != nil {
		if resource.Current.HealthCheckPath != nil && aws.ToString(resource.Current.HealthCheckPath) != resource.Desired.HealthCheck.Path {
			return true
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

	if resource.Desired.DeregistrationDelay > 0 {
		input.DeregistrationDelay = resource.Desired.DeregistrationDelay
	}

	tg, err := m.client.CreateTargetGroup(ctx, input)
	if err != nil {
		return err
	}

	resource.Arn = aws.ToString(tg.TargetGroupArn)
	return nil
}

func (m *TargetGroupManager) Update(ctx context.Context, resource *TargetGroupResource) error {
	// Target groups have limited update capability
	// For significant changes, we need to recreate
	log.Info("target group update detected - recreating", "name", resource.Name)

	// Delete the old one
	if err := m.client.DeleteTargetGroup(ctx, resource.Arn); err != nil {
		return fmt.Errorf("failed to delete old target group: %w", err)
	}

	// Create a new one
	return m.Create(ctx, resource)
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
	case TargetGroupActionDelete:
		return m.Delete(ctx, resource)
	case TargetGroupActionNoop:
		log.Debug("no changes detected, skipping target group", "name", resource.Name)
		return nil
	default:
		return fmt.Errorf("unknown action: %s", resource.Action)
	}
}
