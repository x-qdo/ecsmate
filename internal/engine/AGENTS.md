# ENGINE PACKAGE

Orchestration layer: planning, dependency resolution, execution, and progress tracking.

## FILES

| File | Purpose |
|------|---------|
| `planner.go` | Compares desired vs current state, generates `Plan` with `DiffEntry` items |
| `depgraph.go` | DAG for resource dependencies, topological sort, change propagation |
| `executor.go` | Applies plan to AWS in dependency order with parallel execution |
| `tracker.go` | Interactive TTY progress display, event streaming |
| `rollback.go` | Service rollback to previous task definition |
| `operations.go` | Shared operation types |

## KEY TYPES

```go
Plan           // Contains DesiredState + []DiffEntry + Summary
DependencyGraph // DAG with nodes (resources) and edges (dependencies)
Executor       // Orchestrates AWS calls with Tracker
Tracker        // Progress display (interactive/non-interactive modes)
```

## EXECUTION ORDER

Hardcoded in `executor.go:Execute()`:
1. LogGroups
2. ServiceDiscovery
3. TargetGroups
4. TaskDefs (parallel)
5. ListenerRules
6. Services (by dependency level, with hooks)
7. ScheduledTasks

## CHANGE PROPAGATION

`depgraph.go:PropagateChanges()` cascades updates through DAG:
- TaskDef UPDATE → dependent Services/ScheduledTasks get UPDATE
- TargetGroup RECREATE → dependent ListenerRules get UPDATE
- ListenerRule propagation suppressed unless actual config changes OR recreation

## TRACKER MODES

- **Interactive TTY**: Re-renders service deployment progress in place
- **Non-interactive**: Prints events and state changes as log lines

## WAIT BEHAVIOR

`executor.go:waitForService()` polls ECS until:
- `RolloutState == COMPLETED` (success)
- `RolloutState == FAILED` (circuit breaker)
- Rollback detected (task def mismatch after completion)
- Timeout (default 15m)

On failure: fetches CloudWatch logs from stopped tasks.

## HOOKS

Pre/post deployment hooks run as one-off ECS tasks:
- Pre-hook failure blocks deployment
- Post-hook failure logged but doesn't fail deployment
