package cli

import (
	"context"
	"fmt"
	"os"
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
	statusEvents   int
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
	statusCmd.Flags().IntVar(&statusEvents, "events", 5, "Number of recent events to show per service")
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
			log.Error("--cluster is required when using --service")
			os.Exit(ExitCodeError)
		}
		serviceNames = []string{statusService}
		cluster = opts.Cluster
	} else {
		// Initialize SSM client for parameter resolution (cluster name comes from SSM)
		var ssmClient *awsclient.SSMClient
		if !opts.NoSSM {
			var err error
			ssmClient, err = awsclient.NewSSMClient(ctx, opts.Region)
			if err != nil {
				log.Warn("failed to initialize SSM client, SSM references will not be resolved", "error", err)
			}
		}

		manifest, err := loadManifest(ctx, &opts, ssmClient)
		if err != nil {
			log.Error("failed to load manifest", "error", err)
			os.Exit(ExitCodeError)
		}

		for name, svc := range manifest.Services {
			// Build full service name with namespace prefix (e.g., "cal-web")
			fullName := name
			if manifest.Name != "" {
				fullName = manifest.Name + "-" + name
			}
			serviceNames = append(serviceNames, fullName)
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
		log.Error("no cluster specified (use --cluster or define in manifest)")
		os.Exit(ExitCodeError)
	}

	ecsClient, err := awsclient.NewECSClient(ctx, opts.Region, cluster)
	if err != nil {
		log.Error("failed to create ECS client", "error", err)
		os.Exit(ExitCodeError)
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

	// Track seen event IDs to detect new events
	seenEvents := make(map[string]bool)

	// Color functions for new events
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()
	if noColor {
		cyan = fmt.Sprint
		yellow = fmt.Sprint
	}

	// Initial render
	statuses, err := getServiceStatuses(ctx, ecsClient, serviceNames)
	if err != nil {
		return err
	}

	// Mark all current events as seen
	for _, status := range statuses {
		for _, event := range status.Events {
			seenEvents[event.ID] = true
		}
	}

	renderStatus(statuses, noColor, true)
	fmt.Println("  ─────────────────────────────────────────")
	fmt.Println("  New events will appear below:")
	fmt.Println()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			statuses, err := getServiceStatuses(ctx, ecsClient, serviceNames)
			if err != nil {
				log.Error("failed to get status", "error", err)
				continue
			}

			// Print any new events
			for _, status := range statuses {
				for i := len(status.Events) - 1; i >= 0; i-- {
					event := status.Events[i]
					if !seenEvents[event.ID] {
						seenEvents[event.ID] = true
						timestamp := event.CreatedAt.Local().Format("15:04:05")
						fmt.Printf("  %s %s %s\n", cyan(timestamp), yellow(status.Service), event.Message)
					}
				}
			}
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

		// Recent events
		if len(status.Events) > 0 && statusEvents > 0 {
			fmt.Printf("    %s\n", bold("Recent Events:"))
			count := statusEvents
			if count > len(status.Events) {
				count = len(status.Events)
			}
			for i := 0; i < count; i++ {
				event := status.Events[i]
				timestamp := event.CreatedAt.Local().Format("15:04:05")
				fmt.Printf("      %s %s\n", cyan(timestamp), event.Message)
			}
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
