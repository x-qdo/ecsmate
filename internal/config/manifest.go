package config

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue"

	"github.com/x-qdo/ecsmate/internal/log"
)

type Manifest struct {
	Name            string
	Tags            map[string]string
	Secrets         *SecretsConfig
	TaskDefinitions map[string]TaskDefinition
	Services        map[string]Service
	ScheduledTasks  map[string]ScheduledTask
	Ingress         *Ingress
}

type SecretsConfig struct {
	Managed  *ManagedSecretsConfig
	External map[string]string
}

type ManagedSecretsConfig struct {
	File         string
	KMSKeyArn    string
	KMSKeyRegion string
	SSMKMSKeyID  string
	SSMPrefix    string
}

type LogGroup struct {
	Name            string
	RetentionInDays int
	KMSKeyID        string
	Tags            map[string]string
}

type Ingress struct {
	ListenerArn string
	VpcID       string
	Rules       []IngressRule
}

type IngressRule struct {
	Priority int

	// Match conditions
	Host  string
	Hosts []string
	Paths []string

	// Backend (one of these)
	Service       *IngressServiceBackend
	Redirect      *IngressRedirect
	FixedResponse *IngressFixedResponse

	// Target group settings (when using service backend)
	HealthCheck         *TargetGroupHealthCheck
	DeregistrationDelay int
	Tags                map[string]string
}

type IngressServiceBackend struct {
	Name          string // service name in manifest
	ContainerName string
	ContainerPort int
}

type IngressRedirect struct {
	StatusCode string
	Protocol   string
	Host       string
	Port       string
	Path       string
	Query      string
}

type IngressFixedResponse struct {
	StatusCode  string
	ContentType string
	MessageBody string
}

type TargetGroupHealthCheck struct {
	Path               string
	Protocol           string
	Port               string
	HealthyThreshold   int
	UnhealthyThreshold int
	Timeout            int
	Interval           int
	Matcher            string
}

type TaskDefinition struct {
	Name string
	Type string // managed, merged, remote

	// For managed type
	Family                  string
	CPU                     string
	Memory                  string
	NetworkMode             string
	RequiresCompatibilities []string
	ExecutionRoleArn        string
	TaskRoleArn             string
	ContainerDefinitions    []ContainerDefinition
	Volumes                 []Volume
	RuntimePlatform         *RuntimePlatform

	// For merged type
	BaseArn   string
	Overrides *TaskDefOverrides

	// For remote type
	Arn string
}

type RuntimePlatform struct {
	CPUArchitecture       string
	OperatingSystemFamily string
}

type TaskDefOverrides struct {
	CPU                  string
	Memory               string
	ExecutionRoleArn     string
	TaskRoleArn          string
	ContainerDefinitions []ContainerOverride
}

type ContainerDefinition struct {
	Name             string
	Image            string
	CPU              int
	Memory           int
	Essential        bool
	PortMappings     []PortMapping
	Environment      []KeyValuePair
	Secrets          []Secret
	MountPoints      []MountPoint
	Command          []string
	EntryPoint       []string
	WorkingDirectory string
	HealthCheck      *HealthCheck
	LogConfiguration *LogConfiguration
	DependsOn        []ContainerDependency
	LinuxParameters  *LinuxParameters
	Ulimits          []Ulimit
}

type LinuxParameters struct {
	InitProcessEnabled bool
	Capabilities       *KernelCapabilities
}

type KernelCapabilities struct {
	Add  []string
	Drop []string
}

type Ulimit struct {
	Name      string // core, cpu, data, fsize, locks, memlock, msgqueue, nice, nofile, nproc, rss, rtprio, rttime, sigpending, stack
	SoftLimit int
	HardLimit int
}

type ContainerOverride struct {
	Name        string
	Image       string
	CPU         int
	Memory      int
	Environment []KeyValuePair
	Secrets     []Secret
	Command     []string
}

type PortMapping struct {
	ContainerPort int
	HostPort      int
	Protocol      string
	Name          string
	AppProtocol   string
}

type KeyValuePair struct {
	Name  string
	Value string
}

type Secret struct {
	Name      string
	ValueFrom string
}

type MountPoint struct {
	SourceVolume  string
	ContainerPath string
	ReadOnly      bool
}

type Volume struct {
	Name                   string
	HostPath               string
	EFSVolumeConfiguration *EFSVolumeConfig
}

type EFSVolumeConfig struct {
	FileSystemID          string
	RootDirectory         string
	TransitEncryption     string
	TransitEncryptionPort int
	AuthorizationConfig   *EFSAuthConfig
}

type EFSAuthConfig struct {
	AccessPointID string
	IAM           string // ENABLED or DISABLED
}

type HealthCheck struct {
	Command     []string
	Interval    int
	Timeout     int
	Retries     int
	StartPeriod int
}

type LogConfiguration struct {
	LogDriver     string
	Options       map[string]string
	SecretOptions []Secret

	// Log group management (only for awslogs driver)
	CreateLogGroup      bool
	RetentionInDays     int
	KMSKeyID            string
	LogGroupTags        map[string]string
	SubscriptionFilters []SubscriptionFilter
}

type SubscriptionFilter struct {
	Name           string
	DestinationArn string
	FilterPattern  string
	RoleArn        string
	Distribution   string
}

type ContainerDependency struct {
	ContainerName string
	Condition     string
}

type Service struct {
	Name                             string
	Cluster                          string
	TaskDefinition                   string
	DesiredCount                     int
	LaunchType                       string
	CapacityProviderStrategy         []CapacityProviderStrategyItem
	PlatformVersion                  string
	SchedulingStrategy               string // REPLICA or DAEMON
	DeploymentController             string // ECS, CODE_DEPLOY, or EXTERNAL
	EnableExecuteCommand             bool
	HealthCheckGracePeriodSeconds    int
	HealthCheckGracePeriodSecondsSet bool
	NetworkConfiguration             *NetworkConfiguration
	LoadBalancers                    []LoadBalancer
	ServiceRegistries                []ServiceRegistry
	Deployment                       DeploymentConfig
	DependsOn                        []string
	AutoScaling                      *AutoScalingConfig
	Hooks                            *Hooks
}

type Hooks struct {
	PreHook  *Hook
	PostHook *Hook
}

type Hook struct {
	TaskDefinition     string
	ContainerOverrides []HookContainerOverride
	Timeout            int // seconds, default 600
}

type HookContainerOverride struct {
	Name        string
	Command     []string
	Environment []KeyValuePair
}

