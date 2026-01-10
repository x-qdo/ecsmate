package resources

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
)

type ServiceAction string

const (
	ServiceActionCreate   ServiceAction = "CREATE"
	ServiceActionUpdate   ServiceAction = "UPDATE"
	ServiceActionDelete   ServiceAction = "DELETE"
	ServiceActionRecreate ServiceAction = "RECREATE"
	ServiceActionNoop     ServiceAction = "NOOP"
)

type AutoScalingAction string

const (
	AutoScalingActionCreate AutoScalingAction = "CREATE"
	AutoScalingActionUpdate AutoScalingAction = "UPDATE"
	AutoScalingActionDelete AutoScalingAction = "DELETE"
	AutoScalingActionNoop   AutoScalingAction = "NOOP"
)

type ServiceResource struct {
	Name    string
	Desired *config.Service
	Current *types.Service
	Action  ServiceAction

	TaskDefinitionArn string
	ClusterArn        string

	// Recreate tracking - fields that changed and force recreation
	RecreateReasons []string

	// Propagation reason when action was set due to dependency change
	PropagationReason string

	// Auto-scaling state
	CurrentAutoScaling *CurrentAutoScalingState
	AutoScalingAction  AutoScalingAction
}

type CurrentAutoScalingState struct {
	Target   *awsclient.ScalableTarget
	Policies []awsclient.ScalingPolicyInfo
}

// Validate validates the service resource configuration
func (r *ServiceResource) Validate() error {
	if r.Desired == nil {
		return fmt.Errorf("no desired state for service %s", r.Name)
	}

	svc := r.Desired

	if svc.Name == "" {
		return fmt.Errorf("service name is required")
	}
	if svc.Cluster == "" {
		return fmt.Errorf("service %s: cluster is required", svc.Name)
	}
	if r.TaskDefinitionArn == "" && svc.TaskDefinition == "" {
		return fmt.Errorf("service %s: taskDefinition is required", svc.Name)
	}

	// Validate network configuration
	if svc.NetworkConfiguration != nil {
		if len(svc.NetworkConfiguration.Subnets) == 0 {
			return fmt.Errorf("service %s: networkConfiguration.subnets is required", svc.Name)
		}
	}

	// Validate load balancers
	for i, lb := range svc.LoadBalancers {
		if lb.TargetGroupArn == "" {
			return fmt.Errorf("service %s: loadBalancers[%d].targetGroupArn is required", svc.Name, i)
		}
		if lb.ContainerName == "" {
			return fmt.Errorf("service %s: loadBalancers[%d].containerName is required", svc.Name, i)
		}
		if lb.ContainerPort <= 0 {
			return fmt.Errorf("service %s: loadBalancers[%d].containerPort is required", svc.Name, i)
		}
	}

	// Validate service registries
	for i, reg := range svc.ServiceRegistries {
		if reg.RegistryArn == "" {
			return fmt.Errorf("service %s: serviceRegistries[%d].registryArn is required", svc.Name, i)
		}
	}

	return nil
}

