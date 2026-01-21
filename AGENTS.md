# ECSMATE KNOWLEDGE BASE

**Generated:** 2026-01-21
**Commit:** 6209b01
**Branch:** fix-differ

## OVERVIEW

ECS deployment CLI. Renders desired state from CUE manifests, diffs against AWS, applies changes with live tracking.

## STRUCTURE

```
ecsmate/
├── cmd/ecsmate/       # Entry point (main.go calls cli.Execute)
├── internal/
│   ├── cli/           # Cobra commands (diff, apply, status, rollback, validate, template)
│   ├── config/        # CUE loading, manifest parsing, SSM resolution
│   ├── resources/     # Resource managers, state building, AWS discovery
│   ├── engine/        # Planner, DAG, Executor, Tracker
│   ├── diff/          # Change rendering and formatting
│   ├── aws/           # AWS SDK wrappers (ECS, ELBv2, Scheduler, IAM, SSM, CloudWatch)
│   └── log/           # slog-based logging wrapper
├── pkg/cue/           # CUE schema (embedded in binary via go:embed)
└── examples/          # Sample manifests (webapp, cloudinsurance)
```

## DATA FLOW

```
CUE Files → CUELoader → Manifest → ResourceBuilder → DesiredState
                                          ↓
                          AWS APIs (discover current state)
                                          ↓
                                    Planner + DAG
                                          ↓
                        ┌─────────────────┴─────────────────┐
                        ↓                                   ↓
                  Diff Renderer                         Executor
                                                            ↓
                                                         Tracker
```

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Add CLI command | `internal/cli/` | New file, register in `root.go` init() |
| Modify manifest schema | `pkg/cue/*.cue` | Update CUE, then `internal/config/manifest.go` |
| Add AWS resource type | `internal/resources/` | Follow manager pattern (see `scheduled.go`) |
| Change diff output | `internal/diff/renderer.go` | |
| Modify execution order | `internal/engine/depgraph.go` | DAG edges, topological sort |
| Add deployment tracking | `internal/engine/tracker.go` | Interactive TTY logic |

## CONVENTIONS

### CUE Schema
- Schema in `pkg/cue/` embedded via `embed.go`
- Users import `github.com/qdo/ecsmate/pkg/cue:schema`
- Use `type | *"default"` for `--set` overridable values (NOT concrete values)

### Resource Managers
- One manager per AWS resource type in `internal/resources/`
- Pattern: `BuildResource()` → `DetermineAction()` → `Apply()`
- Resources tagged `ManagedBy=ecsmate` for ownership

### Execution Order (hardcoded in Executor)
LogGroups → ServiceDiscovery → TargetGroups → TaskDefs → ListenerRules → Services → ScheduledTasks

### Service Naming
- Auto-prefixed with `manifest.name` unless already prefixed

### Error Handling
- Always `fmt.Errorf("context: %w", err)` for wrapping
- Exit codes: 0=success, 1=error, 2=diff detected, 3=rollout failed

## ANTI-PATTERNS

- **blue-green/canary strategies**: Schema allows but executor rejects (requires CodeDeploy)
- **Concrete CUE values**: `tag: "latest"` blocks `--set` overrides; use `tag: string | *"latest"`
- **Type suppression**: Never `as any`, `@ts-ignore` equivalent patterns

## TESTING

- Table-driven tests in `*_test.go` files
- No external test frameworks (standard `testing` package only)
- Manual mocks defined in test files (e.g., `mockSSMResolver`)
- `examples/` used for integration testing with `validate`/`template` commands

```bash
go test ./...                           # All tests
go test ./internal/engine/...           # Package tests
go run ./cmd/ecsmate validate -m examples/webapp  # Manual validation
```

## BUILD & RELEASE

```bash
go build ./cmd/ecsmate                  # Build binary
go test ./...                           # Run tests
```

- GoReleaser for releases (`.goreleaser.yaml`)
- CI: GitHub Actions (`.github/workflows/test.yml`, `release.yml`)
- Multi-platform: Linux/Darwin/Windows (amd64, arm64)

## GOTCHAS

- `ListenerRule` propagation suppressed unless actual config changes OR recreation
- `gradual` deployment uses native ECS rolling (not CodeDeploy)
- Log groups only created with `awslogs` driver AND `createLogGroup: true`
- Orphan detection uses naming pattern `{manifestName}-r{priority}` + `ManagedBy` tag