type CapacityProviderStrategyItem struct {
	CapacityProvider string
	Weight           int
	Base             int
}

type ServiceRegistry struct {
	RegistryArn      string
	ServiceDiscovery *ServiceDiscoveryConfig
	ContainerName    string
	ContainerPort    int
	Port             int
}

type ServiceDiscoveryConfig struct {
	NamespaceArn  string
	NamespaceID   string
	Name          string
	DNSRecordType string
	DNSTTL        int
	RoutingPolicy string
	Tags          map[string]string
}

type NetworkConfiguration struct {
	Subnets        []string
	SecurityGroups []string
	AssignPublicIp string
}

type LoadBalancer struct {
	TargetGroupArn string
	ContainerName  string
	ContainerPort  int
}

type DeploymentConfig struct {
	Strategy string // rolling, gradual

	// Rolling/Gradual config
	MinimumHealthyPercent    int
	MaximumPercent           int
	MinimumHealthyPercentSet bool
	MaximumPercentSet        bool
	CircuitBreakerEnable     bool
	CircuitBreakerRollback   bool

	// Deployment alarms (ECS native)
	Alarms              []string
	AlarmRollbackEnable bool

	// Gradual deployment steps
	GradualSteps []GradualStep
}

type GradualStep struct {
	Percent     int // Percentage of desired count to deploy
	WaitSeconds int // Seconds to wait before next step
}

type AutoScalingConfig struct {
	MinCapacity int
	MaxCapacity int
	Policies    []ScalingPolicy
}

type ScalingPolicy struct {
	Name             string
	Type             string // TargetTrackingScaling, StepScaling
	TargetValue      float64
	PredefinedMetric string
	CustomMetricSpec *CustomMetricSpec
	ScaleInCooldown  int
	ScaleOutCooldown int
}

type CustomMetricSpec struct {
	Namespace  string
	MetricName string
	Dimensions []MetricDimension
	Statistic  string
}

type MetricDimension struct {
	Name  string
	Value string
}

type Tag struct {
	Key   string
	Value string
}

type DeadLetterConfig struct {
	Arn string
}

type RetryPolicy struct {
	MaximumEventAgeInSeconds int
	MaximumRetryAttempts     int
}

type ScheduledTask struct {
	Name                 string
	TaskDefinition       string
	Cluster              string
	TaskCount            int
	ScheduleType         string // cron, rate
	ScheduleExpression   string
	Timezone             string
	NetworkConfiguration *NetworkConfiguration
	LaunchType           string
	PlatformVersion      string
	Group                string
	Overrides            *TaskOverrides
	Tags                 []Tag
	DeadLetterConfig     *DeadLetterConfig
	RetryPolicy          *RetryPolicy
}

type TaskOverrides struct {
	CPU                string
	Memory             string
	TaskRoleArn        string
	ExecutionRoleArn   string
	ContainerOverrides []ContainerOverride
}

// ParseManifest parses a CUE value into a Manifest struct
func ParseManifest(value cue.Value) (*Manifest, error) {
	log.Debug("parsing manifest from CUE value")

	manifest := &Manifest{
		Tags:            make(map[string]string),
		TaskDefinitions: make(map[string]TaskDefinition),
		Services:        make(map[string]Service),
		ScheduledTasks:  make(map[string]ScheduledTask),
	}

	if name, err := ExtractString(value, "name"); err == nil {
		manifest.Name = name
	}

	tags := value.LookupPath(cue.ParsePath("tags"))
	if tags.Exists() {
		iter, err := tags.Fields()
		if err == nil {
			for iter.Next() {
				if val, err := iter.Value().String(); err == nil {
					key := iter.Selector().String()
					key = strings.Trim(key, "\"")
					manifest.Tags[key] = val
				}
			}
		}
	}

	secretsVal := value.LookupPath(cue.ParsePath("secrets"))
	if secretsVal.Exists() {
		manifest.Secrets = &SecretsConfig{
			External: make(map[string]string),
		}

		managed := secretsVal.LookupPath(cue.ParsePath("managed"))
		if managed.Exists() {
			manifest.Secrets.Managed = &ManagedSecretsConfig{}
			if file, err := ExtractString(managed, "file"); err == nil {
				manifest.Secrets.Managed.File = file
			}
			if kmsArn, err := ExtractString(managed, "kmsKeyArn"); err == nil {
				manifest.Secrets.Managed.KMSKeyArn = kmsArn
			}
			if kmsRegion, err := ExtractString(managed, "kmsKeyRegion"); err == nil {
				manifest.Secrets.Managed.KMSKeyRegion = kmsRegion
			}
			if ssmKMSKeyID, err := ExtractString(managed, "ssmKmsKeyId"); err == nil {
				manifest.Secrets.Managed.SSMKMSKeyID = ssmKMSKeyID
			}
			if prefix, err := ExtractString(managed, "ssmPrefix"); err == nil {
				manifest.Secrets.Managed.SSMPrefix = prefix
			}
		}

		external := secretsVal.LookupPath(cue.ParsePath("external"))
		if external.Exists() {
			iter, err := external.Fields()
			if err == nil {
				for iter.Next() {
					if val, err := iter.Value().String(); err == nil {
						key := iter.Selector().String()
						key = strings.Trim(key, "\"")
						manifest.Secrets.External[key] = val
					}
				}
			}
		}
	}

	// Parse task definitions
	taskDefs := value.LookupPath(cue.ParsePath("taskDefinitions"))
	if taskDefs.Exists() {
		iter, err := taskDefs.Fields()
		if err != nil {
			return nil, fmt.Errorf("failed to iterate task definitions: %w", err)
		}

		for iter.Next() {
			name := iter.Selector().String()
			td, err := parseTaskDefinition(name, iter.Value())
			if err != nil {
				return nil, fmt.Errorf("failed to parse task definition %s: %w", name, err)
			}
			manifest.TaskDefinitions[name] = td
		}
	}

	// Parse services
	services := value.LookupPath(cue.ParsePath("services"))
	if services.Exists() {
		iter, err := services.Fields()
		if err != nil {
			return nil, fmt.Errorf("failed to iterate services: %w", err)
		}

		for iter.Next() {
			name := iter.Selector().String()
			svc, err := parseService(name, iter.Value())
			if err != nil {
				return nil, fmt.Errorf("failed to parse service %s: %w", name, err)
			}
			manifest.Services[name] = svc
		}
	}

	// Parse scheduled tasks
	scheduled := value.LookupPath(cue.ParsePath("scheduledTasks"))
	if scheduled.Exists() {
		iter, err := scheduled.Fields()
		if err != nil {
			return nil, fmt.Errorf("failed to iterate scheduled tasks: %w", err)
		}

		for iter.Next() {
			name := iter.Selector().String()
			task, err := parseScheduledTask(name, iter.Value())
			if err != nil {
				return nil, fmt.Errorf("failed to parse scheduled task %s: %w", name, err)
			}
			manifest.ScheduledTasks[name] = task
		}
	}

	// Parse ingress
	ingress := value.LookupPath(cue.ParsePath("ingress"))
	if ingress.Exists() {
		ing, err := parseIngress(ingress)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ingress: %w", err)
		}
		manifest.Ingress = ing
	}

	log.Info("parsed manifest",
		"name", manifest.Name,
		"taskDefinitions", len(manifest.TaskDefinitions),
		"services", len(manifest.Services),
		"scheduledTasks", len(manifest.ScheduledTasks),
		"hasIngress", manifest.Ingress != nil)

	return manifest, nil
}