func (r *ServiceResource) ToCreateInput() (*ecs.CreateServiceInput, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}

	svc := r.Desired

	input := &ecs.CreateServiceInput{
		ServiceName:    aws.String(svc.Name),
		Cluster:        aws.String(svc.Cluster),
		TaskDefinition: aws.String(r.TaskDefinitionArn),
		DesiredCount:   aws.Int32(int32(svc.DesiredCount)),
	}

	if len(svc.CapacityProviderStrategy) > 0 {
		for _, cp := range svc.CapacityProviderStrategy {
			item := types.CapacityProviderStrategyItem{
				CapacityProvider: aws.String(cp.CapacityProvider),
				Weight:           int32(cp.Weight),
			}
			if cp.Base > 0 {
				item.Base = int32(cp.Base)
			}
			input.CapacityProviderStrategy = append(input.CapacityProviderStrategy, item)
		}
	} else if svc.LaunchType != "" {
		input.LaunchType = types.LaunchType(svc.LaunchType)
	}

	if svc.PlatformVersion != "" {
		input.PlatformVersion = aws.String(svc.PlatformVersion)
	}

	input.EnableExecuteCommand = svc.EnableExecuteCommand
	if svc.HealthCheckGracePeriodSecondsSet {
		input.HealthCheckGracePeriodSeconds = aws.Int32(int32(svc.HealthCheckGracePeriodSeconds))
	}

	if svc.NetworkConfiguration != nil {
		input.NetworkConfiguration = buildNetworkConfiguration(svc.NetworkConfiguration)
	}

	for _, lb := range svc.LoadBalancers {
		input.LoadBalancers = append(input.LoadBalancers, types.LoadBalancer{
			TargetGroupArn: aws.String(lb.TargetGroupArn),
			ContainerName:  aws.String(lb.ContainerName),
			ContainerPort:  aws.Int32(int32(lb.ContainerPort)),
		})
	}

	for _, reg := range svc.ServiceRegistries {
		sr := types.ServiceRegistry{
			RegistryArn: aws.String(reg.RegistryArn),
		}
		if reg.ContainerName != "" {
			sr.ContainerName = aws.String(reg.ContainerName)
		}
		if reg.ContainerPort > 0 {
			sr.ContainerPort = aws.Int32(int32(reg.ContainerPort))
		}
		if reg.Port > 0 {
			sr.Port = aws.Int32(int32(reg.Port))
		}
		input.ServiceRegistries = append(input.ServiceRegistries, sr)
	}

	deploymentConfig := buildDeploymentConfiguration(svc.Deployment)
	if deploymentConfig != nil {
		input.DeploymentConfiguration = deploymentConfig
	}

	input.DeploymentController = &types.DeploymentController{
		Type: types.DeploymentControllerTypeEcs,
	}

	return input, nil
}

func (r *ServiceResource) ToUpdateInput() (*ecs.UpdateServiceInput, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}

	svc := r.Desired

	input := &ecs.UpdateServiceInput{
		Service:        aws.String(svc.Name),
		Cluster:        aws.String(svc.Cluster),
		TaskDefinition: aws.String(r.TaskDefinitionArn),
		DesiredCount:   aws.Int32(int32(svc.DesiredCount)),
	}

	// Capacity provider strategy can be updated (unlike launchType)
	if len(svc.CapacityProviderStrategy) > 0 {
		for _, cp := range svc.CapacityProviderStrategy {
			item := types.CapacityProviderStrategyItem{
				CapacityProvider: aws.String(cp.CapacityProvider),
				Weight:           int32(cp.Weight),
			}
			if cp.Base > 0 {
				item.Base = int32(cp.Base)
			}
			input.CapacityProviderStrategy = append(input.CapacityProviderStrategy, item)
		}
	}

	if svc.PlatformVersion != "" {
		input.PlatformVersion = aws.String(svc.PlatformVersion)
	}

	input.EnableExecuteCommand = aws.Bool(svc.EnableExecuteCommand)
	if svc.HealthCheckGracePeriodSecondsSet {
		input.HealthCheckGracePeriodSeconds = aws.Int32(int32(svc.HealthCheckGracePeriodSeconds))
	}

	if svc.NetworkConfiguration != nil {
		input.NetworkConfiguration = buildNetworkConfiguration(svc.NetworkConfiguration)
	}

	if len(svc.LoadBalancers) > 0 {
		for _, lb := range svc.LoadBalancers {
			input.LoadBalancers = append(input.LoadBalancers, types.LoadBalancer{
				TargetGroupArn: aws.String(lb.TargetGroupArn),
				ContainerName:  aws.String(lb.ContainerName),
				ContainerPort:  aws.Int32(int32(lb.ContainerPort)),
			})
		}
	}

	deploymentConfig := buildDeploymentConfiguration(svc.Deployment)
	if deploymentConfig != nil {
		input.DeploymentConfiguration = deploymentConfig
	}

	return input, nil
}

func buildNetworkConfiguration(nc *config.NetworkConfiguration) *types.NetworkConfiguration {
	awsVpcConfig := &types.AwsVpcConfiguration{
		Subnets:        nc.Subnets,
		SecurityGroups: nc.SecurityGroups,
	}

	if nc.AssignPublicIp != "" {
		awsVpcConfig.AssignPublicIp = types.AssignPublicIp(nc.AssignPublicIp)
	}

	return &types.NetworkConfiguration{
		AwsvpcConfiguration: awsVpcConfig,
	}
}

func buildDeploymentConfiguration(dc config.DeploymentConfig) *types.DeploymentConfiguration {
	return BuildDeploymentConfiguration(dc)
}

type ServiceManager struct {
	ecsClient         *awsclient.ECSClient
	autoScalingClient *awsclient.AutoScalingClient
}

