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

type TaskDefAction string

const (
	TaskDefActionCreate TaskDefAction = "CREATE"
	TaskDefActionUpdate TaskDefAction = "UPDATE"
	TaskDefActionNoop   TaskDefAction = "NOOP"
)

type TaskDefResource struct {
	Name    string
	Type    string // managed, merged, remote
	Desired *config.TaskDefinition
	Current *types.TaskDefinition
	Action  TaskDefAction

	// Resolved ARN after registration or discovery
	ResolvedArn string
}

// ToRegisterInput converts a managed task definition config to ECS RegisterTaskDefinitionInput
func (r *TaskDefResource) ToRegisterInput() (*ecs.RegisterTaskDefinitionInput, error) {
	if r.Desired == nil {
		return nil, fmt.Errorf("no desired state for task definition %s", r.Name)
	}

	td := r.Desired

	input := &ecs.RegisterTaskDefinitionInput{
		Family:                  aws.String(td.Family),
		NetworkMode:             types.NetworkMode(td.NetworkMode),
		RequiresCompatibilities: make([]types.Compatibility, 0, len(td.RequiresCompatibilities)),
		ContainerDefinitions:    make([]types.ContainerDefinition, 0, len(td.ContainerDefinitions)),
	}

	if td.CPU != "" {
		input.Cpu = aws.String(td.CPU)
	}
	if td.Memory != "" {
		input.Memory = aws.String(td.Memory)
	}
	if td.ExecutionRoleArn != "" {
		input.ExecutionRoleArn = aws.String(td.ExecutionRoleArn)
	}
	if td.TaskRoleArn != "" {
		input.TaskRoleArn = aws.String(td.TaskRoleArn)
	}

	for _, compat := range td.RequiresCompatibilities {
		input.RequiresCompatibilities = append(input.RequiresCompatibilities, types.Compatibility(compat))
	}

	if td.RuntimePlatform != nil {
		input.RuntimePlatform = &types.RuntimePlatform{}
		if td.RuntimePlatform.CPUArchitecture != "" {
			input.RuntimePlatform.CpuArchitecture = types.CPUArchitecture(td.RuntimePlatform.CPUArchitecture)
		}
		if td.RuntimePlatform.OperatingSystemFamily != "" {
			input.RuntimePlatform.OperatingSystemFamily = types.OSFamily(td.RuntimePlatform.OperatingSystemFamily)
		}
	}

	for _, cd := range td.ContainerDefinitions {
		containerDef := convertContainerDefinition(cd)
		input.ContainerDefinitions = append(input.ContainerDefinitions, containerDef)
	}

	for _, vol := range td.Volumes {
		volume := types.Volume{
			Name: aws.String(vol.Name),
		}
		if vol.HostPath != "" {
			volume.Host = &types.HostVolumeProperties{
				SourcePath: aws.String(vol.HostPath),
			}
		}
		if vol.EFSVolumeConfiguration != nil {
			efsConfig := &types.EFSVolumeConfiguration{
				FileSystemId: aws.String(vol.EFSVolumeConfiguration.FileSystemID),
			}
			if vol.EFSVolumeConfiguration.RootDirectory != "" {
				efsConfig.RootDirectory = aws.String(vol.EFSVolumeConfiguration.RootDirectory)
			}
			if vol.EFSVolumeConfiguration.TransitEncryption != "" {
				efsConfig.TransitEncryption = types.EFSTransitEncryption(vol.EFSVolumeConfiguration.TransitEncryption)
			}
			if vol.EFSVolumeConfiguration.TransitEncryptionPort > 0 {
				efsConfig.TransitEncryptionPort = aws.Int32(int32(vol.EFSVolumeConfiguration.TransitEncryptionPort))
			}
			if vol.EFSVolumeConfiguration.AuthorizationConfig != nil {
				efsConfig.AuthorizationConfig = &types.EFSAuthorizationConfig{}
				if vol.EFSVolumeConfiguration.AuthorizationConfig.AccessPointID != "" {
					efsConfig.AuthorizationConfig.AccessPointId = aws.String(vol.EFSVolumeConfiguration.AuthorizationConfig.AccessPointID)
				}
				if vol.EFSVolumeConfiguration.AuthorizationConfig.IAM != "" {
					efsConfig.AuthorizationConfig.Iam = types.EFSAuthorizationConfigIAM(vol.EFSVolumeConfiguration.AuthorizationConfig.IAM)
				}
			}
			volume.EfsVolumeConfiguration = efsConfig
		}
		input.Volumes = append(input.Volumes, volume)
	}

	return input, nil
}

