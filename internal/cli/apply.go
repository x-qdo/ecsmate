package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/config"
	"github.com/x-qdo/ecsmate/internal/diff"
	"github.com/x-qdo/ecsmate/internal/engine"
	"github.com/x-qdo/ecsmate/internal/log"
	"github.com/x-qdo/ecsmate/internal/resources"
)

var (
	applyAutoApprove bool
	applyNoWait      bool
	applyTimeout     time.Duration
	applyLogLines    int
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply changes to achieve desired state",
	Long: `Apply the changes needed to bring ECS to the desired state defined in manifests.

This will:
1. Register new task definition revisions if changed
2. Update services with new task definitions
3. Wait for deployments to complete
4. Track progress with live output

Exit codes:
  0 - All changes applied successfully
  1 - Error occurred
  3 - Rollout failed`,
	RunE: runApply,
}

func init() {
	applyCmd.Flags().BoolVar(&applyAutoApprove, "auto-approve", false, "Skip interactive approval")
	applyCmd.Flags().BoolVar(&applyNoWait, "no-wait", false, "Don't wait for deployments to complete")
	applyCmd.Flags().DurationVar(&applyTimeout, "timeout", 15*time.Minute, "Timeout for waiting on deployments")
	applyCmd.Flags().IntVar(&applyLogLines, "log-lines", -1, "Max log lines to show on task failure (-1=all, 0=none)")
}

func runApply(cmd *cobra.Command, args []string) error {
	opts := GetGlobalOptions()
	log.Debug("running apply", "manifest", opts.ManifestPath, "values", opts.ValueFiles, "auto-approve", applyAutoApprove)

	ctx := context.Background()

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
		printManifestError(err, !opts.NoColor)
		os.Exit(ExitCodeError)
	}

	var managedSecrets *config.ManagedSecrets
	if manifest.Secrets != nil && manifest.Secrets.Managed != nil {
		managedSecrets, err = config.LoadManagedSecrets(ctx, opts.ManifestPath, manifest.Secrets.Managed, opts.Region)
		if err != nil {
			log.Error("failed to load managed secrets", "error", err)
			os.Exit(ExitCodeError)
		}
		manifest.ResolveManagedSecrets(managedSecrets)

		// Validate that all secret references resolved to valid ARNs
		if secretErrors := manifest.ValidateSecretReferences(); len(secretErrors) > 0 {
			for _, e := range secretErrors {
				log.Error("invalid secret reference", "error", e)
			}
			os.Exit(ExitCodeError)
		}
	}

	clients, err := initAWSClients(ctx, &opts, manifest)
	if err != nil {
		log.Error("failed to initialize AWS clients", "error", err)
		os.Exit(ExitCodeError)
	}

	// Get or create EventBridge scheduler role if needed
	var schedulerRoleArn string
	if clients.IAM != nil && len(manifest.ScheduledTasks) > 0 {
		schedulerRoleArn, err = clients.IAM.GetOrCreateEventBridgeRole(ctx)
		if err != nil {
			log.Warn("failed to get/create EventBridge role", "error", err)
		}
	}

	builder := resources.NewResourceBuilderWithConfig(resources.ResourceBuilderConfig{
		ECSClient:              clients.ECS,
		SchedulerClient:        clients.Scheduler,
		AutoScalingClient:      clients.AutoScaling,
		ELBV2Client:            clients.ELBV2,
		ServiceDiscoveryClient: clients.ServiceDiscovery,
		SchedulerGroupName:     manifest.Name,
	})
	state, err := builder.BuildDesiredState(ctx, manifest, schedulerRoleArn)
	if err != nil {
		log.Error("failed to build desired state", "error", err)
		os.Exit(ExitCodeError)
	}

	planner := engine.NewPlanner()
	plan := planner.GeneratePlan(state)

	var ssmParamsManager *resources.SSMParamsManager
	var ssmChanges []resources.SSMParamChange
	if managedSecrets != nil && ssmClient != nil {
		ssmParamsManager = resources.NewSSMParamsManager(ssmClient.Client(), managedSecrets)
		if ssmParamsManager != nil {
			var err error
			ssmChanges, err = ssmParamsManager.Diff(ctx)
			if err != nil {
				log.Error("failed to diff SSM parameters", "error", err)
				os.Exit(ExitCodeError)
			}
		}
	}

	hasSSMChanges := len(ssmChanges) > 0
	if !plan.HasChanges() && !hasSSMChanges {
		fmt.Println("No changes to apply.")
		return nil
	}

	renderer := diff.NewRenderer(os.Stdout, opts.NoColor)
	renderer.RenderHeader(manifest.Name)

	if hasSSMChanges {
		var diffChanges []diff.SSMParamChange
		for _, c := range ssmChanges {
			diffChanges = append(diffChanges, diff.SSMParamChange{
				Name:    c.Name,
				Action:  c.Action,
				OldHash: c.OldHash,
				NewHash: c.NewHash,
			})
		}
		renderer.RenderSSMParamChanges(diffChanges)
	}

	renderer.RenderDiff(plan.Entries)
	renderer.RenderSummary(plan.Summary, manifest.Name)

	if !applyAutoApprove {
		if !confirmApply() {
			fmt.Println("Apply cancelled.")
			return nil
		}
	}

	execPlan, err := engine.BuildExecutionPlan(state)
	if err != nil {
		log.Error("failed to build execution plan", "error", err)
		os.Exit(ExitCodeError)
	}

	cluster := opts.Cluster
	if cluster == "" {
		for _, svc := range manifest.Services {
			if svc.Cluster != "" {
				cluster = svc.Cluster
				break
			}
		}
	}

	executor := engine.NewExecutor(engine.ExecutorConfig{
		ECSClient:              clients.ECS,
		SchedulerClient:        clients.Scheduler,
		CloudWatchClient:       clients.CloudWatch,
		ELBV2Client:            clients.ELBV2,
		ServiceDiscoveryClient: clients.ServiceDiscovery,
		TaskDefManager:         builder.TaskDefManager(),
		ServiceManager:         builder.ServiceManager(),
		ScheduledManager:       builder.ScheduledTaskManager(),
		SSMParamsManager:       ssmParamsManager,
		Output:                 os.Stdout,
		NoColor:                opts.NoColor,
		NoWait:                 applyNoWait,
		Timeout:                applyTimeout,
		LogLines:               applyLogLines,
	})

	if err := executor.Execute(ctx, execPlan, cluster); err != nil {
		log.Error("deployment failed", "error", err)
		os.Exit(ExitCodeRolloutFailed)
	}

	return nil
}

func confirmApply() bool {
	fmt.Print("\nDo you want to apply these changes? (yes/no): ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "yes" || response == "y"
}