func NewServiceManager(ecsClient *awsclient.ECSClient) *ServiceManager {
	return &ServiceManager{ecsClient: ecsClient}
}

func NewServiceManagerWithAutoScaling(ecsClient *awsclient.ECSClient, asClient *awsclient.AutoScalingClient) *ServiceManager {
	return &ServiceManager{
		ecsClient:         ecsClient,
		autoScalingClient: asClient,
	}
}

func (m *ServiceManager) BuildResource(ctx context.Context, name string, svc *config.Service, taskDefArn string) (*ServiceResource, error) {
	resource := &ServiceResource{
		Name:              name,
		Desired:           svc,
		TaskDefinitionArn: taskDefArn,
	}

	if err := m.discoverService(ctx, resource); err != nil {
		log.Debug("failed to discover service", "name", name, "error", err)
	}

	if m.autoScalingClient != nil {
		if err := m.discoverAutoScaling(ctx, resource); err != nil {
			log.Debug("failed to discover auto scaling", "name", name, "error", err)
		}
	}

	resource.determineAction()
	resource.determineAutoScalingAction()

	return resource, nil
}

func (m *ServiceManager) discoverService(ctx context.Context, resource *ServiceResource) error {
	serviceName := resource.Desired.Name
	cluster := resource.Desired.Cluster

	log.Debug("discovering service", "name", serviceName, "cluster", cluster)

	services, err := m.ecsClient.DescribeServices(ctx, []string{serviceName})
	if err != nil {
		if strings.Contains(err.Error(), "MISSING") {
			return nil
		}
		return err
	}

	for _, svc := range services {
		if aws.ToString(svc.Status) != "INACTIVE" {
			resource.Current = &svc
			return nil
		}
	}

	return nil
}

func (m *ServiceManager) discoverAutoScaling(ctx context.Context, resource *ServiceResource) error {
	if m.autoScalingClient == nil {
		return nil
	}

	cluster := resource.Desired.Cluster
	serviceName := resource.Desired.Name

	target, err := m.autoScalingClient.DescribeScalableTarget(ctx, cluster, serviceName)
	if err != nil {
		return err
	}

	if target == nil {
		return nil
	}

	resource.CurrentAutoScaling = &CurrentAutoScalingState{
		Target: target,
	}

	policies, err := m.autoScalingClient.DescribeScalingPolicies(ctx, cluster, serviceName)
	if err != nil {
		log.Debug("failed to get scaling policies", "error", err)
	} else {
		resource.CurrentAutoScaling.Policies = policies
	}

	return nil
}

func (resource *ServiceResource) determineAction() {
	if resource.Current == nil {
		resource.Action = ServiceActionCreate
		return
	}

	// Check if recreation is required (immutable field changed)
	recreateReasons := resource.checkRecreateRequired()
	if len(recreateReasons) > 0 {
		resource.Action = ServiceActionRecreate
		resource.RecreateReasons = recreateReasons
		return
	}

	if resource.hasChanges() {
		resource.Action = ServiceActionUpdate
		return
	}

	resource.Action = ServiceActionNoop
}

// checkRecreateRequired checks if any immutable fields have changed
// These fields cannot be updated in-place and require service recreation
func (resource *ServiceResource) checkRecreateRequired() []string {
	if resource.Current == nil || resource.Desired == nil {
		return nil
	}

	var reasons []string
	current := resource.Current
	desired := resource.Desired

	// LaunchType cannot be changed after service creation
	if desired.LaunchType != "" && string(current.LaunchType) != desired.LaunchType {
		reasons = append(reasons, fmt.Sprintf("launchType changed from %q to %q", current.LaunchType, desired.LaunchType))
	}

	// SchedulingStrategy cannot be changed after service creation
	if desired.SchedulingStrategy != "" && string(current.SchedulingStrategy) != desired.SchedulingStrategy {
		reasons = append(reasons, fmt.Sprintf("schedulingStrategy changed from %q to %q", current.SchedulingStrategy, desired.SchedulingStrategy))
	}

	// ServiceRegistries cannot be changed after service creation
	if resource.serviceRegistriesChanged() {
		reasons = append(reasons, "serviceRegistries changed (immutable after creation)")
	}

	// DeploymentController type cannot be changed
	if resource.deploymentControllerChanged() {
		reasons = append(reasons, "deploymentController changed (immutable after creation)")
	}

	return reasons
}

