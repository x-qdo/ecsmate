package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/diff"
	"github.com/qdo/ecsmate/internal/engine"
	"github.com/qdo/ecsmate/internal/log"
	"github.com/qdo/ecsmate/internal/resources"
)

const (
	ExitCodeSuccess       = 0
	ExitCodeError         = 1
	ExitCodeDiffDetected  = 2
	ExitCodeRolloutFailed = 3
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show diff between desired and current state",
	Long: `Compare the desired state defined in manifests with the current state in ECS.

Outputs a colored unified diff showing what would change if apply is run.

Exit codes:
  0 - No changes detected
  1 - Error occurred
  2 - Changes detected (useful for CI)`,
	RunE: runDiff,
}

func runDiff(cmd *cobra.Command, args []string) error {
	opts := GetGlobalOptions()
	log.Debug("running diff", "manifest", opts.ManifestPath, "values", opts.ValueFiles)

	ctx := context.Background()

	// Initialize SSM client for parameter resolution
	var ssmClient *aws.SSMClient
	if !opts.NoSSM {
		var err error
		ssmClient, err = aws.NewSSMClient(ctx, opts.Region)
		if err != nil {
			log.Warn("failed to initialize SSM client, SSM references will not be resolved", "error", err)
		}
	}

	manifest, err := loadManifest(ctx, &opts, ssmClient)
	if err != nil {
		log.Error("failed to load manifest", "error", err)
		os.Exit(ExitCodeError)
	}

	clients, err := initAWSClients(ctx, &opts, manifest)
	if err != nil {
		log.Error("failed to initialize AWS clients", "error", err)
		os.Exit(ExitCodeError)
	}

	// Get EventBridge scheduler role ARN if needed
	var schedulerRoleArn string
	if clients.IAM != nil && len(manifest.ScheduledTasks) > 0 {
		schedulerRoleArn, err = clients.IAM.GetOrCreateEventBridgeRole(ctx)
		if err != nil {
			log.Warn("failed to get/create EventBridge role", "error", err)
		}
	}

	builder := resources.NewResourceBuilderWithConfig(resources.ResourceBuilderConfig{
		ECSClient:          clients.ECS,
		SchedulerClient:    clients.Scheduler,
		AutoScalingClient:  clients.AutoScaling,
		ELBV2Client:        clients.ELBV2,
		SchedulerGroupName: manifest.Name,
	})
	state, err := builder.BuildDesiredState(ctx, manifest, schedulerRoleArn)
	if err != nil {
		log.Error("failed to build desired state", "error", err)
		os.Exit(ExitCodeError)
	}

	planner := engine.NewPlanner()
	plan := planner.GeneratePlan(state)

	renderer := diff.NewRenderer(os.Stdout, opts.NoColor)
	renderer.RenderHeader(manifest.Name)
	renderer.RenderDiff(plan.Entries)
	renderer.RenderSummary(plan.Summary, manifest.Name)

	if plan.HasChanges() {
		os.Exit(ExitCodeDiffDetected)
	}

	return nil
}

func loadManifest(ctx context.Context, opts *GlobalOptions, ssmResolver config.SSMResolver) (*config.Manifest, error) {
	loader := config.NewCUELoader()

	evaluated, err := loader.LoadManifest(opts.ManifestPath, opts.ValueFiles, opts.SetValues)
	if err != nil {
		return nil, err
	}

	// Extract manifest field (supports both root-level and nested manifest)
	manifestValue, err := loader.GetManifest(evaluated)
	if err != nil {
		return nil, err
	}

	manifest, err := config.ParseManifest(manifestValue)
	if err != nil {
		return nil, err
	}

	// Resolve SSM references if resolver is provided
	if ssmResolver != nil {
		if err := config.ResolveSSMReferences(ctx, manifest, ssmResolver); err != nil {
			return nil, fmt.Errorf("failed to resolve SSM references: %w", err)
		}
	}

	return manifest, nil
}

type AWSClients struct {
	ECS         *aws.ECSClient
	Scheduler   *aws.SchedulerClient
	AutoScaling *aws.AutoScalingClient
	IAM         *aws.IAMClient
	ELBV2       *aws.ELBV2Client
}

func initAWSClients(ctx context.Context, opts *GlobalOptions, manifest *config.Manifest) (*AWSClients, error) {
	clients := &AWSClients{}

	cluster := opts.Cluster
	if cluster == "" {
		for _, svc := range manifest.Services {
			if svc.Cluster != "" {
				cluster = svc.Cluster
				break
			}
		}
	}

	var err error
	clients.ECS, err = aws.NewECSClient(ctx, opts.Region, cluster)
	if err != nil {
		return nil, err
	}

	// Initialize Scheduler client if there are scheduled tasks
	if len(manifest.ScheduledTasks) > 0 {
		clients.Scheduler, err = aws.NewSchedulerClient(ctx, opts.Region)
		if err != nil {
			log.Warn("failed to initialize scheduler client", "error", err)
		}

		// Initialize IAM client for EventBridge role
		clients.IAM, err = aws.NewIAMClient(ctx, opts.Region)
		if err != nil {
			log.Warn("failed to initialize IAM client", "error", err)
		}
	}

	// Initialize AutoScaling client if any service has auto-scaling config
	for _, svc := range manifest.Services {
		if svc.AutoScaling != nil {
			clients.AutoScaling, err = aws.NewAutoScalingClient(ctx, opts.Region)
			if err != nil {
				log.Warn("failed to initialize auto-scaling client", "error", err)
			}
			break
		}
	}

	// Initialize ELBV2 client if there's an ingress configuration
	if manifest.Ingress != nil {
		clients.ELBV2, err = aws.NewELBV2Client(ctx, opts.Region)
		if err != nil {
			log.Warn("failed to initialize ELBV2 client", "error", err)
		}
	}

	return clients, nil
}