func convertContainerDefinition(cd config.ContainerDefinition) types.ContainerDefinition {
	containerDef := types.ContainerDefinition{
		Name:      aws.String(cd.Name),
		Image:     aws.String(cd.Image),
		Essential: aws.Bool(cd.Essential),
	}

	if cd.CPU > 0 {
		containerDef.Cpu = int32(cd.CPU)
	}
	if cd.Memory > 0 {
		containerDef.Memory = aws.Int32(int32(cd.Memory))
	}
	if cd.WorkingDirectory != "" {
		containerDef.WorkingDirectory = aws.String(cd.WorkingDirectory)
	}
	if len(cd.Command) > 0 {
		containerDef.Command = cd.Command
	}
	if len(cd.EntryPoint) > 0 {
		containerDef.EntryPoint = cd.EntryPoint
	}

	for _, env := range cd.Environment {
		containerDef.Environment = append(containerDef.Environment, types.KeyValuePair{
			Name:  aws.String(env.Name),
			Value: aws.String(env.Value),
		})
	}

	for _, secret := range cd.Secrets {
		containerDef.Secrets = append(containerDef.Secrets, types.Secret{
			Name:      aws.String(secret.Name),
			ValueFrom: aws.String(secret.ValueFrom),
		})
	}

	for _, pm := range cd.PortMappings {
		portMapping := types.PortMapping{
			ContainerPort: aws.Int32(int32(pm.ContainerPort)),
		}
		if pm.HostPort > 0 {
			portMapping.HostPort = aws.Int32(int32(pm.HostPort))
		}
		if pm.Protocol != "" {
			portMapping.Protocol = types.TransportProtocol(pm.Protocol)
		}
		if pm.Name != "" {
			portMapping.Name = aws.String(pm.Name)
		}
		if pm.AppProtocol != "" {
			portMapping.AppProtocol = types.ApplicationProtocol(pm.AppProtocol)
		}
		containerDef.PortMappings = append(containerDef.PortMappings, portMapping)
	}

	for _, mp := range cd.MountPoints {
		containerDef.MountPoints = append(containerDef.MountPoints, types.MountPoint{
			SourceVolume:  aws.String(mp.SourceVolume),
			ContainerPath: aws.String(mp.ContainerPath),
			ReadOnly:      aws.Bool(mp.ReadOnly),
		})
	}

	if cd.HealthCheck != nil {
		containerDef.HealthCheck = &types.HealthCheck{
			Command:     cd.HealthCheck.Command,
			Interval:    aws.Int32(int32(cd.HealthCheck.Interval)),
			Timeout:     aws.Int32(int32(cd.HealthCheck.Timeout)),
			Retries:     aws.Int32(int32(cd.HealthCheck.Retries)),
			StartPeriod: aws.Int32(int32(cd.HealthCheck.StartPeriod)),
		}
	}

	if cd.LogConfiguration != nil {
		containerDef.LogConfiguration = &types.LogConfiguration{
			LogDriver: types.LogDriver(cd.LogConfiguration.LogDriver),
		}
		if len(cd.LogConfiguration.Options) > 0 {
			containerDef.LogConfiguration.Options = cd.LogConfiguration.Options
		}
		for _, so := range cd.LogConfiguration.SecretOptions {
			containerDef.LogConfiguration.SecretOptions = append(
				containerDef.LogConfiguration.SecretOptions,
				types.Secret{
					Name:      aws.String(so.Name),
					ValueFrom: aws.String(so.ValueFrom),
				},
			)
		}
	}

	for _, dep := range cd.DependsOn {
		containerDef.DependsOn = append(containerDef.DependsOn, types.ContainerDependency{
			ContainerName: aws.String(dep.ContainerName),
			Condition:     types.ContainerCondition(dep.Condition),
		})
	}

	if cd.LinuxParameters != nil {
		containerDef.LinuxParameters = &types.LinuxParameters{}
		if cd.LinuxParameters.InitProcessEnabled {
			containerDef.LinuxParameters.InitProcessEnabled = aws.Bool(true)
		}
		if cd.LinuxParameters.Capabilities != nil {
			containerDef.LinuxParameters.Capabilities = &types.KernelCapabilities{}
			if len(cd.LinuxParameters.Capabilities.Add) > 0 {
				containerDef.LinuxParameters.Capabilities.Add = cd.LinuxParameters.Capabilities.Add
			}
			if len(cd.LinuxParameters.Capabilities.Drop) > 0 {
				containerDef.LinuxParameters.Capabilities.Drop = cd.LinuxParameters.Capabilities.Drop
			}
		}
	}

	for _, ul := range cd.Ulimits {
		containerDef.Ulimits = append(containerDef.Ulimits, types.Ulimit{
			Name:      types.UlimitName(ul.Name),
			SoftLimit: int32(ul.SoftLimit),
			HardLimit: int32(ul.HardLimit),
		})
	}

	return containerDef
}