// loadBalancersChanged checks if load balancer configuration has changed
func (resource *ServiceResource) loadBalancersChanged() bool {
	current := resource.Current
	desired := resource.Desired

	if len(current.LoadBalancers) != len(desired.LoadBalancers) {
		return true
	}

	// Build map of current LBs for comparison
	currentLBs := make(map[string]bool)
	for _, lb := range current.LoadBalancers {
		key := fmt.Sprintf("%s:%s:%d",
			aws.ToString(lb.TargetGroupArn),
			aws.ToString(lb.ContainerName),
			aws.ToInt32(lb.ContainerPort))
		currentLBs[key] = true
	}

	// Check if all desired LBs exist in current
	for _, lb := range desired.LoadBalancers {
		key := fmt.Sprintf("%s:%s:%d", lb.TargetGroupArn, lb.ContainerName, lb.ContainerPort)
		if !currentLBs[key] {
			return true
		}
	}

	return false
}

// serviceRegistriesChanged checks if service registry configuration has changed
func (resource *ServiceResource) serviceRegistriesChanged() bool {
	current := resource.Current
	desired := resource.Desired

	if len(current.ServiceRegistries) != len(desired.ServiceRegistries) {
		return true
	}

	// Build map of current registries for comparison
	currentRegs := make(map[string]bool)
	for _, reg := range current.ServiceRegistries {
		currentRegs[aws.ToString(reg.RegistryArn)] = true
	}

	// Check if all desired registries exist in current
	for _, reg := range desired.ServiceRegistries {
		if !currentRegs[reg.RegistryArn] {
			return true
		}
	}

	return false
}

// deploymentControllerChanged checks if deployment controller type has changed
func (resource *ServiceResource) deploymentControllerChanged() bool {
	current := resource.Current
	desired := resource.Desired

	currentType := types.DeploymentControllerTypeEcs
	if current.DeploymentController != nil {
		currentType = current.DeploymentController.Type
	}

	desiredType := types.DeploymentControllerTypeEcs
	if desired.DeploymentController != "" {
		desiredType = types.DeploymentControllerType(desired.DeploymentController)
	}

	return currentType != desiredType
}

func (resource *ServiceResource) determineAutoScalingAction() {
	desired := resource.Desired
	if desired == nil {
		resource.AutoScalingAction = AutoScalingActionNoop
		return
	}

	hasDesiredAutoScaling := desired.AutoScaling != nil && desired.AutoScaling.MaxCapacity > 0
	hasCurrentAutoScaling := resource.CurrentAutoScaling != nil && resource.CurrentAutoScaling.Target != nil

	if !hasDesiredAutoScaling {
		if hasCurrentAutoScaling {
			resource.AutoScalingAction = AutoScalingActionDelete
		} else {
			resource.AutoScalingAction = AutoScalingActionNoop
		}
		return
	}

	if !hasCurrentAutoScaling {
		resource.AutoScalingAction = AutoScalingActionCreate
		return
	}

	if resource.hasAutoScalingChanges() {
		resource.AutoScalingAction = AutoScalingActionUpdate
		return
	}

	resource.AutoScalingAction = AutoScalingActionNoop
}

// RecalculateAction refreshes the service and autoscaling actions after mutating desired state.
func (resource *ServiceResource) RecalculateAction() {
	resource.determineAction()
	resource.determineAutoScalingAction()
}

func (resource *ServiceResource) hasAutoScalingChanges() bool {
	if resource.CurrentAutoScaling == nil || resource.Desired.AutoScaling == nil {
		return true
	}

	current := resource.CurrentAutoScaling.Target
	desired := resource.Desired.AutoScaling

	if current.MinCapacity != desired.MinCapacity {
		return true
	}
	if current.MaxCapacity != desired.MaxCapacity {
		return true
	}

	if len(resource.CurrentAutoScaling.Policies) != len(desired.Policies) {
		return true
	}

	for _, desiredPolicy := range desired.Policies {
		found := false
		for _, currentPolicy := range resource.CurrentAutoScaling.Policies {
			if strings.HasSuffix(currentPolicy.PolicyName, desiredPolicy.Name) {
				found = true
				if currentPolicy.TargetValue != desiredPolicy.TargetValue {
					return true
				}
				break
			}
		}
		if !found {
			return true
		}
	}

	return false
}

