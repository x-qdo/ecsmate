# ecsmate

ECS deployment management CLI for CI/CD. ecsmate renders desired state from CUE,
diffs it against live AWS resources, and applies changes with live tracking.

## Table of contents
- [Highlights](#highlights)
- [Requirements](#requirements)
- [Build and run](#build-and-run)
- [CLI basics](#cli-basics)
  - [Global flags](#global-flags)
  - [Exit codes](#exit-codes)
  - [Commands](#commands)
- [How it works](#how-it-works)
- [Diff output (differ)](#diff-output-differ)
- [Apply tracker (tracker)](#apply-tracker-tracker)
- [Manifest format](#manifest-format)
  - [Task definitions](#task-definitions)
  - [Services](#services)
  - [Scheduled tasks](#scheduled-tasks)
  - [Ingress (ALB listener rules)](#ingress-alb-listener-rules)
- [Values and overrides](#values-and-overrides)
- [SSM parameter references](#ssm-parameter-references)
- [Examples](#examples)
- [Notes and limitations](#notes-and-limitations)

## Highlights
- Declarative manifests in [CUE] with values overlays and `--set` overrides.
- Diff output that groups resource changes and shows recreate reasons.
- Apply pipeline with live tracker (interactive TTY or log-friendly output).
- ECS services, task definitions, scheduled tasks, and ALB ingress support.
- Optional SSM parameter resolution with `{{ssm:/path}}` placeholders.
- Built-in status, rollback, validate, and template commands.

## Requirements
- Go 1.25+ to build.
- AWS credentials with access to ECS, EventBridge Scheduler, ELBv2,
  Application Auto Scaling, IAM, CloudWatch Logs, and SSM (as needed).

## Build and run
```bash
go build ./cmd/ecsmate
./ecsmate --help
```

You can also run directly:
```bash
go run ./cmd/ecsmate --help
```

## CLI basics
### Global flags
- `-m, --manifest`: manifest directory (default: `.`)
- `-f, --values`: values files (repeatable, merged in order)
- `--set`: override values with `key=value` (repeatable)
- `-c, --cluster`: ECS cluster name/ARN
- `-r, --region`: AWS region
- `--log-level`: `debug|info|warn|error`
- `--no-color`: disable ANSI colors
- `--no-ssm`: skip SSM resolution

### Exit codes
- `0` success/no diff
- `1` error
- `2` diff detected (diff command only)
- `3` rollout failed (apply/rollback)

### Commands
`diff`  
Show desired vs current state. Exit code `2` if changes are detected.

`apply`  
Apply the plan. Prompts for confirmation unless `--auto-approve` is set.
Flags:
- `--auto-approve`: skip interactive confirmation
- `--no-wait`: return after submitting changes
- `--timeout`: wait timeout for deployments (default 15m)
- `--log-lines`: task log lines on failure (`-1` all, `0` none, `N` limit)

![apply.png](docs/apply.png)

`status`  
Show service status. Use `--watch` to stream updates.
Flags:
- `--service`: check a specific service (requires `--cluster`)
- `--watch`: refresh in a loop
- `--interval`: watch interval seconds
- `--events`: number of recent events per service

`rollback`  
Rollback a service to a previous task definition revision.
Flags:
- `--service`: service name (required)
- `--revision`: negative for relative (`-1` previous), positive for absolute
- `--list`: list available revisions
- `--limit`: number of revisions to list
- `--no-wait`: do not wait for deployment completion

`validate`  
Validate CUE syntax, schema constraints, and manifest content without AWS calls.

`template`  
Render the fully resolved manifest (YAML or JSON) without diffing/applying.
Flags:
- `-o, --output`: `yaml` (default) or `json`

## How it works
1. Loads all `.cue` files in the manifest directory plus `taskdefs/` and
   `values/` subdirectories.
2. Merges any `--values` files and applies `--set` overrides.
3. Resolves `{{ssm:...}}` placeholders (unless `--no-ssm`).
4. Builds desired state and discovers current ECS/ALB/Scheduler resources.
5. Generates a plan and renders a diff.
6. Applies in order:
   - Log groups (from `awslogs` container configs with `createLogGroup: true`)
   - Target groups (ingress)
   - Task definitions
   - Listener rules (ingress)
   - ECS services
   - Scheduled tasks

Service names are automatically prefixed with the manifest `name` (if set),
unless already prefixed.

## Diff output ("differ")
The diff renderer groups changes by resource and shows a boxed, structured view.
It marks actions as Create/Update/Delete/Recreate and includes recreate reasons.

Notable behaviors:
- ECS-assigned defaults like `hostPort` are ignored if only present remotely.
- Unchanged `environment` entries are suppressed to reduce noise.

## Apply tracker ("tracker")
The tracker prints structured progress with sections and task statuses. In an
interactive TTY, it re-renders service deployment progress in place, including:
- rollout state and reasons
- old vs new task definition
- per-task status (RUNNING/PENDING/STOPPED)
- progress bar and recent events

In non-interactive output, it prints events and state changes as log lines.

## Manifest format
Manifests are defined in CUE. Import the schema from `pkg/cue`.

Basic shape:
```cue
import "github.com/qdo/ecsmate/pkg/cue:schema"

manifest: schema.#Manifest & {
  name: "myapp"
  taskDefinitions: { ... }
  services: { ... }
  scheduledTasks: { ... }
  ingress: { ... }
}
```

### Task definitions
Three types:
- `managed`: fully defined in CUE
- `merged`: override an existing task definition ARN
- `remote`: use a task definition ARN as-is

Managed example:
```cue
taskDefinitions: {
  web: {
    type: "managed"
    family: "myapp-web"
    cpu: "256"
    memory: "512"
    requiresCompatibilities: ["FARGATE"]
    containerDefinitions: [{
      name: "web"
      image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/web:latest"
      portMappings: [{ containerPort: 80 }]
      logConfiguration: {
        logDriver: "awslogs"
        options: {
          awslogs-group: "/ecs/myapp/web"
          awslogs-region: "us-east-1"
          awslogs-stream-prefix: "ecs"
        }
        createLogGroup: true
        retentionInDays: 14
      }
    }]
  }
}
```

Merged example:
```cue
taskDefinitions: {
  api: {
    type: "merged"
    baseArn: "arn:aws:ecs:us-east-1:123456789012:task-definition/api:12"
    overrides: {
      cpu: "512"
      containerDefinitions: [{
        name: "api"
        image: "123456789012.dkr.ecr.us-east-1.amazonaws.com/api:v2"
      }]
    }
  }
}
```

Remote example:
```cue
taskDefinitions: {
  worker: {
    type: "remote"
    arn: "arn:aws:ecs:us-east-1:123456789012:task-definition/worker:42"
  }
}
```

### Services
```cue
services: {
  web: {
    cluster: "my-ecs-cluster"
    taskDefinition: "web"
    desiredCount: 2
    launchType: "FARGATE"
    networkConfiguration: {
      awsvpcConfiguration: {
        subnets: ["subnet-123", "subnet-456"]
        securityGroups: ["sg-123"]
        assignPublicIp: "DISABLED"
      }
    }
    deployment: {
      strategy: "rolling" // or "gradual"
      config: {
        minimumHealthyPercent: 50
        maximumPercent: 200
        circuitBreaker: { enable: true, rollback: true }
        alarms: ["my-alarm-name"]
        alarmRollback: true
      }
    }
  }
}
```

Deployment strategies:
- `rolling`: standard ECS update with deployment configuration.
- `gradual`: ECS-native staged rollout using `deployment.config.steps` with
  `percent` and `wait` (seconds).

`blue-green` and `canary` are present in the CUE schema but are not supported
by the executor; they will fail with an explicit error.

Gradual example:
```cue
deployment: {
  strategy: "gradual"
  config: {
    steps: [
      { percent: 25, wait: 60 },
      { percent: 50, wait: 60 },
      { percent: 75, wait: 60 },
      { percent: 100, wait: 0 },
    ]
  }
}
```

Auto scaling:
```cue
autoScaling: {
  minCapacity: 1
  maxCapacity: 10
  policies: [{
    name: "cpu"
    type: "TargetTrackingScaling"
    targetValue: 50
    predefinedMetric: "ECSServiceAverageCPUUtilization"
  }]
}
```

Service dependency ordering:
```cue
services: {
  web: { ... }
  worker: {
    dependsOn: ["web"]
  }
}
```

### Scheduled tasks
Scheduled tasks are created via EventBridge Scheduler. When scheduled tasks are
present, ecsmate ensures an EventBridge role exists for the scheduler.
```cue
scheduledTasks: {
  nightly: {
    taskDefinition: "cron"
    cluster: "my-ecs-cluster"
    taskCount: 1
    schedule: {
      type: "cron"
      expression: "0 2 * * ? *"
    }
    networkConfiguration: {
      awsvpcConfiguration: {
        subnets: ["subnet-123"]
        securityGroups: ["sg-123"]
        assignPublicIp: "DISABLED"
      }
    }
  }
}
```

### Ingress (ALB listener rules)
Ingress rules generate target groups and listener rules.
```cue
ingress: {
  listenerArn: "arn:aws:elasticloadbalancing:..."
  vpcId: "vpc-123"
  rules: [{
    priority: 10
    host: "app.example.com"
    service: {
      name: "web"
      containerName: "web"
      containerPort: 80
    }
    healthCheck: { path: "/health", matcher: "200" }
  }]
}
```

## Values and overrides
Manifests commonly split defaults and environment-specific values under
`values/`. Provide extra overlays with `-f` and ad-hoc changes with `--set`.

Examples:
```bash
ecsmate diff -m ./deploy -f values/staging.cue
ecsmate apply -m ./deploy --set image.tag=v1.2.3
```

## SSM parameter references
Any string field can include `{{ssm:/path/to/param}}`. ecsmate resolves these
in batch via SSM (and splits StringList values on commas for subnets/security
groups). Use `--no-ssm` to keep placeholders intact.

Example:
```cue
cluster: "{{ssm:/myapp/prod/ecs/cluster_name}}"
```

## Examples
Two sample manifests live under `examples/`:
- `examples/webapp`: web + worker services with scheduled cron task.
- `examples/cloudinsurance`: ingress rules, service discovery, and SSM-backed
  infrastructure values.

Try:
```bash
ecsmate validate -m examples/webapp
ecsmate diff -m examples/webapp -f examples/webapp/values/staging.cue
ecsmate template -m examples/cloudinsurance -o yaml
```

## Notes and limitations
- Service names are prefixed with `manifest.name` unless already prefixed.
- `blue-green` and `canary` strategies require CodeDeploy and are not supported
  by the executor.
- Log groups are only created when using `awslogs` with `createLogGroup: true`.
- Some schema fields are not wired by the parser yet (for example scheduled task
  `tags`, `retryPolicy`, `deadLetterConfig`, `group`, `platformVersion`).

[CUE]: https://github.com/cue-lang/cue