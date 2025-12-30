package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/engine"
	"github.com/qdo/ecsmate/internal/log"
)

var (
	rollbackService   string
	rollbackRevision  int
	rollbackListLimit int
	rollbackList      bool
	rollbackNoWait    bool
)

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Rollback to previous revision",
	Long: `Rollback a service to a previous task definition revision.

Uses ECS's built-in revision history. Specify a negative number for relative
rollback (e.g., -1 for previous revision) or a positive number for absolute
revision.

Examples:
  # Rollback to previous revision
  ecsmate rollback --service web --revision -1

  # Rollback to specific revision
  ecsmate rollback --service web --revision 42

  # List available revisions
  ecsmate rollback --service web --list`,
	RunE: runRollback,
}

func init() {
	rollbackCmd.Flags().StringVar(&rollbackService, "service", "", "Service name to rollback")
	rollbackCmd.Flags().IntVar(&rollbackRevision, "revision", -1, "Revision to rollback to (-1 for previous)")
	rollbackCmd.Flags().BoolVar(&rollbackList, "list", false, "List available revisions")
	rollbackCmd.Flags().IntVar(&rollbackListLimit, "limit", 10, "Number of revisions to list")
	rollbackCmd.Flags().BoolVar(&rollbackNoWait, "no-wait", false, "Don't wait for deployment to complete")
	rollbackCmd.MarkFlagRequired("service")
}

func runRollback(cmd *cobra.Command, args []string) error {
	opts := GetGlobalOptions()
	ctx := context.Background()
	log.Debug("running rollback", "service", rollbackService, "revision", rollbackRevision, "cluster", opts.Cluster)

	if opts.Cluster == "" {
		return fmt.Errorf("--cluster is required for rollback")
	}

	// Create ECS client
	ecsClient, err := awsclient.NewECSClient(ctx, opts.Region, opts.Cluster)
	if err != nil {
		return fmt.Errorf("failed to create ECS client: %w", err)
	}

	manager := engine.NewRollbackManager(ecsClient)

	// Handle --list flag
	if rollbackList {
		return listRevisions(ctx, manager, rollbackService, rollbackListLimit, opts.NoColor)
	}

	// Perform rollback
	result, err := manager.Rollback(ctx, rollbackService, rollbackRevision, !rollbackNoWait)
	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	// Print result
	printRollbackResult(result, opts.NoColor)

	if !result.Success {
		os.Exit(ExitCodeRolloutFailed)
	}

	return nil
}

func listRevisions(ctx context.Context, manager *engine.RollbackManager, serviceName string, limit int, noColor bool) error {
	revisions, err := manager.ListRevisions(ctx, serviceName, limit)
	if err != nil {
		return err
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	if noColor {
		cyan = fmt.Sprint
		green = fmt.Sprint
	}

	fmt.Printf("\nAvailable revisions for service %s:\n\n", cyan(serviceName))

	for _, rev := range revisions {
		marker := "  "
		suffix := ""
		if rev.IsCurrent {
			marker = green("→ ")
			suffix = green(" (current)")
		}

		fmt.Printf("%s%-60s  revision %d%s\n", marker, rev.Arn, rev.Revision, suffix)
	}

	fmt.Println()
	return nil
}

func printRollbackResult(result *engine.RollbackResult, noColor bool) {
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	if noColor {
		green = fmt.Sprint
		red = fmt.Sprint
		cyan = fmt.Sprint
	}

	fmt.Println()

	if result.Success {
		fmt.Printf("%s Rollback successful\n\n", green("✓"))
	} else {
		fmt.Printf("%s Rollback failed\n\n", red("✗"))
	}

	fmt.Printf("  Service:    %s\n", cyan(result.ServiceName))
	fmt.Printf("  From:       %s\n", result.PreviousTaskDef)
	fmt.Printf("  To:         %s\n", result.TargetTaskDef)
	fmt.Printf("  Revision:   %d\n", result.TargetRevision)
	fmt.Printf("  Message:    %s\n", result.Message)

	fmt.Println()
}