func (resource *ServiceResource) hasChanges() bool {
	if resource.Current == nil || resource.Desired == nil {
		return true
	}

	current := resource.Current
	desired := resource.Desired

	if resource.TaskDefinitionArn != "" {
		currentTaskDef := aws.ToString(current.TaskDefinition)
		if !taskDefArnMatches(currentTaskDef, resource.TaskDefinitionArn) {
			return true
		}
	}

	if int(current.DesiredCount) != desired.DesiredCount {
		return true
	}

	if string(current.LaunchType) != desired.LaunchType && desired.LaunchType != "" {
		return true
	}

	if current.EnableExecuteCommand != desired.EnableExecuteCommand {
		return true
	}

	if desired.HealthCheckGracePeriodSecondsSet {
		if aws.ToInt32(current.HealthCheckGracePeriodSeconds) != int32(desired.HealthCheckGracePeriodSeconds) {
			return true
		}
	}

	if current.NetworkConfiguration != nil && desired.NetworkConfiguration != nil {
		currentVpc := current.NetworkConfiguration.AwsvpcConfiguration
		desiredVpc := desired.NetworkConfiguration

		if !stringSliceEqual(currentVpc.Subnets, desiredVpc.Subnets) {
			return true
		}
		if !stringSliceEqual(currentVpc.SecurityGroups, desiredVpc.SecurityGroups) {
			return true
		}
	}

	if current.DeploymentConfiguration != nil {
		currentDC := current.DeploymentConfiguration
		desiredDC := desired.Deployment

		if desiredDC.MinimumHealthyPercentSet &&
			int(aws.ToInt32(currentDC.MinimumHealthyPercent)) != desiredDC.MinimumHealthyPercent {
			return true
		}
		if desiredDC.MaximumPercentSet &&
			int(aws.ToInt32(currentDC.MaximumPercent)) != desiredDC.MaximumPercent {
			return true
		}

		if currentDC.DeploymentCircuitBreaker != nil {
			if currentDC.DeploymentCircuitBreaker.Enable != desiredDC.CircuitBreakerEnable {
				return true
			}
			if currentDC.DeploymentCircuitBreaker.Rollback != desiredDC.CircuitBreakerRollback {
				return true
			}
		} else if desiredDC.CircuitBreakerEnable {
			return true
		}
	}

	if resource.loadBalancersChanged() {
		return true
	}

	return false
}

func taskDefArnMatches(currentArn, desiredArn string) bool {
	currentFamily := extractFamily(currentArn)
	desiredFamily := extractFamily(desiredArn)
	if currentFamily != desiredFamily {
		return false
	}

	currentRev := extractRevision(currentArn)
	desiredRev := extractRevision(desiredArn)
	return currentRev == desiredRev
}

func extractRevision(arn string) string {
	colonIdx := strings.LastIndex(arn, ":")
	if colonIdx > 0 && colonIdx < len(arn)-1 {
		return arn[colonIdx+1:]
	}
	return ""
}

func (m *ServiceManager) Create(ctx context.Context, resource *ServiceResource) error {
	input, err := resource.ToCreateInput()
	if err != nil {
		return err
	}

	created, err := m.ecsClient.CreateService(ctx, input)
	if err != nil {
		return err
	}

	resource.Current = created
	return nil
}

func (m *ServiceManager) Update(ctx context.Context, resource *ServiceResource) error {
	input, err := resource.ToUpdateInput()
	if err != nil {
		return err
	}

	updated, err := m.ecsClient.UpdateService(ctx, input)
	if err != nil {
		return err
	}

	resource.Current = updated
	return nil
}

func (m *ServiceManager) Delete(ctx context.Context, resource *ServiceResource) error {
	serviceName := resource.Name
	if resource.Desired != nil {
		serviceName = resource.Desired.Name
	}

	log.Info("deleting service for recreation", "service", serviceName)

	// First, delete auto-scaling if present
	if m.autoScalingClient != nil && resource.CurrentAutoScaling != nil {
		if err := m.deleteAutoScaling(ctx, resource); err != nil {
			log.Warn("failed to delete auto scaling during service deletion", "error", err)
		}
	}

	// Delete the service (force=true to stop tasks)
	if err := m.ecsClient.DeleteService(ctx, serviceName, true); err != nil {
		return fmt.Errorf("failed to delete service %s: %w", serviceName, err)
	}

	// Wait for service to become inactive
	if err := m.ecsClient.WaitForServiceInactive(ctx, serviceName); err != nil {
		return fmt.Errorf("failed waiting for service %s to become inactive: %w", serviceName, err)
	}

	resource.Current = nil
	return nil
}

