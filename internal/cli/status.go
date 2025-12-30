package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	awsclient "github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/log"
)

var (
	statusWatch    bool
	statusInterval int
	statusService  string
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current deployment status",
	Long: `Display the current status of services defined in the manifest.

Shows:
- Task definition versions
- Service desired/running/pending counts
- Deployment status
- Health check status

Use --watch to continuously monitor status.

Examples:
  # Show status for all services in manifest
  ecsmate status -m ./deploy

  # Show status for specific service
  ecsmate status --service web -c my-cluster

  # Watch status continuously
  ecsmate status -m ./deploy --watch`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVarP(&statusWatch, "watch", "w", false, "Watch status continuously")
	statusCmd.Flags().IntVar(&statusInterval, "interval", 5, "Watch interval in seconds")
	statusCmd.Flags().StringVar(&statusService, "service", "", "Show status for specific service")
}

func runStatus(cmd *cobra.Command, args []string) error {
	opts := GetGlobalOptions()
	ctx := context.Background()
	log.Debug("running status", "manifest", opts.ManifestPath, "watch", statusWatch, "service", statusService)

	// Determine services to check
	var serviceNames []string
	var cluster string

	if statusService != "" {
		// Check specific service
		if opts.Cluster == "" {
			return fmt.Errorf("--cluster is required when using --service")
		}
		serviceNames = []string{statusService}
		cluster = opts.Cluster
	} else {
		// Load manifest to get service names (no SSM resolution needed for status)
		manifest, err := loadManifest(ctx, &opts, nil)
		if err != nil {
			return err
		}

		for name, svc := range manifest.Services {
			serviceNames = append(serviceNames, name)
			if cluster == "" {
				cluster = svc.Cluster
			}
		}

		if len(serviceNames) == 0 {
			fmt.Println("No services defined in manifest")
			return nil
		}
	}

	// Create ECS client
	if cluster == "" && opts.Cluster != "" {
		cluster = opts.Cluster
	}
	if cluster == "" {
		return fmt.Errorf("no cluster specified (use --cluster or define in manifest)")
	}

	ecsClient, err := awsclient.NewECSClient(ctx, opts.Region, cluster)
	if err != nil {
		return fmt.Errorf("failed to create ECS client: %w", err)
	}

	if statusWatch {
		return watchStatus(ctx, ecsClient, serviceNames, opts.NoColor)
	}

	return printStatus(ctx, ecsClient, serviceNames, opts.NoColor)
}

func printStatus(ctx context.Context, ecsClient *awsclient.ECSClient, serviceNames []string, noColor bool) error {
	statuses, err := getServiceStatuses(ctx, ecsClient, serviceNames)
	if err != nil {
		return err
	}

	renderStatus(statuses, noColor, false)
	return nil
}

func watchStatus(ctx context.Context, ecsClient *awsclient.ECSClient, serviceNames []string, noColor bool) error {
	ticker := time.NewTicker(time.Duration(statusInterval) * time.Second)
	defer ticker.Stop()

	// Initial render
	statuses, err := getServiceStatuses(ctx, ecsClient, serviceNames)
	if err != nil {
		return err
	}
	renderStatus(statuses, noColor, true)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Clear screen and move cursor to top
			fmt.Print("\033[H\033[2J")

			statuses, err := getServiceStatuses(ctx, ecsClient, serviceNames)
			if err != nil {
				log.Error("failed to get status", "error", err)
				continue
			}
			renderStatus(statuses, noColor, true)
		}
	}
}

func getServiceStatuses(ctx context.Context, ecsClient *awsclient.ECSClient, serviceNames []string) ([]*awsclient.DeploymentStatus, error) {
	var statuses []*awsclient.DeploymentStatus

	for _, name := range serviceNames {
		status, err := ecsClient.GetDeploymentStatus(ctx, name)
		if err != nil {
			log.Warn("failed to get status for service", "service", name, "error", err)
			continue
		}
		statuses = append(statuses, status)
	}

	return statuses, nil
}

func renderStatus(statuses []*awsclient.DeploymentStatus, noColor bool, watchMode bool) {
	cyan := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	bold := color.New(color.Bold).SprintFunc()
	if noColor {
		cyan = fmt.Sprint
		green = fmt.Sprint
		yellow = fmt.Sprint
		red = fmt.Sprint
		bold = fmt.Sprint
	}

	fmt.Println()
	fmt.Printf("%s\n\n", bold("Service Status"))

	if watchMode {
		fmt.Printf("  %s (updating every %ds, Ctrl+C to exit)\n\n",
			yellow("WATCHING"), statusInterval)
	}

	for _, status := range statuses {
		// Service name and status
		statusColor := green
		statusSymbol := "✓"
		if status.Status != "ACTIVE" {
			statusColor = red
			statusSymbol = "✗"
		}

		fmt.Printf("  %s %s %s\n",
			statusColor(statusSymbol),
			cyan(status.Service),
			statusColor(status.Status))

		// Task counts
		fmt.Printf("    Tasks: %s desired, %s running, %s pending\n",
			bold(fmt.Sprintf("%d", status.DesiredCount)),
			green(fmt.Sprintf("%d", status.RunningCount)),
			yellow(fmt.Sprintf("%d", status.PendingCount)))

		// Progress bar
		total := status.DesiredCount
		if total > 0 {
			progress := float64(status.RunningCount) / float64(total)
			bar := renderProgressBar(progress, 30)
			pct := int(progress * 100)
			pctColor := green
			if pct < 100 {
				pctColor = yellow
			}
			fmt.Printf("    Progress: %s %s\n", bar, pctColor(fmt.Sprintf("%d%%", pct)))
		}

		// Task definition
		fmt.Printf("    Task Definition: %s\n", status.TaskDefinition)

		// Rollout state
		if status.RolloutState != "" {
			rolloutColor := yellow
			if status.RolloutState == "COMPLETED" {
				rolloutColor = green
			} else if status.RolloutState == "FAILED" {
				rolloutColor = red
			}
			fmt.Printf("    Rollout: %s\n", rolloutColor(status.RolloutState))
		}

		fmt.Println()
	}

	if len(statuses) == 0 {
		fmt.Println("  No services found")
		fmt.Println()
	}
}

func renderProgressBar(progress float64, width int) string {
	filled := int(progress * float64(width))
	empty := width - filled

	bar := ""
	for i := 0; i < filled; i++ {
		bar += "█"
	}
	for i := 0; i < empty; i++ {
		bar += "░"
	}

	return bar
}