func parseTaskDefinition(name string, v cue.Value) (TaskDefinition, error) {
	td := TaskDefinition{Name: name}

	// Get type
	if t, err := ExtractString(v, "type"); err == nil {
		td.Type = t
	} else {
		return td, fmt.Errorf("task definition type is required")
	}

	switch td.Type {
	case "managed":
		if family, err := ExtractString(v, "family"); err == nil {
			td.Family = family
		}
		if cpu, err := ExtractString(v, "cpu"); err == nil {
			td.CPU = cpu
		}
		if memory, err := ExtractString(v, "memory"); err == nil {
			td.Memory = memory
		}
		if networkMode, err := ExtractString(v, "networkMode"); err == nil {
			td.NetworkMode = networkMode
		}
		if roleArn, err := ExtractString(v, "executionRoleArn"); err == nil {
			td.ExecutionRoleArn = roleArn
		}
		if roleArn, err := ExtractString(v, "taskRoleArn"); err == nil {
			td.TaskRoleArn = roleArn
		}
		if compat, err := ExtractStringSlice(v, "requiresCompatibilities"); err == nil {
			td.RequiresCompatibilities = compat
		}

		// Parse container definitions
		containers := v.LookupPath(cue.ParsePath("containerDefinitions"))
		if containers.Exists() {
			iter, err := containers.List()
			if err != nil {
				return td, fmt.Errorf("failed to list container definitions: %w", err)
			}
			for iter.Next() {
				cd, err := parseContainerDefinition(iter.Value())
				if err != nil {
					return td, fmt.Errorf("failed to parse container definition: %w", err)
				}
				td.ContainerDefinitions = append(td.ContainerDefinitions, cd)
			}
		}

	case "merged":
		if baseArn, err := ExtractString(v, "baseArn"); err == nil {
			td.BaseArn = baseArn
		}
		// Parse overrides
		overrides := v.LookupPath(cue.ParsePath("overrides"))
		if overrides.Exists() {
			td.Overrides = &TaskDefOverrides{}
			if cpu, err := ExtractString(overrides, "cpu"); err == nil {
				td.Overrides.CPU = cpu
			}
			if memory, err := ExtractString(overrides, "memory"); err == nil {
				td.Overrides.Memory = memory
			}
		}

	case "remote":
		if arn, err := ExtractString(v, "arn"); err == nil {
			td.Arn = arn
		}
	}

	return td, nil
}