type TaskDefManager struct {
	ecsClient *awsclient.ECSClient
}

func NewTaskDefManager(ecsClient *awsclient.ECSClient) *TaskDefManager {
	return &TaskDefManager{ecsClient: ecsClient}
}

// BuildResource creates a TaskDefResource from a config.TaskDefinition and discovers current state
func (m *TaskDefManager) BuildResource(ctx context.Context, name string, td *config.TaskDefinition) (*TaskDefResource, error) {
	resource := &TaskDefResource{
		Name:    name,
		Type:    td.Type,
		Desired: td,
	}

	switch td.Type {
	case "managed":
		if err := m.discoverManagedTaskDef(ctx, resource); err != nil {
			log.Debug("failed to discover managed task definition", "name", name, "error", err)
		}
		resource.determineAction()

	case "merged":
		if err := m.buildMergedTaskDef(ctx, resource); err != nil {
			return nil, fmt.Errorf("failed to build merged task definition %s: %w", name, err)
		}
		resource.determineAction()

	case "remote":
		if err := m.discoverRemoteTaskDef(ctx, resource); err != nil {
			return nil, fmt.Errorf("failed to discover remote task definition %s: %w", name, err)
		}
		resource.Action = TaskDefActionNoop

	default:
		return nil, fmt.Errorf("unknown task definition type: %s", td.Type)
	}

	return resource, nil
}

func (m *TaskDefManager) discoverManagedTaskDef(ctx context.Context, resource *TaskDefResource) error {
	family := resource.Desired.Family
	log.Debug("discovering managed task definition", "family", family)

	current, err := m.ecsClient.DescribeTaskDefinition(ctx, family)
	if err != nil {
		if strings.Contains(err.Error(), "Unable to describe task definition") {
			log.Debug("task definition does not exist", "family", family)
			return nil
		}
		return err
	}

	resource.Current = current
	resource.ResolvedArn = aws.ToString(current.TaskDefinitionArn)
	return nil
}

func (m *TaskDefManager) buildMergedTaskDef(ctx context.Context, resource *TaskDefResource) error {
	baseArn := resource.Desired.BaseArn
	log.Debug("building merged task definition", "baseArn", baseArn)

	base, err := m.ecsClient.DescribeTaskDefinition(ctx, baseArn)
	if err != nil {
		return fmt.Errorf("failed to fetch base task definition %s: %w", baseArn, err)
	}

	merged := m.applyOverrides(base, resource.Desired)
	resource.Desired = merged

	family := extractFamily(baseArn)
	if resource.Desired.Family == "" {
		resource.Desired.Family = family
	}

	current, err := m.ecsClient.DescribeTaskDefinition(ctx, resource.Desired.Family)
	if err == nil {
		resource.Current = current
		resource.ResolvedArn = aws.ToString(current.TaskDefinitionArn)
	}

	return nil
}