func (m *ServiceManager) Recreate(ctx context.Context, resource *ServiceResource) error {
	log.Info("recreating service", "service", resource.Name, "reasons", resource.RecreateReasons)

	// Step 1: Delete the existing service
	if err := m.Delete(ctx, resource); err != nil {
		return fmt.Errorf("failed to delete service during recreation: %w", err)
	}

	// Step 2: Create the new service
	if err := m.Create(ctx, resource); err != nil {
		return fmt.Errorf("failed to create service during recreation: %w", err)
	}

	return nil
}

func (m *ServiceManager) Apply(ctx context.Context, resource *ServiceResource) error {
	switch resource.Action {
	case ServiceActionCreate:
		if err := m.Create(ctx, resource); err != nil {
			return err
		}
	case ServiceActionUpdate:
		if err := m.Update(ctx, resource); err != nil {
			return err
		}
	case ServiceActionDelete:
		if err := m.Delete(ctx, resource); err != nil {
			return err
		}
		return nil // Skip auto-scaling for delete
	case ServiceActionRecreate:
		if err := m.Recreate(ctx, resource); err != nil {
			return err
		}
	case ServiceActionNoop:
		log.Debug("no changes detected, skipping service update", "name", resource.Name)
	default:
		return fmt.Errorf("unknown action: %s", resource.Action)
	}

	if err := m.applyAutoScaling(ctx, resource); err != nil {
		return fmt.Errorf("failed to apply auto scaling: %w", err)
	}

	return nil
}

func (m *ServiceManager) applyAutoScaling(ctx context.Context, resource *ServiceResource) error {
	if m.autoScalingClient == nil {
		return nil
	}

	switch resource.AutoScalingAction {
	case AutoScalingActionCreate:
		return m.createAutoScaling(ctx, resource)
	case AutoScalingActionUpdate:
		return m.updateAutoScaling(ctx, resource)
	case AutoScalingActionDelete:
		return m.deleteAutoScaling(ctx, resource)
	case AutoScalingActionNoop:
		log.Debug("no auto scaling changes detected", "service", resource.Name)
		return nil
	default:
		return nil
	}
}

func (m *ServiceManager) createAutoScaling(ctx context.Context, resource *ServiceResource) error {
	desired := resource.Desired.AutoScaling
	cluster := resource.Desired.Cluster
	serviceName := resource.Desired.Name

	err := m.autoScalingClient.RegisterScalableTarget(ctx, &awsclient.RegisterScalableTargetInput{
		Cluster:     cluster,
		Service:     serviceName,
		MinCapacity: desired.MinCapacity,
		MaxCapacity: desired.MaxCapacity,
	})
	if err != nil {
		return fmt.Errorf("failed to register scalable target: %w", err)
	}

	for _, policy := range desired.Policies {
		if err := m.applyScalingPolicy(ctx, cluster, serviceName, policy); err != nil {
			return err
		}
	}

	log.Info("created auto scaling", "service", serviceName, "min", desired.MinCapacity, "max", desired.MaxCapacity)
	return nil
}

func (m *ServiceManager) updateAutoScaling(ctx context.Context, resource *ServiceResource) error {
	desired := resource.Desired.AutoScaling
	cluster := resource.Desired.Cluster
	serviceName := resource.Desired.Name

	err := m.autoScalingClient.RegisterScalableTarget(ctx, &awsclient.RegisterScalableTargetInput{
		Cluster:     cluster,
		Service:     serviceName,
		MinCapacity: desired.MinCapacity,
		MaxCapacity: desired.MaxCapacity,
	})
	if err != nil {
		return fmt.Errorf("failed to update scalable target: %w", err)
	}

	desiredPolicyNames := make(map[string]bool)
	for _, policy := range desired.Policies {
		desiredPolicyNames[policy.Name] = true
		if err := m.applyScalingPolicy(ctx, cluster, serviceName, policy); err != nil {
			return err
		}
	}

	if resource.CurrentAutoScaling != nil {
		for _, currentPolicy := range resource.CurrentAutoScaling.Policies {
			policyName := extractPolicyName(currentPolicy.PolicyName, serviceName)
			if !desiredPolicyNames[policyName] {
				if err := m.autoScalingClient.DeleteScalingPolicy(ctx, cluster, serviceName, policyName); err != nil {
					log.Warn("failed to delete old scaling policy", "policy", policyName, "error", err)
				}
			}
		}
	}

	log.Info("updated auto scaling", "service", serviceName)
	return nil
}