func parseContainerDefinition(v cue.Value) (ContainerDefinition, error) {
	cd := ContainerDefinition{Essential: true}

	if name, err := ExtractString(v, "name"); err == nil {
		cd.Name = name
	}
	if image, err := ExtractString(v, "image"); err == nil {
		cd.Image = image
	}
	if cpu, err := ExtractInt(v, "cpu"); err == nil {
		cd.CPU = int(cpu)
	}
	if memory, err := ExtractInt(v, "memory"); err == nil {
		cd.Memory = int(memory)
	}
	if essential, err := ExtractBool(v, "essential"); err == nil {
		cd.Essential = essential
	}
	if wd, err := ExtractString(v, "workingDirectory"); err == nil {
		cd.WorkingDirectory = wd
	}
	if cmd, err := ExtractStringSlice(v, "command"); err == nil {
		cd.Command = cmd
	}
	if ep, err := ExtractStringSlice(v, "entryPoint"); err == nil {
		cd.EntryPoint = ep
	}

	env := v.LookupPath(cue.ParsePath("environment"))
	if env.Exists() {
		iter, err := env.Fields()
		if err == nil {
			for iter.Next() {
				if val, err := iter.Value().String(); err == nil {
					key := iter.Selector().String()
					key = strings.Trim(key, "\"")
					cd.Environment = append(cd.Environment, KeyValuePair{Name: key, Value: val})
				}
			}
		}
	}

	secrets := v.LookupPath(cue.ParsePath("secrets"))
	if secrets.Exists() {
		iter, err := secrets.Fields()
		if err == nil {
			for iter.Next() {
				if val, err := iter.Value().String(); err == nil {
					key := iter.Selector().String()
					key = strings.Trim(key, "\"")
					cd.Secrets = append(cd.Secrets, Secret{Name: key, ValueFrom: val})
				}
			}
		}
	}

	// Parse port mappings
	ports := v.LookupPath(cue.ParsePath("portMappings"))
	if ports.Exists() {
		iter, err := ports.List()
		if err == nil {
			for iter.Next() {
				pm := PortMapping{}
				if cp, err := ExtractInt(iter.Value(), "containerPort"); err == nil {
					pm.ContainerPort = int(cp)
				}
				if hp, err := ExtractInt(iter.Value(), "hostPort"); err == nil {
					pm.HostPort = int(hp)
				}
				if proto, err := ExtractString(iter.Value(), "protocol"); err == nil {
					pm.Protocol = proto
				}
				if name, err := ExtractString(iter.Value(), "name"); err == nil {
					pm.Name = name
				}
				if appProtocol, err := ExtractString(iter.Value(), "appProtocol"); err == nil {
					pm.AppProtocol = appProtocol
				}
				cd.PortMappings = append(cd.PortMappings, pm)
			}
		}
	}

	// Parse mount points
	mountPoints := v.LookupPath(cue.ParsePath("mountPoints"))
	if mountPoints.Exists() {
		iter, err := mountPoints.List()
		if err == nil {
			for iter.Next() {
				mp := MountPoint{}
				if sourceVolume, err := ExtractString(iter.Value(), "sourceVolume"); err == nil {
					mp.SourceVolume = sourceVolume
				}
				if containerPath, err := ExtractString(iter.Value(), "containerPath"); err == nil {
					mp.ContainerPath = containerPath
				}
				if readOnly, err := ExtractBool(iter.Value(), "readOnly"); err == nil {
					mp.ReadOnly = readOnly
				}
				cd.MountPoints = append(cd.MountPoints, mp)
			}
		}
	}

	// Parse container health check
	healthCheck := v.LookupPath(cue.ParsePath("healthCheck"))
	if healthCheck.Exists() {
		cd.HealthCheck = &HealthCheck{}
		if command, err := ExtractStringSlice(healthCheck, "command"); err == nil {
			cd.HealthCheck.Command = command
		}
		if interval, err := ExtractInt(healthCheck, "interval"); err == nil {
			cd.HealthCheck.Interval = int(interval)
		}
		if timeout, err := ExtractInt(healthCheck, "timeout"); err == nil {
			cd.HealthCheck.Timeout = int(timeout)
		}
		if retries, err := ExtractInt(healthCheck, "retries"); err == nil {
			cd.HealthCheck.Retries = int(retries)
		}
		if startPeriod, err := ExtractInt(healthCheck, "startPeriod"); err == nil {
			cd.HealthCheck.StartPeriod = int(startPeriod)
		}
	}

	// Parse startup dependencies
	dependencies := v.LookupPath(cue.ParsePath("dependsOn"))
	if dependencies.Exists() {
		iter, err := dependencies.List()
		if err == nil {
			for iter.Next() {
				dependency := ContainerDependency{}
				if containerName, err := ExtractString(iter.Value(), "containerName"); err == nil {
					dependency.ContainerName = containerName
				}
				if condition, err := ExtractString(iter.Value(), "condition"); err == nil {
					dependency.Condition = condition
				}
				cd.DependsOn = append(cd.DependsOn, dependency)
			}
		}
	}

	// Parse Linux parameters and capabilities
	linuxParameters := v.LookupPath(cue.ParsePath("linuxParameters"))
	if linuxParameters.Exists() {
		cd.LinuxParameters = &LinuxParameters{}
		if initProcessEnabled, err := ExtractBool(linuxParameters, "initProcessEnabled"); err == nil {
			cd.LinuxParameters.InitProcessEnabled = initProcessEnabled
		}
		capabilities := linuxParameters.LookupPath(cue.ParsePath("capabilities"))
		if capabilities.Exists() {
			cd.LinuxParameters.Capabilities = &KernelCapabilities{}
			if add, err := ExtractStringSlice(capabilities, "add"); err == nil {
				cd.LinuxParameters.Capabilities.Add = add
			}
			if drop, err := ExtractStringSlice(capabilities, "drop"); err == nil {
				cd.LinuxParameters.Capabilities.Drop = drop
			}
		}
	}

	// Parse ulimits
	ulimits := v.LookupPath(cue.ParsePath("ulimits"))
	if ulimits.Exists() {
		iter, err := ulimits.List()
		if err == nil {
			for iter.Next() {
				ulimit := Ulimit{}
				if name, err := ExtractString(iter.Value(), "name"); err == nil {
					ulimit.Name = name
				}
				if softLimit, err := ExtractInt(iter.Value(), "softLimit"); err == nil {
					ulimit.SoftLimit = int(softLimit)
				}
				if hardLimit, err := ExtractInt(iter.Value(), "hardLimit"); err == nil {
					ulimit.HardLimit = int(hardLimit)
				}
				cd.Ulimits = append(cd.Ulimits, ulimit)
			}
		}
	}

	// Parse log configuration
	logConfig := v.LookupPath(cue.ParsePath("logConfiguration"))
	if logConfig.Exists() {
		cd.LogConfiguration = &LogConfiguration{
			Options:      make(map[string]string),
			LogGroupTags: make(map[string]string),
		}
		if driver, err := ExtractString(logConfig, "logDriver"); err == nil {
			cd.LogConfiguration.LogDriver = driver
		}
		opts := logConfig.LookupPath(cue.ParsePath("options"))
		if opts.Exists() {
			iter, err := opts.Fields()
			if err == nil {
				for iter.Next() {
					if val, err := iter.Value().String(); err == nil {
						key := iter.Selector().String()
						key = strings.Trim(key, "\"")
						cd.LogConfiguration.Options[key] = val
					}
				}
			}
		}
		// Log group management fields
		if create, err := ExtractBool(logConfig, "createLogGroup"); err == nil {
			cd.LogConfiguration.CreateLogGroup = create
		}
		if retention, err := ExtractInt(logConfig, "retentionInDays"); err == nil {
			cd.LogConfiguration.RetentionInDays = int(retention)
		}
		if kmsKey, err := ExtractString(logConfig, "kmsKeyId"); err == nil {
			cd.LogConfiguration.KMSKeyID = kmsKey
		}
		secretOpts := logConfig.LookupPath(cue.ParsePath("secretOptions"))
		if secretOpts.Exists() {
			iter, err := secretOpts.Fields()
			if err == nil {
				for iter.Next() {
					if val, err := iter.Value().String(); err == nil {
						key := iter.Selector().String()
						key = strings.Trim(key, "\"")
						cd.LogConfiguration.SecretOptions = append(cd.LogConfiguration.SecretOptions, Secret{Name: key, ValueFrom: val})
					}
				}
			}
		}
		logTags := logConfig.LookupPath(cue.ParsePath("logGroupTags"))
		if logTags.Exists() {
			iter, err := logTags.Fields()
			if err == nil {
				for iter.Next() {
					if val, err := iter.Value().String(); err == nil {
						key := iter.Selector().String()
						key = strings.Trim(key, "\"")
						cd.LogConfiguration.LogGroupTags[key] = val
					}
				}
			}
		}
		subscriptionFilters := logConfig.LookupPath(cue.ParsePath("subscriptionFilters"))
		if subscriptionFilters.Exists() {
			iter, err := subscriptionFilters.List()
			if err != nil {
				return cd, fmt.Errorf("failed to list subscription filters: %w", err)
			}

			i := 0
			for iter.Next() {
				filter := SubscriptionFilter{}
				filterValue := iter.Value()

				name, err := ExtractString(filterValue, "name")
				if err != nil {
					return cd, fmt.Errorf("subscriptionFilters[%d].name is required: %w", i, err)
				}
				filter.Name = name

				destinationArn, err := ExtractString(filterValue, "destinationArn")
				if err != nil {
					return cd, fmt.Errorf("subscriptionFilters[%d].destinationArn is required: %w", i, err)
				}
				filter.DestinationArn = destinationArn

				if pattern, err := ExtractString(filterValue, "filterPattern"); err == nil {
					filter.FilterPattern = pattern
				}
				if roleArn, err := ExtractString(filterValue, "roleArn"); err == nil {
					filter.RoleArn = roleArn
				}
				if distribution, err := ExtractString(filterValue, "distribution"); err == nil {
					filter.Distribution = distribution
				}

				cd.LogConfiguration.SubscriptionFilters = append(cd.LogConfiguration.SubscriptionFilters, filter)
				i++
			}
		}
	}

	return cd, nil
}