func (m *TaskDefManager) applyOverrides(base *types.TaskDefinition, td *config.TaskDefinition) *config.TaskDefinition {
	merged := &config.TaskDefinition{
		Name:                     td.Name,
		Type:                     "managed",
		Family:                   aws.ToString(base.Family),
		CPU:                      aws.ToString(base.Cpu),
		Memory:                   aws.ToString(base.Memory),
		NetworkMode:              string(base.NetworkMode),
		ExecutionRoleArn:         aws.ToString(base.ExecutionRoleArn),
		TaskRoleArn:              aws.ToString(base.TaskRoleArn),
		RequiresCompatibilities:  make([]string, 0, len(base.RequiresCompatibilities)),
		ContainerDefinitions:     make([]config.ContainerDefinition, 0, len(base.ContainerDefinitions)),
	}

	for _, compat := range base.RequiresCompatibilities {
		merged.RequiresCompatibilities = append(merged.RequiresCompatibilities, string(compat))
	}

	for _, cd := range base.ContainerDefinitions {
		merged.ContainerDefinitions = append(merged.ContainerDefinitions, convertECSContainerDefinition(cd))
	}

	if td.Overrides != nil {
		if td.Overrides.CPU != "" {
			merged.CPU = td.Overrides.CPU
		}
		if td.Overrides.Memory != "" {
			merged.Memory = td.Overrides.Memory
		}
		if td.Overrides.ExecutionRoleArn != "" {
			merged.ExecutionRoleArn = td.Overrides.ExecutionRoleArn
		}
		if td.Overrides.TaskRoleArn != "" {
			merged.TaskRoleArn = td.Overrides.TaskRoleArn
		}

		for _, override := range td.Overrides.ContainerDefinitions {
			for i, cd := range merged.ContainerDefinitions {
				if cd.Name == override.Name {
					if override.Image != "" {
						merged.ContainerDefinitions[i].Image = override.Image
					}
					if override.CPU > 0 {
						merged.ContainerDefinitions[i].CPU = override.CPU
					}
					if override.Memory > 0 {
						merged.ContainerDefinitions[i].Memory = override.Memory
					}
					if len(override.Command) > 0 {
						merged.ContainerDefinitions[i].Command = override.Command
					}
					if len(override.Environment) > 0 {
						merged.ContainerDefinitions[i].Environment = mergeEnvironment(
							merged.ContainerDefinitions[i].Environment,
							override.Environment,
						)
					}
					if len(override.Secrets) > 0 {
						merged.ContainerDefinitions[i].Secrets = mergeSecrets(
							merged.ContainerDefinitions[i].Secrets,
							override.Secrets,
						)
					}
					break
				}
			}
		}
	}

	return merged
}

