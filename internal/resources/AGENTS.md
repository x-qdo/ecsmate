# RESOURCES PACKAGE

Resource managers that bridge CUE manifests to AWS API calls.

## MANAGER PATTERN

Each AWS resource type has a manager with consistent interface:

```go
type XxxManager struct { client *aws.XxxClient }

func (m *XxxManager) BuildResource(ctx, name, spec) (*XxxResource, error)
func (m *XxxManager) DetermineAction(desired, current) Action
func (m *XxxManager) Apply(ctx, resource) error
```

## FILES

| File | Resource Type | AWS Service |
|------|---------------|-------------|
| `taskdef.go` | Task definitions | ECS |
| `service.go` | ECS services + auto scaling | ECS, App Auto Scaling |
| `scheduled.go` | Scheduled tasks | EventBridge Scheduler |
| `targetgroup.go` | ALB target groups | ELBv2 |
| `listenerrule.go` | ALB listener rules | ELBv2 |
| `ingress.go` | Read-only ingress ownership checks and planning | ELBv2 |
| `servicediscovery.go` | Cloud Map services | Service Discovery |
| `loggroup.go` | CloudWatch log groups | CloudWatch Logs |
| `discovery.go` | State orchestration | (coordinates all managers) |
| `hook.go` | Pre/post deployment hooks | ECS RunTask |

## DISCOVERY FLOW

CLI calls `DiscoverIngress()` before scheduler IAM role creation, then passes the checked ingress plan to `discovery.go:BuildDesiredState()`:

1. Build TaskDefs (from manifest)
2. Build ServiceDiscovery (extract from service registries)
3. Build Services (link to TaskDefs, resolve SD ARNs)
4. Build ScheduledTasks (link to TaskDefs)
5. Validate ingress-managed service target-group bindings against manifest ownership

Ingress resources come from the supplied plan, not a second discovery. The executor revalidates the plan before its first write.

## ACTION TYPES

```go
ActionCreate   // Resource doesn't exist
ActionUpdate   // Resource exists, config differs
ActionRecreate // Immutable field changed (e.g., TG port/protocol)
ActionDelete   // Orphan detection
ActionNoop     // No changes
```

## OWNERSHIP

Resources tagged with `ManagedBy=ecsmate` for:
- Orphan detection (delete resources no longer in manifest)
- Safe cleanup (won't touch unmanaged resources)

Ingress additionally requires every manifest tag to match; target groups must also match the manifest naming pattern. Untagged legacy forwarding rules require verified target-group ownership. Other ingress rules require their own matching tags.

## ORPHAN DETECTION

- TargetGroups: naming pattern `{manifestName}-r{priority}` + tag check
- ServiceDiscovery: by namespace, tag check
- ListenerRules: by host/path first; priority fallback only for manageable, verified-owned rules. No automatic priority allocation. Protected rules and their referenced target groups are not cleaned up.

## RECREATE TRIGGERS

| Resource | Immutable Fields |
|----------|------------------|
| TargetGroup | Port, Protocol, TargetType, VpcId |
| Service | LaunchType, LoadBalancers (cannot add/remove) |

## SERVICE AUTO SCALING

`service.go` integrates Application Auto Scaling:
- Registers scalable target
- Creates/updates scaling policies
- Handles cleanup on service delete