func parseService(name string, v cue.Value) (Service, error) {
	svc := Service{Name: name}

	if cluster, err := ExtractString(v, "cluster"); err == nil {
		svc.Cluster = cluster
	}
	if td, err := ExtractString(v, "taskDefinition"); err == nil {
		svc.TaskDefinition = td
	}
	if dc, err := ExtractInt(v, "desiredCount"); err == nil {
		svc.DesiredCount = int(dc)
	}
	if lt, err := ExtractString(v, "launchType"); err == nil {
		svc.LaunchType = lt
	}
	if exec, err := ExtractBool(v, "enableExecuteCommand"); err == nil {
		svc.EnableExecuteCommand = exec
	}
	if grace, err := ExtractInt(v, "healthCheckGracePeriodSeconds"); err == nil {
		svc.HealthCheckGracePeriodSeconds = int(grace)
		svc.HealthCheckGracePeriodSecondsSet = true
	}

	// Parse capacity provider strategy
	cpStrategy := v.LookupPath(cue.ParsePath("capacityProviderStrategy"))
	if cpStrategy.Exists() {
		iter, err := cpStrategy.List()
		if err == nil {
			for iter.Next() {
				item := CapacityProviderStrategyItem{}
				if cp, err := ExtractString(iter.Value(), "capacityProvider"); err == nil {
					item.CapacityProvider = cp
				}
				if weight, err := ExtractInt(iter.Value(), "weight"); err == nil {
					item.Weight = int(weight)
				}
				if base, err := ExtractInt(iter.Value(), "base"); err == nil {
					item.Base = int(base)
				}
				svc.CapacityProviderStrategy = append(svc.CapacityProviderStrategy, item)
			}
		}
	}

	if deps, err := ExtractStringSlice(v, "dependsOn"); err == nil {
		svc.DependsOn = deps
	}

	// Parse network configuration
	netConfig := v.LookupPath(cue.ParsePath("networkConfiguration.awsvpcConfiguration"))
	if netConfig.Exists() {
		svc.NetworkConfiguration = &NetworkConfiguration{}
		if subnets, err := ExtractStringSlice(netConfig, "subnets"); err == nil {
			svc.NetworkConfiguration.Subnets = subnets
		}
		if sgs, err := ExtractStringSlice(netConfig, "securityGroups"); err == nil {
			svc.NetworkConfiguration.SecurityGroups = sgs
		}
		if pip, err := ExtractString(netConfig, "assignPublicIp"); err == nil {
			svc.NetworkConfiguration.AssignPublicIp = pip
		}
	}

	// Parse load balancers
	loadBalancers := v.LookupPath(cue.ParsePath("loadBalancers"))
	if loadBalancers.Exists() {
		iter, err := loadBalancers.List()
		if err == nil {
			for iter.Next() {
				lb := LoadBalancer{}
				if arn, err := ExtractString(iter.Value(), "targetGroupArn"); err == nil {
					lb.TargetGroupArn = arn
				}
				if name, err := ExtractString(iter.Value(), "containerName"); err == nil {
					lb.ContainerName = name
				}
				if port, err := ExtractInt(iter.Value(), "containerPort"); err == nil {
					lb.ContainerPort = int(port)
				}
				svc.LoadBalancers = append(svc.LoadBalancers, lb)
			}
		}
	}

	// Parse service registries
	serviceRegistries := v.LookupPath(cue.ParsePath("serviceRegistries"))
	if serviceRegistries.Exists() {
		iter, err := serviceRegistries.List()
		if err == nil {
			for iter.Next() {
				reg := ServiceRegistry{}
				sd := iter.Value().LookupPath(cue.ParsePath("serviceDiscovery"))
				if sd.Exists() {
					reg.ServiceDiscovery = &ServiceDiscoveryConfig{
						DNSRecordType: "A",
						DNSTTL:        60,
						RoutingPolicy: "MULTIVALUE",
					}
					if ns, err := ExtractString(sd, "namespaceArn"); err == nil {
						reg.ServiceDiscovery.NamespaceArn = ns
					}
					if nsID, err := ExtractString(sd, "namespaceId"); err == nil {
						reg.ServiceDiscovery.NamespaceID = nsID
					}
					if name, err := ExtractString(sd, "name"); err == nil {
						reg.ServiceDiscovery.Name = name
					}
					if dnsType, err := ExtractString(sd, "dnsRecordType"); err == nil {
						reg.ServiceDiscovery.DNSRecordType = dnsType
					}
					if ttl, err := ExtractInt(sd, "dnsTTL"); err == nil {
						reg.ServiceDiscovery.DNSTTL = int(ttl)
					}
					if policy, err := ExtractString(sd, "routingPolicy"); err == nil {
						reg.ServiceDiscovery.RoutingPolicy = policy
					}
					tags := sd.LookupPath(cue.ParsePath("tags"))
					if tags.Exists() {
						iter, err := tags.Fields()
						if err == nil {
							reg.ServiceDiscovery.Tags = make(map[string]string)
							for iter.Next() {
								if val, err := iter.Value().String(); err == nil {
									key := iter.Selector().String()
									key = strings.Trim(key, "\"")
									reg.ServiceDiscovery.Tags[key] = val
								}
							}
						}
					}
				} else if arn, err := ExtractString(iter.Value(), "registryArn"); err == nil {
					reg.RegistryArn = arn
				}
				if name, err := ExtractString(iter.Value(), "containerName"); err == nil {
					reg.ContainerName = name
				}
				if port, err := ExtractInt(iter.Value(), "containerPort"); err == nil {
					reg.ContainerPort = int(port)
				}
				if port, err := ExtractInt(iter.Value(), "port"); err == nil {
					reg.Port = int(port)
				}
				svc.ServiceRegistries = append(svc.ServiceRegistries, reg)
			}
		}
	}

	// Parse deployment configuration
	deployment := v.LookupPath(cue.ParsePath("deployment"))
	if deployment.Exists() {
		if strategy, err := ExtractString(deployment, "strategy"); err == nil {
			svc.Deployment.Strategy = strategy
		}

		config := deployment.LookupPath(cue.ParsePath("config"))
		if config.Exists() {
			// Common deployment config
			if mhp, err := ExtractInt(config, "minimumHealthyPercent"); err == nil {
				svc.Deployment.MinimumHealthyPercent = int(mhp)
				svc.Deployment.MinimumHealthyPercentSet = true
			}
			if mp, err := ExtractInt(config, "maximumPercent"); err == nil {
				svc.Deployment.MaximumPercent = int(mp)
				svc.Deployment.MaximumPercentSet = true
			}

			// Circuit breaker
			cb := config.LookupPath(cue.ParsePath("circuitBreaker"))
			if cb.Exists() {
				if enable, err := ExtractBool(cb, "enable"); err == nil {
					svc.Deployment.CircuitBreakerEnable = enable
				}
				if rollback, err := ExtractBool(cb, "rollback"); err == nil {
					svc.Deployment.CircuitBreakerRollback = rollback
				}
			}

			// Deployment alarms
			if alarms, err := ExtractStringSlice(config, "alarms"); err == nil {
				svc.Deployment.Alarms = alarms
			}
			if alarmRollback, err := ExtractBool(config, "alarmRollback"); err == nil {
				svc.Deployment.AlarmRollbackEnable = alarmRollback
			}

			// Gradual deployment steps
			steps := config.LookupPath(cue.ParsePath("steps"))
			if steps.Exists() {
				iter, err := steps.List()
				if err == nil {
					for iter.Next() {
						step := GradualStep{}
						if pct, err := ExtractInt(iter.Value(), "percent"); err == nil {
							step.Percent = int(pct)
						}
						if wait, err := ExtractInt(iter.Value(), "wait"); err == nil {
							step.WaitSeconds = int(wait)
						}
						svc.Deployment.GradualSteps = append(svc.Deployment.GradualSteps, step)
					}
				}
			}
		}
	}

	// Parse hooks
	hooks := v.LookupPath(cue.ParsePath("hooks"))
	if hooks.Exists() {
		h, err := parseHooks(hooks)
		if err != nil {
			return svc, fmt.Errorf("failed to parse hooks: %w", err)
		}
		svc.Hooks = h
	}

	return svc, nil
}

