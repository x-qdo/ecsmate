package config

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
)

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
					environment: [{
						name: "ENV"
						value: "production"
					}, {
						name: "DEBUG"
						value: "false"
					}]
					secrets: [{
						name: "DB_PASSWORD"
						valueFrom: "arn:aws:ssm:us-east-1:123456789:parameter/db-password"
					}]
					portMappings: [{
						containerPort: 8080
						hostPort: 8080
						protocol: "tcp"
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
	if cd.Environment[0].Name != "ENV" || cd.Environment[0].Value != "production" {
		t.Errorf("unexpected environment[0]: %+v", cd.Environment[0])
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
	if svc.Deployment.MaximumPercent != 200 {
		t.Errorf("expected maximumPercent 200, got %d", svc.Deployment.MaximumPercent)
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