func convertECSContainerDefinition(cd types.ContainerDefinition) config.ContainerDefinition {
	result := config.ContainerDefinition{
		Name:      aws.ToString(cd.Name),
		Image:     aws.ToString(cd.Image),
		CPU:       int(cd.Cpu),
		Memory:    int(aws.ToInt32(cd.Memory)),
		Essential: aws.ToBool(cd.Essential),
		Command:   cd.Command,
		EntryPoint: cd.EntryPoint,
	}

	if cd.WorkingDirectory != nil {
		result.WorkingDirectory = aws.ToString(cd.WorkingDirectory)
	}

	for _, env := range cd.Environment {
		result.Environment = append(result.Environment, config.KeyValuePair{
			Name:  aws.ToString(env.Name),
			Value: aws.ToString(env.Value),
		})
	}

	for _, secret := range cd.Secrets {
		result.Secrets = append(result.Secrets, config.Secret{
			Name:      aws.ToString(secret.Name),
			ValueFrom: aws.ToString(secret.ValueFrom),
		})
	}

	for _, pm := range cd.PortMappings {
		result.PortMappings = append(result.PortMappings, config.PortMapping{
			ContainerPort: int(aws.ToInt32(pm.ContainerPort)),
			HostPort:      int(aws.ToInt32(pm.HostPort)),
			Protocol:      string(pm.Protocol),
			Name:          aws.ToString(pm.Name),
			AppProtocol:   string(pm.AppProtocol),
		})
	}

	for _, mp := range cd.MountPoints {
		result.MountPoints = append(result.MountPoints, config.MountPoint{
			SourceVolume:  aws.ToString(mp.SourceVolume),
			ContainerPath: aws.ToString(mp.ContainerPath),
			ReadOnly:      aws.ToBool(mp.ReadOnly),
		})
	}

	if cd.HealthCheck != nil {
		result.HealthCheck = &config.HealthCheck{
			Command:     cd.HealthCheck.Command,
			Interval:    int(aws.ToInt32(cd.HealthCheck.Interval)),
			Timeout:     int(aws.ToInt32(cd.HealthCheck.Timeout)),
			Retries:     int(aws.ToInt32(cd.HealthCheck.Retries)),
			StartPeriod: int(aws.ToInt32(cd.HealthCheck.StartPeriod)),
		}
	}

	if cd.LogConfiguration != nil {
		result.LogConfiguration = &config.LogConfiguration{
			LogDriver: string(cd.LogConfiguration.LogDriver),
			Options:   cd.LogConfiguration.Options,
		}
		for _, so := range cd.LogConfiguration.SecretOptions {
			result.LogConfiguration.SecretOptions = append(result.LogConfiguration.SecretOptions, config.Secret{
				Name:      aws.ToString(so.Name),
				ValueFrom: aws.ToString(so.ValueFrom),
			})
		}
	}

	for _, dep := range cd.DependsOn {
		result.DependsOn = append(result.DependsOn, config.ContainerDependency{
			ContainerName: aws.ToString(dep.ContainerName),
			Condition:     string(dep.Condition),
		})
	}

	if cd.LinuxParameters != nil {
		result.LinuxParameters = &config.LinuxParameters{}
		if cd.LinuxParameters.InitProcessEnabled != nil {
			result.LinuxParameters.InitProcessEnabled = *cd.LinuxParameters.InitProcessEnabled
		}
		if cd.LinuxParameters.Capabilities != nil {
			result.LinuxParameters.Capabilities = &config.KernelCapabilities{
				Add:  cd.LinuxParameters.Capabilities.Add,
				Drop: cd.LinuxParameters.Capabilities.Drop,
			}
		}
	}

	for _, ul := range cd.Ulimits {
		result.Ulimits = append(result.Ulimits, config.Ulimit{
			Name:      string(ul.Name),
			SoftLimit: int(ul.SoftLimit),
			HardLimit: int(ul.HardLimit),
		})
	}

	return result
}

func (m *TaskDefManager) discoverRemoteTaskDef(ctx context.Context, resource *TaskDefResource) error {
	arn := resource.Desired.Arn
	log.Debug("discovering remote task definition", "arn", arn)

	current, err := m.ecsClient.DescribeTaskDefinition(ctx, arn)
	if err != nil {
		return fmt.Errorf("failed to fetch remote task definition %s: %w", arn, err)
	}

	resource.Current = current
	resource.ResolvedArn = aws.ToString(current.TaskDefinitionArn)
	return nil
}

func (resource *TaskDefResource) determineAction() {
	if resource.Current == nil {
		resource.Action = TaskDefActionCreate
		return
	}

	if resource.hasChanges() {
		resource.Action = TaskDefActionUpdate
		return
	}

	resource.Action = TaskDefActionNoop
}