func parseHooks(v cue.Value) (*Hooks, error) {
	hooks := &Hooks{}

	preHook := v.LookupPath(cue.ParsePath("preHook"))
	if preHook.Exists() {
		h, err := parseHook(preHook)
		if err != nil {
			return nil, fmt.Errorf("preHook: %w", err)
		}
		hooks.PreHook = h
	}

	postHook := v.LookupPath(cue.ParsePath("postHook"))
	if postHook.Exists() {
		h, err := parseHook(postHook)
		if err != nil {
			return nil, fmt.Errorf("postHook: %w", err)
		}
		hooks.PostHook = h
	}

	return hooks, nil
}

func parseHook(v cue.Value) (*Hook, error) {
	hook := &Hook{Timeout: 600} // default timeout

	if td, err := ExtractString(v, "taskDefinition"); err == nil {
		hook.TaskDefinition = td
	} else {
		return nil, fmt.Errorf("taskDefinition is required")
	}

	if timeout, err := ExtractInt(v, "timeout"); err == nil {
		hook.Timeout = int(timeout)
	}

	// Parse container overrides
	overrides := v.LookupPath(cue.ParsePath("containerOverrides"))
	if overrides.Exists() {
		iter, err := overrides.List()
		if err != nil {
			return nil, fmt.Errorf("failed to iterate containerOverrides: %w", err)
		}
		for iter.Next() {
			co := HookContainerOverride{}
			if name, err := ExtractString(iter.Value(), "name"); err == nil {
				co.Name = name
			}
			if command, err := ExtractStringSlice(iter.Value(), "command"); err == nil {
				co.Command = command
			}

			env := iter.Value().LookupPath(cue.ParsePath("environment"))
			if env.Exists() {
				envIter, err := env.Fields()
				if err == nil {
					for envIter.Next() {
						if val, err := envIter.Value().String(); err == nil {
							key := envIter.Selector().String()
							key = strings.Trim(key, "\"")
							co.Environment = append(co.Environment, KeyValuePair{Name: key, Value: val})
						}
					}
				}
			}

			hook.ContainerOverrides = append(hook.ContainerOverrides, co)
		}
	}

	return hook, nil
}

