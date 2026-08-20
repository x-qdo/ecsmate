package config

import (
	"strings"
	"testing"

	"cuelang.org/go/cue/cuecontext"
)

func TestParseContainerDefinition_RejectsNonConcreteFields(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "scalar", body: `cpu: int`, wantErr: "cpu"},
		{name: "environment", body: `environment: FOO: string`, wantErr: "environment.FOO"},
		{name: "port mappings", body: `portMappings: [{containerPort: int}]`, wantErr: "portMappings.0.containerPort"},
		{name: "mount point", body: `mountPoints: [{sourceVolume: string, containerPath: "/data"}]`, wantErr: "mountPoints.0.sourceVolume"},
		{name: "health check", body: `healthCheck: command: [string]`, wantErr: "healthCheck.command"},
		{name: "dependency", body: `dependsOn: [{containerName: string, condition: "SUCCESS"}]`, wantErr: "dependsOn.0.containerName"},
		{name: "Linux capabilities", body: `linuxParameters: capabilities: add: [string]`, wantErr: "linuxParameters.capabilities.add"},
		{name: "ulimit", body: `ulimits: [{name: "nofile", softLimit: int, hardLimit: 2048}]`, wantErr: "ulimits.0.softLimit"},
		{name: "log option", body: `logConfiguration: {logDriver: "awslogs", options: region: string}`, wantErr: "logConfiguration.options.region"},
	}

	ctx := cuecontext.New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			value := ctx.CompileString(`{name: "app", image: "app:latest", ` + tt.body + `}`)
			if err := value.Err(); err != nil {
				t.Fatalf("compile test value: %v", err)
			}

			_, err := parseContainerDefinition(value)
			if err == nil {
				t.Fatal("expected non-concrete field to fail parsing")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error to contain %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestParseContainerDefinition_RequiresNameAndImage(t *testing.T) {
	ctx := cuecontext.New()
	tests := []struct {
		name    string
		value   string
		wantErr string
	}{
		{name: "missing name", value: `{image: "app:latest"}`, wantErr: "name is required"},
		{name: "missing image", value: `{name: "app"}`, wantErr: "image is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseContainerDefinition(ctx.CompileString(tt.value))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestParseManifest_Basic(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "test-app"
		taskDefinitions: {
			web: {
				type: "managed"
				family: "test-web"
				cpu: "256"
				memory: "512"
				networkMode: "awsvpc"
				containerDefinitions: [{
					name: "app"
					image: "nginx:latest"
					essential: true
					portMappings: [{
						containerPort: 80
						protocol: "tcp"
					}]
				}]
			}
		}
		services: {
			web: {
				cluster: "test-cluster"
				taskDefinition: "web"
				desiredCount: 3
				launchType: "FARGATE"
				healthCheckGracePeriodSeconds: 25
				loadBalancers: [{
					targetGroupArn: "arn:aws:elasticloadbalancing:us-east-1:123456789:targetgroup/test/abc123"
					containerName: "app"
					containerPort: 80
				}]
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	if manifest.Name != "test-app" {
		t.Errorf("expected name 'test-app', got '%s'", manifest.Name)
	}

	if len(manifest.TaskDefinitions) != 1 {
		t.Errorf("expected 1 task definition, got %d", len(manifest.TaskDefinitions))
	}

	td, ok := manifest.TaskDefinitions["web"]
	if !ok {
		t.Fatal("expected task definition 'web' not found")
	}

	if td.Type != "managed" {
		t.Errorf("expected type 'managed', got '%s'", td.Type)
	}
	if td.Family != "test-web" {
		t.Errorf("expected family 'test-web', got '%s'", td.Family)
	}
	if td.CPU != "256" {
		t.Errorf("expected cpu '256', got '%s'", td.CPU)
	}
	if td.Memory != "512" {
		t.Errorf("expected memory '512', got '%s'", td.Memory)
	}

	if len(td.ContainerDefinitions) != 1 {
		t.Fatalf("expected 1 container definition, got %d", len(td.ContainerDefinitions))
	}

	cd := td.ContainerDefinitions[0]
	if cd.Name != "app" {
		t.Errorf("expected container name 'app', got '%s'", cd.Name)
	}
	if cd.Image != "nginx:latest" {
		t.Errorf("expected image 'nginx:latest', got '%s'", cd.Image)
	}
	if !cd.Essential {
		t.Error("expected container to be essential")
	}

	if len(manifest.Services) != 1 {
		t.Errorf("expected 1 service, got %d", len(manifest.Services))
	}

	svc, ok := manifest.Services["web"]
	if !ok {
		t.Fatal("expected service 'web' not found")
	}

	if svc.Cluster != "test-cluster" {
		t.Errorf("expected cluster 'test-cluster', got '%s'", svc.Cluster)
	}
	if svc.DesiredCount != 3 {
		t.Errorf("expected desiredCount 3, got %d", svc.DesiredCount)
	}
	if !svc.HealthCheckGracePeriodSecondsSet || svc.HealthCheckGracePeriodSeconds != 25 {
		t.Errorf("expected healthCheckGracePeriodSeconds 25, got %d", svc.HealthCheckGracePeriodSeconds)
	}
	if len(svc.LoadBalancers) != 1 {
		t.Fatalf("expected 1 load balancer, got %d", len(svc.LoadBalancers))
	}
	if svc.LoadBalancers[0].TargetGroupArn == "" {
		t.Error("expected loadBalancer targetGroupArn to be set")
	}
}

func TestParseManifest_MergedTaskDef(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "merged-app"
		taskDefinitions: {
			nginx: {
				type: "merged"
				baseArn: "arn:aws:ecs:us-east-1:123456789:task-definition/nginx-base:3"
				overrides: {
					cpu: "512"
					memory: "1024"
				}
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	td, ok := manifest.TaskDefinitions["nginx"]
	if !ok {
		t.Fatal("expected task definition 'nginx' not found")
	}

	if td.Type != "merged" {
		t.Errorf("expected type 'merged', got '%s'", td.Type)
	}
	if td.BaseArn != "arn:aws:ecs:us-east-1:123456789:task-definition/nginx-base:3" {
		t.Errorf("unexpected baseArn: %s", td.BaseArn)
	}
}

func TestParseManifest_RemoteTaskDef(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "remote-app"
		taskDefinitions: {
			cron: {
				type: "remote"
				arn: "arn:aws:ecs:us-east-1:123456789:task-definition/shared-cron:5"
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	td, ok := manifest.TaskDefinitions["cron"]
	if !ok {
		t.Fatal("expected task definition 'cron' not found")
	}

	if td.Type != "remote" {
		t.Errorf("expected type 'remote', got '%s'", td.Type)
	}
	if td.Arn != "arn:aws:ecs:us-east-1:123456789:task-definition/shared-cron:5" {
		t.Errorf("unexpected arn: %s", td.Arn)
	}
}

func TestParseManifest_ManagedSecretsKMSKeyRegion(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "secret-app"
		secrets: {
			managed: {
				file: "secrets.enc.yaml"
				kmsKeyArn: "arn:aws:kms:us-east-1:123456789012:key/abc"
				kmsKeyRegion: "us-east-1"
				ssmKmsKeyId: "alias/app-ssm"
				ssmPrefix: "/secret-app/prod"
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	if manifest.Secrets == nil || manifest.Secrets.Managed == nil {
		t.Fatal("expected managed secrets config")
	}
	if manifest.Secrets.Managed.KMSKeyRegion != "us-east-1" {
		t.Fatalf("expected KMS key region us-east-1, got %q", manifest.Secrets.Managed.KMSKeyRegion)
	}
	if manifest.Secrets.Managed.SSMKMSKeyID != "alias/app-ssm" {
		t.Fatalf("expected SSM KMS key ID alias/app-ssm, got %q", manifest.Secrets.Managed.SSMKMSKeyID)
	}
}

func TestParseManifest_ContainerDefinitionFull(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "full-app"
		taskDefinitions: {
			app: {
				type: "managed"
				family: "my-app"
				cpu: "256"
				memory: "512"
				containerDefinitions: [{
					name: "main"
					image: "my-image:v1"
					cpu: 128
					memory: 256
					essential: true
					command: ["serve", "--port", "8080"]
					entryPoint: ["/bin/sh", "-c"]
					workingDirectory: "/app"
					environment: {
						ENV: "production"
						DEBUG: "false"
					}
					secrets: {
						DB_PASSWORD: "arn:aws:ssm:us-east-1:123456789:parameter/db-password"
					}
					portMappings: [{
						containerPort: 8080
						hostPort: 8080
						protocol: "tcp"
						name: "http"
						appProtocol: "http"
					}]
					mountPoints: [{
						sourceVolume: "data"
						containerPath: "/data"
						readOnly: true
					}]
					healthCheck: {
						command: ["CMD-SHELL", "curl -f http://localhost:8080/health || exit 1"]
						interval: 30
						timeout: 5
						retries: 3
						startPeriod: 10
					}
					dependsOn: [{
						containerName: "init"
						condition: "SUCCESS"
					}]
					linuxParameters: {
						initProcessEnabled: true
						capabilities: {
							add: ["SYS_PTRACE"]
							drop: ["NET_RAW"]
						}
					}
					ulimits: [{
						name: "nofile"
						softLimit: 1024
						hardLimit: 2048
					}]
					logConfiguration: {
						logDriver: "awslogs"
						options: {
							"awslogs-group":         "/ecs/my-app"
							"awslogs-region":        "us-east-1"
							"awslogs-stream-prefix": "ecs"
						}
					}
				}]
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	td := manifest.TaskDefinitions["app"]
	cd := td.ContainerDefinitions[0]

	if cd.CPU != 128 {
		t.Errorf("expected cpu 128, got %d", cd.CPU)
	}
	if cd.Memory != 256 {
		t.Errorf("expected memory 256, got %d", cd.Memory)
	}
	if cd.WorkingDirectory != "/app" {
		t.Errorf("expected workingDirectory '/app', got '%s'", cd.WorkingDirectory)
	}

	if len(cd.Command) != 3 {
		t.Errorf("expected 3 command args, got %d", len(cd.Command))
	}

	if len(cd.EntryPoint) != 2 {
		t.Errorf("expected 2 entrypoint args, got %d", len(cd.EntryPoint))
	}

	if len(cd.Environment) != 2 {
		t.Errorf("expected 2 environment vars, got %d", len(cd.Environment))
	}
	envMap := make(map[string]string)
	for _, kv := range cd.Environment {
		envMap[kv.Name] = kv.Value
	}
	if envMap["ENV"] != "production" {
		t.Errorf("expected ENV=production, got %s", envMap["ENV"])
	}

	if len(cd.Secrets) != 1 {
		t.Errorf("expected 1 secret, got %d", len(cd.Secrets))
	}
	if cd.Secrets[0].Name != "DB_PASSWORD" {
		t.Errorf("expected secret name 'DB_PASSWORD', got '%s'", cd.Secrets[0].Name)
	}

	if len(cd.PortMappings) != 1 {
		t.Errorf("expected 1 port mapping, got %d", len(cd.PortMappings))
	}
	if cd.PortMappings[0].ContainerPort != 8080 {
		t.Errorf("expected containerPort 8080, got %d", cd.PortMappings[0].ContainerPort)
	}
	if cd.PortMappings[0].Name != "http" || cd.PortMappings[0].AppProtocol != "http" {
		t.Errorf("expected named HTTP port mapping, got name=%q appProtocol=%q", cd.PortMappings[0].Name, cd.PortMappings[0].AppProtocol)
	}

	if len(cd.MountPoints) != 1 || cd.MountPoints[0] != (MountPoint{SourceVolume: "data", ContainerPath: "/data", ReadOnly: true}) {
		t.Errorf("unexpected mount points: %+v", cd.MountPoints)
	}

	if cd.HealthCheck == nil {
		t.Fatal("expected health check")
	}
	if len(cd.HealthCheck.Command) != 2 || cd.HealthCheck.Interval != 30 || cd.HealthCheck.Timeout != 5 || cd.HealthCheck.Retries != 3 || cd.HealthCheck.StartPeriod != 10 {
		t.Errorf("unexpected health check: %+v", cd.HealthCheck)
	}

	if len(cd.DependsOn) != 1 || cd.DependsOn[0] != (ContainerDependency{ContainerName: "init", Condition: "SUCCESS"}) {
		t.Errorf("unexpected container dependencies: %+v", cd.DependsOn)
	}

	if cd.LinuxParameters == nil || !cd.LinuxParameters.InitProcessEnabled || cd.LinuxParameters.Capabilities == nil {
		t.Fatalf("unexpected Linux parameters: %+v", cd.LinuxParameters)
	}
	if len(cd.LinuxParameters.Capabilities.Add) != 1 || cd.LinuxParameters.Capabilities.Add[0] != "SYS_PTRACE" || len(cd.LinuxParameters.Capabilities.Drop) != 1 || cd.LinuxParameters.Capabilities.Drop[0] != "NET_RAW" {
		t.Errorf("unexpected Linux capabilities: %+v", cd.LinuxParameters.Capabilities)
	}

	if len(cd.Ulimits) != 1 || cd.Ulimits[0] != (Ulimit{Name: "nofile", SoftLimit: 1024, HardLimit: 2048}) {
		t.Errorf("unexpected ulimits: %+v", cd.Ulimits)
	}

	if cd.LogConfiguration == nil {
		t.Fatal("expected log configuration")
	}
	if cd.LogConfiguration.LogDriver != "awslogs" {
		t.Errorf("expected logDriver 'awslogs', got '%s'", cd.LogConfiguration.LogDriver)
	}
	if cd.LogConfiguration.Options["awslogs-group"] != "/ecs/my-app" {
		t.Errorf("unexpected awslogs-group: %s", cd.LogConfiguration.Options["awslogs-group"])
	}
}

func TestParseManifest_LogSubscriptionFilters(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "log-filter-app"
		taskDefinitions: {
			web: {
				type: "managed"
				containerDefinitions: [{
					name: "app"
					image: "nginx:latest"
					logConfiguration: {
						logDriver: "awslogs"
						options: {
							"awslogs-group": "/ecs/log-filter-app"
						}
						createLogGroup: true
						subscriptionFilters: [{
							name: "slack-error-forwarder"
							destinationArn: "arn:aws:lambda:eu-west-1:123456789012:function:slack"
							filterPattern: "?ERROR ?Error ?error ?Exception ?CRITICAL ?Critical ?Fatal ?fatal"
						}, {
							name: "audit-stream"
							destinationArn: "arn:aws:kinesis:eu-west-1:123456789012:stream:audit"
							roleArn: "arn:aws:iam::123456789012:role/log-delivery"
							distribution: "Random"
						}]
					}
				}]
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	filters := manifest.TaskDefinitions["web"].ContainerDefinitions[0].LogConfiguration.SubscriptionFilters
	if len(filters) != 2 {
		t.Fatalf("expected 2 subscription filters, got %d", len(filters))
	}
	if filters[0].Name != "slack-error-forwarder" {
		t.Errorf("expected first filter name slack-error-forwarder, got %s", filters[0].Name)
	}
	if filters[0].FilterPattern != "?ERROR ?Error ?error ?Exception ?CRITICAL ?Critical ?Fatal ?fatal" {
		t.Errorf("unexpected filter pattern: %s", filters[0].FilterPattern)
	}
	if filters[1].FilterPattern != "" {
		t.Errorf("expected omitted filterPattern to default to empty string, got %s", filters[1].FilterPattern)
	}
	if filters[1].RoleArn == "" || filters[1].Distribution != "Random" {
		t.Errorf("expected role and Random distribution, got role=%q distribution=%q", filters[1].RoleArn, filters[1].Distribution)
	}
}

func TestParseManifest_ServiceDeployment(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "deploy-app"
		services: {
			web: {
				cluster: "prod-cluster"
				taskDefinition: "web"
				desiredCount: 5
				deployment: {
					strategy: "rolling"
					config: {
						minimumHealthyPercent: 50
						maximumPercent: 200
						circuitBreaker: {
							enable: true
							rollback: true
						}
						alarms: ["high-cpu", "error-rate"]
						alarmRollback: true
					}
				}
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	svc := manifest.Services["web"]

	if svc.Deployment.Strategy != "rolling" {
		t.Errorf("expected strategy 'rolling', got '%s'", svc.Deployment.Strategy)
	}
	if svc.Deployment.MinimumHealthyPercent != 50 {
		t.Errorf("expected minimumHealthyPercent 50, got %d", svc.Deployment.MinimumHealthyPercent)
	}
	if !svc.Deployment.MinimumHealthyPercentSet {
		t.Error("expected minimumHealthyPercent to be marked as set")
	}
	if svc.Deployment.MaximumPercent != 200 {
		t.Errorf("expected maximumPercent 200, got %d", svc.Deployment.MaximumPercent)
	}
	if !svc.Deployment.MaximumPercentSet {
		t.Error("expected maximumPercent to be marked as set")
	}
	if !svc.Deployment.CircuitBreakerEnable {
		t.Error("expected circuit breaker to be enabled")
	}
	if !svc.Deployment.CircuitBreakerRollback {
		t.Error("expected circuit breaker rollback to be enabled")
	}
	if len(svc.Deployment.Alarms) != 2 {
		t.Errorf("expected 2 alarms, got %d", len(svc.Deployment.Alarms))
	}
	if !svc.Deployment.AlarmRollbackEnable {
		t.Error("expected alarm rollback to be enabled")
	}
}

func TestParseManifest_GradualDeployment(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "gradual-app"
		services: {
			api: {
				cluster: "prod"
				taskDefinition: "api"
				desiredCount: 10
				deployment: {
					strategy: "gradual"
					config: {
						steps: [
							{percent: 25, wait: 60},
							{percent: 50, wait: 60},
							{percent: 75, wait: 60},
							{percent: 100, wait: 0}
						]
					}
				}
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	svc := manifest.Services["api"]

	if svc.Deployment.Strategy != "gradual" {
		t.Errorf("expected strategy 'gradual', got '%s'", svc.Deployment.Strategy)
	}
	if len(svc.Deployment.GradualSteps) != 4 {
		t.Fatalf("expected 4 gradual steps, got %d", len(svc.Deployment.GradualSteps))
	}

	expectedSteps := []struct {
		percent int
		wait    int
	}{
		{25, 60}, {50, 60}, {75, 60}, {100, 0},
	}

	for i, expected := range expectedSteps {
		if svc.Deployment.GradualSteps[i].Percent != expected.percent {
			t.Errorf("step %d: expected percent %d, got %d", i, expected.percent, svc.Deployment.GradualSteps[i].Percent)
		}
		if svc.Deployment.GradualSteps[i].WaitSeconds != expected.wait {
			t.Errorf("step %d: expected wait %d, got %d", i, expected.wait, svc.Deployment.GradualSteps[i].WaitSeconds)
		}
	}
}

func TestParseManifest_NetworkConfiguration(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "network-app"
		services: {
			web: {
				cluster: "test"
				taskDefinition: "web"
				desiredCount: 1
				networkConfiguration: {
					awsvpcConfiguration: {
						subnets: ["subnet-1", "subnet-2"]
						securityGroups: ["sg-1", "sg-2"]
						assignPublicIp: "ENABLED"
					}
				}
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	svc := manifest.Services["web"]

	if svc.NetworkConfiguration == nil {
		t.Fatal("expected network configuration")
	}
	if len(svc.NetworkConfiguration.Subnets) != 2 {
		t.Errorf("expected 2 subnets, got %d", len(svc.NetworkConfiguration.Subnets))
	}
	if len(svc.NetworkConfiguration.SecurityGroups) != 2 {
		t.Errorf("expected 2 security groups, got %d", len(svc.NetworkConfiguration.SecurityGroups))
	}
	if svc.NetworkConfiguration.AssignPublicIp != "ENABLED" {
		t.Errorf("expected assignPublicIp 'ENABLED', got '%s'", svc.NetworkConfiguration.AssignPublicIp)
	}
}

func TestParseManifest_ScheduledTasks(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "scheduled-app"
		scheduledTasks: {
			dailyReport: {
				taskDefinition: "cron"
				cluster: "prod-cluster"
				taskCount: 1
				platformVersion: "1.4.0"
				group: "reporting"
				tags: [
					{ key: "env", value: "prod" },
					{ key: "team", value: "data" },
				]
				deadLetterConfig: {
					arn: "arn:aws:sqs:us-east-1:123456789012:dlq"
				}
				retryPolicy: {
					maximumEventAgeInSeconds: 120
					maximumRetryAttempts: 3
				}
				schedule: {
					type: "cron"
					expression: "0 2 * * ? *"
					timezone: "America/New_York"
				}
				launchType: "FARGATE"
				networkConfiguration: {
					awsvpcConfiguration: {
						subnets: ["subnet-1"]
						securityGroups: ["sg-1"]
						assignPublicIp: "DISABLED"
					}
				}
			}
			hourlySync: {
				taskDefinition: "sync"
				cluster: "prod-cluster"
				taskCount: 2
				schedule: {
					type: "rate"
					expression: "1 hour"
				}
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	if len(manifest.ScheduledTasks) != 2 {
		t.Errorf("expected 2 scheduled tasks, got %d", len(manifest.ScheduledTasks))
	}

	daily := manifest.ScheduledTasks["dailyReport"]
	if daily.TaskDefinition != "cron" {
		t.Errorf("expected taskDefinition 'cron', got '%s'", daily.TaskDefinition)
	}
	if daily.ScheduleType != "cron" {
		t.Errorf("expected scheduleType 'cron', got '%s'", daily.ScheduleType)
	}
	if daily.ScheduleExpression != "0 2 * * ? *" {
		t.Errorf("unexpected schedule expression: %s", daily.ScheduleExpression)
	}
	if daily.Timezone != "America/New_York" {
		t.Errorf("expected timezone 'America/New_York', got '%s'", daily.Timezone)
	}
	if daily.PlatformVersion != "1.4.0" {
		t.Errorf("expected platformVersion '1.4.0', got '%s'", daily.PlatformVersion)
	}
	if daily.Group != "reporting" {
		t.Errorf("expected group 'reporting', got '%s'", daily.Group)
	}
	if len(daily.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(daily.Tags))
	}
	if daily.DeadLetterConfig == nil || daily.DeadLetterConfig.Arn == "" {
		t.Errorf("expected deadLetterConfig arn to be set")
	}
	if daily.RetryPolicy == nil || daily.RetryPolicy.MaximumRetryAttempts != 3 {
		t.Errorf("expected retryPolicy maximumRetryAttempts 3")
	}

	hourly := manifest.ScheduledTasks["hourlySync"]
	if hourly.ScheduleType != "rate" {
		t.Errorf("expected scheduleType 'rate', got '%s'", hourly.ScheduleType)
	}
	if hourly.TaskCount != 2 {
		t.Errorf("expected taskCount 2, got %d", hourly.TaskCount)
	}
}

func TestParseManifest_ServiceDependsOn(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "deps-app"
		services: {
			api: {
				cluster: "test"
				taskDefinition: "api"
				desiredCount: 1
				dependsOn: ["db", "cache"]
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	svc := manifest.Services["api"]
	if len(svc.DependsOn) != 2 {
		t.Errorf("expected 2 dependencies, got %d", len(svc.DependsOn))
	}
	if svc.DependsOn[0] != "db" || svc.DependsOn[1] != "cache" {
		t.Errorf("unexpected dependencies: %v", svc.DependsOn)
	}
}

func TestParseManifest_ServiceRegistries(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "test-app"
		taskDefinitions: {
			web: {
				type: "managed"
				family: "test-web"
				containerDefinitions: [{
					name: "app"
					image: "nginx:latest"
				}]
			}
		}
		services: {
			web: {
				cluster: "test-cluster"
				taskDefinition: "web"
				desiredCount: 1
				serviceRegistries: [{
					registryArn: "arn:aws:servicediscovery:us-east-1:123:service/srv-abc"
					containerName: "app"
					containerPort: 80
				}, {
					serviceDiscovery: {
						namespaceArn: "arn:aws:servicediscovery:us-east-1:123:namespace/ns-abc"
						tags: {
							Env: "dev"
						}
					}
					containerName: "app"
					containerPort: 80
				}]
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	svc, ok := manifest.Services["web"]
	if !ok {
		t.Fatal("expected service 'web' not found")
	}
	if len(svc.ServiceRegistries) != 2 {
		t.Fatalf("expected 2 service registries, got %d", len(svc.ServiceRegistries))
	}

	external := svc.ServiceRegistries[0]
	if external.RegistryArn == "" {
		t.Error("expected external registry ARN to be set")
	}
	if external.ServiceDiscovery != nil {
		t.Error("expected external registry to not have service discovery")
	}

	managed := svc.ServiceRegistries[1]
	if managed.ServiceDiscovery == nil {
		t.Fatal("expected managed service discovery to be set")
	}
	if managed.ServiceDiscovery.NamespaceArn == "" {
		t.Error("expected namespace ARN to be set")
	}
	if managed.ServiceDiscovery.DNSRecordType != "A" {
		t.Errorf("expected default DNSRecordType A, got %q", managed.ServiceDiscovery.DNSRecordType)
	}
	if managed.ServiceDiscovery.DNSTTL != 60 {
		t.Errorf("expected default DNSTTL 60, got %d", managed.ServiceDiscovery.DNSTTL)
	}
	if managed.ServiceDiscovery.RoutingPolicy != "MULTIVALUE" {
		t.Errorf("expected default RoutingPolicy MULTIVALUE, got %q", managed.ServiceDiscovery.RoutingPolicy)
	}
	if managed.ServiceDiscovery.Tags["Env"] != "dev" {
		t.Errorf("expected tag Env=dev, got %q", managed.ServiceDiscovery.Tags["Env"])
	}
}

func TestParseManifest_TaskDefinitionMissingType(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{
		name: "bad-app"
		taskDefinitions: {
			web: {
				family: "test"
			}
		}
	}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	_, err := ParseManifest(value)
	if err == nil {
		t.Error("expected error for missing task definition type")
	}
}

func TestParseManifest_Empty(t *testing.T) {
	ctx := cuecontext.New()
	cueStr := `{}`

	value := ctx.CompileString(cueStr)
	if value.Err() != nil {
		t.Fatalf("failed to compile CUE: %v", value.Err())
	}

	manifest, err := ParseManifest(value)
	if err != nil {
		t.Fatalf("failed to parse manifest: %v", err)
	}

	if len(manifest.TaskDefinitions) != 0 {
		t.Errorf("expected 0 task definitions, got %d", len(manifest.TaskDefinitions))
	}
	if len(manifest.Services) != 0 {
		t.Errorf("expected 0 services, got %d", len(manifest.Services))
	}
	if len(manifest.ScheduledTasks) != 0 {
		t.Errorf("expected 0 scheduled tasks, got %d", len(manifest.ScheduledTasks))
	}
}