func (resource *TaskDefResource) hasChanges() bool {
	if resource.Current == nil || resource.Desired == nil {
		return true
	}

	current := resource.Current
	desired := resource.Desired

	if aws.ToString(current.Cpu) != desired.CPU {
		return true
	}
	if aws.ToString(current.Memory) != desired.Memory {
		return true
	}
	if string(current.NetworkMode) != desired.NetworkMode {
		return true
	}
	if aws.ToString(current.ExecutionRoleArn) != desired.ExecutionRoleArn {
		return true
	}
	if aws.ToString(current.TaskRoleArn) != desired.TaskRoleArn {
		return true
	}

	if len(current.ContainerDefinitions) != len(desired.ContainerDefinitions) {
		return true
	}

	for i, cd := range desired.ContainerDefinitions {
		if i >= len(current.ContainerDefinitions) {
			return true
		}
		if hasContainerChanges(current.ContainerDefinitions[i], cd) {
			return true
		}
	}

	return false
}

func hasContainerChanges(current types.ContainerDefinition, desired config.ContainerDefinition) bool {
	if aws.ToString(current.Name) != desired.Name {
		return true
	}
	if aws.ToString(current.Image) != desired.Image {
		return true
	}
	if int(current.Cpu) != desired.CPU {
		return true
	}
	if int(aws.ToInt32(current.Memory)) != desired.Memory && desired.Memory != 0 {
		return true
	}
	if !stringSliceEqual(current.Command, desired.Command) {
		return true
	}
	if !stringSliceEqual(current.EntryPoint, desired.EntryPoint) {
		return true
	}

	if len(current.Environment) != len(desired.Environment) {
		return true
	}
	for _, dEnv := range desired.Environment {
		found := false
		for _, cEnv := range current.Environment {
			if aws.ToString(cEnv.Name) == dEnv.Name && aws.ToString(cEnv.Value) == dEnv.Value {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}

	if len(current.Secrets) != len(desired.Secrets) {
		return true
	}
	for _, dSecret := range desired.Secrets {
		found := false
		for _, cSecret := range current.Secrets {
			if aws.ToString(cSecret.Name) == dSecret.Name && aws.ToString(cSecret.ValueFrom) == dSecret.ValueFrom {
				found = true
				break
			}
		}
		if !found {
			return true
		}
	}

	return false
}

// Register registers the task definition with ECS
func (m *TaskDefManager) Register(ctx context.Context, resource *TaskDefResource) error {
	if resource.Type == "remote" {
		log.Debug("skipping registration of remote task definition", "name", resource.Name)
		return nil
	}

	if resource.Action == TaskDefActionNoop {
		log.Debug("no changes detected, skipping registration", "name", resource.Name)
		return nil
	}

	input, err := resource.ToRegisterInput()
	if err != nil {
		return err
	}

	registered, err := m.ecsClient.RegisterTaskDefinition(ctx, input)
	if err != nil {
		return err
	}

	resource.ResolvedArn = aws.ToString(registered.TaskDefinitionArn)
	resource.Current = registered
	return nil
}

func extractFamily(arn string) string {
	familyRev := arn

	parts := strings.Split(arn, "/")
	if len(parts) >= 2 {
		familyRev = parts[len(parts)-1]
	}

	colonIdx := strings.LastIndex(familyRev, ":")
	if colonIdx > 0 {
		return familyRev[:colonIdx]
	}
	return familyRev
}

func mergeEnvironment(base, override []config.KeyValuePair) []config.KeyValuePair {
	envMap := make(map[string]string)
	for _, e := range base {
		envMap[e.Name] = e.Value
	}
	for _, e := range override {
		envMap[e.Name] = e.Value
	}

	result := make([]config.KeyValuePair, 0, len(envMap))
	for name, value := range envMap {
		result = append(result, config.KeyValuePair{Name: name, Value: value})
	}
	return result
}

func mergeSecrets(base, override []config.Secret) []config.Secret {
	secretMap := make(map[string]string)
	for _, s := range base {
		secretMap[s.Name] = s.ValueFrom
	}
	for _, s := range override {
		secretMap[s.Name] = s.ValueFrom
	}

	result := make([]config.Secret, 0, len(secretMap))
	for name, valueFrom := range secretMap {
		result = append(result, config.Secret{Name: name, ValueFrom: valueFrom})
	}
	return result
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