func parseScheduledTask(name string, v cue.Value) (ScheduledTask, error) {
	task := ScheduledTask{Name: name, TaskCount: 1}

	if td, err := ExtractString(v, "taskDefinition"); err == nil {
		task.TaskDefinition = td
	}
	if cluster, err := ExtractString(v, "cluster"); err == nil {
		task.Cluster = cluster
	}
	if tc, err := ExtractInt(v, "taskCount"); err == nil {
		task.TaskCount = int(tc)
	}
	if lt, err := ExtractString(v, "launchType"); err == nil {
		task.LaunchType = lt
	}
	if pv, err := ExtractString(v, "platformVersion"); err == nil {
		task.PlatformVersion = pv
	}
	if group, err := ExtractString(v, "group"); err == nil {
		task.Group = group
	}

	// Parse schedule
	schedule := v.LookupPath(cue.ParsePath("schedule"))
	if schedule.Exists() {
		if st, err := ExtractString(schedule, "type"); err == nil {
			task.ScheduleType = st
		}
		if expr, err := ExtractString(schedule, "expression"); err == nil {
			task.ScheduleExpression = expr
		}
		if tz, err := ExtractString(schedule, "timezone"); err == nil {
			task.Timezone = tz
		}
	}

	// Parse overrides
	overrides := v.LookupPath(cue.ParsePath("overrides"))
	if overrides.Exists() {
		task.Overrides = &TaskOverrides{}
		if cpu, err := ExtractString(overrides, "cpu"); err == nil {
			task.Overrides.CPU = cpu
		}
		if memory, err := ExtractString(overrides, "memory"); err == nil {
			task.Overrides.Memory = memory
		}
		if roleArn, err := ExtractString(overrides, "taskRoleArn"); err == nil {
			task.Overrides.TaskRoleArn = roleArn
		}
		if roleArn, err := ExtractString(overrides, "executionRoleArn"); err == nil {
			task.Overrides.ExecutionRoleArn = roleArn
		}

		containerOverrides := overrides.LookupPath(cue.ParsePath("containerOverrides"))
		if containerOverrides.Exists() {
			iter, err := containerOverrides.List()
			if err == nil {
				for iter.Next() {
					co := ContainerOverride{}
					if name, err := ExtractString(iter.Value(), "name"); err == nil {
						co.Name = name
					}
					if command, err := ExtractStringSlice(iter.Value(), "command"); err == nil {
						co.Command = command
					}
					if cpu, err := ExtractInt(iter.Value(), "cpu"); err == nil {
						co.CPU = int(cpu)
					}
					if memory, err := ExtractInt(iter.Value(), "memory"); err == nil {
						co.Memory = int(memory)
					}
					env := iter.Value().LookupPath(cue.ParsePath("environment"))
					if env.Exists() {
						envIter, err := env.Fields()
						if err == nil {
							for envIter.Next() {
								if val, err := envIter.Value().String(); err == nil {
									key := envIter.Selector().String()
									key = strings.Trim(key, "\"")
									co.Environment = append(co.Environment, KeyValuePair{Name: key, Value: val})
								}
							}
						}
					}
					task.Overrides.ContainerOverrides = append(task.Overrides.ContainerOverrides, co)
				}
			}
		}
	}

	// Parse network configuration
	netConfig := v.LookupPath(cue.ParsePath("networkConfiguration.awsvpcConfiguration"))
	if netConfig.Exists() {
		task.NetworkConfiguration = &NetworkConfiguration{}
		if subnets, err := ExtractStringSlice(netConfig, "subnets"); err == nil {
			task.NetworkConfiguration.Subnets = subnets
		}
		if sgs, err := ExtractStringSlice(netConfig, "securityGroups"); err == nil {
			task.NetworkConfiguration.SecurityGroups = sgs
		}
		if pip, err := ExtractString(netConfig, "assignPublicIp"); err == nil {
			task.NetworkConfiguration.AssignPublicIp = pip
		}
	}

	// Parse tags
	tags := v.LookupPath(cue.ParsePath("tags"))
	if tags.Exists() {
		iter, err := tags.List()
		if err == nil {
			for iter.Next() {
				tag := Tag{}
				if key, err := ExtractString(iter.Value(), "key"); err == nil {
					tag.Key = key
				}
				if value, err := ExtractString(iter.Value(), "value"); err == nil {
					tag.Value = value
				}
				task.Tags = append(task.Tags, tag)
			}
		}
	}

	// Parse dead letter config
	deadLetter := v.LookupPath(cue.ParsePath("deadLetterConfig"))
	if deadLetter.Exists() {
		dl := &DeadLetterConfig{}
		if arn, err := ExtractString(deadLetter, "arn"); err == nil {
			dl.Arn = arn
		}
		task.DeadLetterConfig = dl
	}

	// Parse retry policy
	retryPolicy := v.LookupPath(cue.ParsePath("retryPolicy"))
	if retryPolicy.Exists() {
		rp := &RetryPolicy{}
		if age, err := ExtractInt(retryPolicy, "maximumEventAgeInSeconds"); err == nil {
			rp.MaximumEventAgeInSeconds = int(age)
		}
		if attempts, err := ExtractInt(retryPolicy, "maximumRetryAttempts"); err == nil {
			rp.MaximumRetryAttempts = int(attempts)
		}
		task.RetryPolicy = rp
	}

	return task, nil
}

func parseIngress(v cue.Value) (*Ingress, error) {
	ing := &Ingress{}

	if arn, err := ExtractString(v, "listenerArn"); err == nil {
		ing.ListenerArn = arn
	}
	if vpcID, err := ExtractString(v, "vpcId"); err == nil {
		ing.VpcID = vpcID
	}

	// Parse rules
	rules := v.LookupPath(cue.ParsePath("rules"))
	if rules.Exists() {
		iter, err := rules.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list rules: %w", err)
		}

		for iter.Next() {
			rule, err := parseIngressRule(iter.Value())
			if err != nil {
				return nil, fmt.Errorf("failed to parse ingress rule: %w", err)
			}
			ing.Rules = append(ing.Rules, rule)
		}
	}

	return ing, nil
}

// ResolveManagedSecrets resolves managed secret references in container definitions.
// It replaces ARN values matching the managed prefix with actual SSM parameter ARNs.
func (m *Manifest) ResolveManagedSecrets(managedSecrets *ManagedSecrets) {
	if managedSecrets == nil || len(managedSecrets.Decrypted) == 0 {
		return
	}

	arnMap := managedSecrets.BuildARNMap()

	for tdName, td := range m.TaskDefinitions {
		for i, cd := range td.ContainerDefinitions {
			for j, secret := range cd.Secrets {
				// Check if this is a managed secret reference (just the key name, no ARN)
				if arn, ok := arnMap[secret.ValueFrom]; ok {
					m.TaskDefinitions[tdName].ContainerDefinitions[i].Secrets[j].ValueFrom = arn
				}
			}
		}
	}
}

// ValidateSecretReferences checks that all container secret ValueFrom values are
// valid ARNs. Call this after ResolveManagedSecrets to catch unresolved or
// misconfigured secret references before they reach ECS (where bare strings
// are silently interpreted as SSM parameter names, usually causing AccessDenied).
func (m *Manifest) ValidateSecretReferences() []string {
	var errors []string

	for tdName, td := range m.TaskDefinitions {
		for _, cd := range td.ContainerDefinitions {
			for _, secret := range cd.Secrets {
				if !isValidSecretArn(secret.ValueFrom) {
					errors = append(errors, fmt.Sprintf(
						"task definition '%s', container '%s': secret '%s' has invalid valueFrom %q — "+
							"expected an ARN (arn:aws:ssm:... or arn:aws:secretsmanager:...) or a managed secret key; "+
							"bare values are passed to ECS as SSM parameter names which will likely fail with AccessDenied",
						tdName, cd.Name, secret.Name, secret.ValueFrom,
					))
				}
			}
		}
	}

	return errors
}