func (m *ServiceManager) deleteAutoScaling(ctx context.Context, resource *ServiceResource) error {
	cluster := resource.Desired.Cluster
	serviceName := resource.Desired.Name

	if resource.CurrentAutoScaling != nil {
		for _, policy := range resource.CurrentAutoScaling.Policies {
			policyName := extractPolicyName(policy.PolicyName, serviceName)
			if err := m.autoScalingClient.DeleteScalingPolicy(ctx, cluster, serviceName, policyName); err != nil {
				log.Warn("failed to delete scaling policy", "policy", policyName, "error", err)
			}
		}
	}

	if err := m.autoScalingClient.DeregisterScalableTarget(ctx, cluster, serviceName); err != nil {
		return fmt.Errorf("failed to deregister scalable target: %w", err)
	}

	log.Info("deleted auto scaling", "service", serviceName)
	return nil
}

func (m *ServiceManager) applyScalingPolicy(ctx context.Context, cluster, serviceName string, policy config.ScalingPolicy) error {
	switch policy.Type {
	case "TargetTrackingScaling", "":
		return m.applyTargetTrackingPolicy(ctx, cluster, serviceName, policy)
	case "StepScaling":
		return m.applyStepScalingPolicy(ctx, cluster, serviceName, policy)
	default:
		return fmt.Errorf("unsupported scaling policy type: %s", policy.Type)
	}
}

func (m *ServiceManager) applyTargetTrackingPolicy(ctx context.Context, cluster, serviceName string, policy config.ScalingPolicy) error {
	input := &awsclient.TargetTrackingPolicyInput{
		Cluster:          cluster,
		Service:          serviceName,
		PolicyName:       policy.Name,
		TargetValue:      policy.TargetValue,
		ScaleInCooldown:  policy.ScaleInCooldown,
		ScaleOutCooldown: policy.ScaleOutCooldown,
	}

	if policy.PredefinedMetric != "" {
		input.PredefinedMetric = normalizePredefinedMetric(policy.PredefinedMetric)
	} else if policy.CustomMetricSpec != nil {
		input.CustomMetric = &awsclient.CustomMetricSpec{
			Namespace:  policy.CustomMetricSpec.Namespace,
			MetricName: policy.CustomMetricSpec.MetricName,
			Statistic:  policy.CustomMetricSpec.Statistic,
		}
		for _, dim := range policy.CustomMetricSpec.Dimensions {
			input.CustomMetric.Dimensions = append(input.CustomMetric.Dimensions, awsclient.MetricDimension{
				Name:  dim.Name,
				Value: dim.Value,
			})
		}
	}

	return m.autoScalingClient.PutTargetTrackingScalingPolicy(ctx, input)
}

func (m *ServiceManager) applyStepScalingPolicy(ctx context.Context, cluster, serviceName string, policy config.ScalingPolicy) error {
	input := &awsclient.StepScalingPolicyInput{
		Cluster:               cluster,
		Service:               serviceName,
		PolicyName:            policy.Name,
		AdjustmentType:        "ChangeInCapacity",
		Cooldown:              policy.ScaleOutCooldown,
		MetricAggregationType: "Average",
	}

	return m.autoScalingClient.PutStepScalingPolicy(ctx, input)
}

func extractPolicyName(fullName, serviceName string) string {
	prefix := serviceName + "-"
	if strings.HasPrefix(fullName, prefix) {
		return strings.TrimPrefix(fullName, prefix)
	}
	return fullName
}

func normalizePredefinedMetric(metric string) string {
	switch metric {
	case "cpu", "CPU":
		return "ECSServiceAverageCPUUtilization"
	case "memory", "Memory":
		return "ECSServiceAverageMemoryUtilization"
	case "alb", "ALB":
		return "ALBRequestCountPerTarget"
	default:
		return metric
	}
}

func (m *ServiceManager) WaitForSteadyState(ctx context.Context, serviceName string) error {
	return m.ecsClient.WaitForSteadyState(ctx, serviceName)
}

func (m *ServiceManager) GetDeploymentStatus(ctx context.Context, serviceName string) (*awsclient.DeploymentStatus, error) {
	return m.ecsClient.GetDeploymentStatus(ctx, serviceName)
}
