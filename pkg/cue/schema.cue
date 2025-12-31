package schema

#Manifest: {
	name: string

	taskDefinitions?: [string]: #TaskDefinition
	services?:        [string]: #Service
	scheduledTasks?:  [string]: #ScheduledTask
	ingress?:         #Ingress
}

#TaskDefinition: #ManagedTaskDef | #MergedTaskDef | #RemoteTaskDef

#ManagedTaskDef: {
	type:   "managed"
	family: string

	cpu?:                   string
	memory?:                string
	networkMode?:           "awsvpc" | "bridge" | "host" | "none"
	requiresCompatibilities?: [...("EC2" | "FARGATE" | "EXTERNAL")]
	executionRoleArn?:      string
	taskRoleArn?:           string
	runtimePlatform?: {
		cpuArchitecture?:       "X86_64" | "ARM64"
		operatingSystemFamily?: "LINUX" | "WINDOWS_SERVER_2019_FULL" | "WINDOWS_SERVER_2019_CORE" | "WINDOWS_SERVER_2022_FULL" | "WINDOWS_SERVER_2022_CORE"
	}
	volumes?: [...#Volume]
	containerDefinitions: [...#ContainerDefinition]
}

#MergedTaskDef: {
	type:    "merged"
	baseArn: string
	overrides: {
		cpu?:                   string
		memory?:                string
		executionRoleArn?:      string
		taskRoleArn?:           string
		containerDefinitions?: [...#ContainerOverride]
	}
}

#RemoteTaskDef: {
	type: "remote"
	arn:  string
}

#ContainerDefinition: {
	name:      string
	image:     string
	cpu?:      int
	memory?:   int
	essential?: bool
	portMappings?: [...#PortMapping]
	environment?: [...#KeyValuePair]
	secrets?: [...#Secret]
	mountPoints?: [...#MountPoint]
	command?: [...string]
	entryPoint?: [...string]
	workingDirectory?: string
	healthCheck?: #HealthCheck
	logConfiguration?: #LogConfiguration
	dependsOn?: [...#ContainerDependency]
	ulimits?: [...#Ulimit]
	linuxParameters?: #LinuxParameters
}

#ContainerOverride: {
	name:   string
	image?: string
	cpu?:   int
	memory?: int
	environment?: [...#KeyValuePair]
	secrets?: [...#Secret]
	command?: [...string]
}

#PortMapping: {
	containerPort: int
	hostPort?:     int
	protocol?:     "tcp" | "udp"
	name?:         string
	appProtocol?:  "http" | "http2" | "grpc"
}

#KeyValuePair: {
	name:  string
	value: string
}

#Secret: {
	name:      string
	valueFrom: string
}

#MountPoint: {
	sourceVolume:  string
	containerPath: string
	readOnly?:     bool
}

#Volume: {
	name: string
	host?: {
		sourcePath?: string
	}
	efsVolumeConfiguration?: {
		fileSystemId:          string
		rootDirectory?:        string
		transitEncryption?:    "ENABLED" | "DISABLED"
		transitEncryptionPort?: int
		authorizationConfig?: {
			accessPointId?: string
			iam?:           "ENABLED" | "DISABLED"
		}
	}
}

#HealthCheck: {
	command: [...string]
	interval?:    int
	timeout?:     int
	retries?:     int
	startPeriod?: int
}

#LogConfiguration: {
	logDriver: "awslogs" | "fluentd" | "gelf" | "journald" | "json-file" | "splunk" | "syslog" | "awsfirelens"
	options?: [string]: string
	secretOptions?: [...#Secret]

	// Log group management (only for awslogs driver)
	// When enabled, ecsmate will create/manage the log group specified in options["awslogs-group"]
	createLogGroup?:  bool
	retentionInDays?: 1 | 3 | 5 | 7 | 14 | 30 | 60 | 90 | 120 | 150 | 180 | 365 | 400 | 545 | 731 | 1096 | 1827 | 2192 | 2557 | 2922 | 3288 | 3653
	kmsKeyId?:        string
	logGroupTags?: [string]: string
}

#ContainerDependency: {
	containerName: string
	condition:     "START" | "COMPLETE" | "SUCCESS" | "HEALTHY"
}

#Ulimit: {
	name:      string
	softLimit: int
	hardLimit: int
}

#LinuxParameters: {
	capabilities?: {
		add?: [...string]
		drop?: [...string]
	}
	initProcessEnabled?: bool
}