// ValidateSecretReferencesOffline checks secret references without managed secrets
// resolution. If managed secrets are configured, bare key names are allowed
// (they'll be resolved at diff/apply time). Without managed secrets, all values
// must be ARNs.
func (m *Manifest) ValidateSecretReferencesOffline() []string {
	hasManagedSecrets := m.Secrets != nil && m.Secrets.Managed != nil
	var errors []string

	for tdName, td := range m.TaskDefinitions {
		for _, cd := range td.ContainerDefinitions {
			for _, secret := range cd.Secrets {
				if isValidSecretArn(secret.ValueFrom) {
					continue
				}

				// With managed secrets, bare key names (no slashes, no colons) are
				// assumed to be managed secret keys that will be resolved later.
				if hasManagedSecrets && isBareKeyName(secret.ValueFrom) {
					continue
				}

				if hasManagedSecrets {
					errors = append(errors, fmt.Sprintf(
						"task definition '%s', container '%s': secret '%s' has suspicious valueFrom %q — "+
							"not an ARN and doesn't look like a managed secret key name (contains '/' or ':')",
						tdName, cd.Name, secret.Name, secret.ValueFrom,
					))
				} else {
					errors = append(errors, fmt.Sprintf(
						"task definition '%s', container '%s': secret '%s' has invalid valueFrom %q — "+
							"expected an ARN (arn:aws:ssm:... or arn:aws:secretsmanager:...); "+
							"configure secrets.managed to use key-based references",
						tdName, cd.Name, secret.Name, secret.ValueFrom,
					))
				}
			}
		}
	}

	return errors
}

// isValidSecretArn checks if a value is a valid AWS ARN for SSM or Secrets Manager.
func isValidSecretArn(value string) bool {
	return strings.HasPrefix(value, "arn:aws:ssm:") ||
		strings.HasPrefix(value, "arn:aws:secretsmanager:")
}

// isBareKeyName checks if a value looks like a simple secret key name
// (alphanumeric, underscores, hyphens, dots — no slashes or colons).
func isBareKeyName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r == '/' || r == ':' {
			return false
		}
	}
	return true
}

func parseIngressRule(v cue.Value) (IngressRule, error) {
	rule := IngressRule{
		Tags: make(map[string]string),
	}

	if priority, err := ExtractInt(v, "priority"); err == nil {
		rule.Priority = int(priority)
	}

	// Parse match conditions
	if host, err := ExtractString(v, "host"); err == nil {
		rule.Host = host
	}
	if hosts, err := ExtractStringSlice(v, "hosts"); err == nil {
		rule.Hosts = hosts
	}
	if paths, err := ExtractStringSlice(v, "paths"); err == nil {
		rule.Paths = paths
	}

	// Parse service backend
	svc := v.LookupPath(cue.ParsePath("service"))
	if svc.Exists() {
		rule.Service = &IngressServiceBackend{}
		if name, err := ExtractString(svc, "name"); err == nil {
			rule.Service.Name = name
		}
		if containerName, err := ExtractString(svc, "containerName"); err == nil {
			rule.Service.ContainerName = containerName
		}
		if containerPort, err := ExtractInt(svc, "containerPort"); err == nil {
			rule.Service.ContainerPort = int(containerPort)
		}
	}

	// Parse redirect backend
	redirect := v.LookupPath(cue.ParsePath("redirect"))
	if redirect.Exists() {
		rule.Redirect = &IngressRedirect{
			StatusCode: "HTTP_301",
		}
		if sc, err := ExtractString(redirect, "statusCode"); err == nil {
			rule.Redirect.StatusCode = sc
		}
		if protocol, err := ExtractString(redirect, "protocol"); err == nil {
			rule.Redirect.Protocol = protocol
		}
		if host, err := ExtractString(redirect, "host"); err == nil {
			rule.Redirect.Host = host
		}
		if port, err := ExtractString(redirect, "port"); err == nil {
			rule.Redirect.Port = port
		}
		if path, err := ExtractString(redirect, "path"); err == nil {
			rule.Redirect.Path = path
		}
		if query, err := ExtractString(redirect, "query"); err == nil {
			rule.Redirect.Query = query
		}
	}

	// Parse fixed-response backend
	fixedResp := v.LookupPath(cue.ParsePath("fixedResponse"))
	if fixedResp.Exists() {
		rule.FixedResponse = &IngressFixedResponse{}
		if sc, err := ExtractString(fixedResp, "statusCode"); err == nil {
			rule.FixedResponse.StatusCode = sc
		}
		if ct, err := ExtractString(fixedResp, "contentType"); err == nil {
			rule.FixedResponse.ContentType = ct
		}
		if mb, err := ExtractString(fixedResp, "messageBody"); err == nil {
			rule.FixedResponse.MessageBody = mb
		}
	}

	// Parse health check (for service backends)
	hc := v.LookupPath(cue.ParsePath("healthCheck"))
	if hc.Exists() {
		rule.HealthCheck = &TargetGroupHealthCheck{
			Path:               "/",
			Protocol:           "HTTP",
			Port:               "traffic-port",
			HealthyThreshold:   5,
			UnhealthyThreshold: 2,
			Timeout:            5,
			Interval:           30,
			Matcher:            "200",
		}
		if path, err := ExtractString(hc, "path"); err == nil {
			rule.HealthCheck.Path = path
		}
		if protocol, err := ExtractString(hc, "protocol"); err == nil {
			rule.HealthCheck.Protocol = protocol
		}
		if port, err := ExtractString(hc, "port"); err == nil {
			rule.HealthCheck.Port = port
		}
		if ht, err := ExtractInt(hc, "healthyThreshold"); err == nil {
			rule.HealthCheck.HealthyThreshold = int(ht)
		}
		if ut, err := ExtractInt(hc, "unhealthyThreshold"); err == nil {
			rule.HealthCheck.UnhealthyThreshold = int(ut)
		}
		if timeout, err := ExtractInt(hc, "timeout"); err == nil {
			rule.HealthCheck.Timeout = int(timeout)
		}
		if interval, err := ExtractInt(hc, "interval"); err == nil {
			rule.HealthCheck.Interval = int(interval)
		}
		if matcher, err := ExtractString(hc, "matcher"); err == nil {
			rule.HealthCheck.Matcher = matcher
		}
	}

	if deregDelay, err := ExtractInt(v, "deregistrationDelay"); err == nil {
		rule.DeregistrationDelay = int(deregDelay)
	}

	// Parse tags
	tags := v.LookupPath(cue.ParsePath("tags"))
	if tags.Exists() {
		iter, err := tags.Fields()
		if err == nil {
			for iter.Next() {
				if val, err := iter.Value().String(); err == nil {
					key := iter.Selector().String()
					key = strings.Trim(key, "\"")
					rule.Tags[key] = val
				}
			}
		}
	}

	return rule, nil
}
