package cli

import (
	"fmt"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate manifest syntax",
	Long: `Validate the CUE manifest syntax and schema without connecting to AWS.

Checks:
- CUE syntax is valid
- Schema constraints are satisfied
- Values can be merged
- All required fields are present

Examples:
  # Validate default manifest
  ecsmate validate -m ./deploy

  # Validate with specific values
  ecsmate validate -m ./deploy -f values/prod.cue`,
	RunE: runValidate,
}

func runValidate(cmd *cobra.Command, args []string) error {
	opts := GetGlobalOptions()
	log.Debug("running validate", "manifest", opts.ManifestPath, "values", opts.ValueFiles)

	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	if opts.NoColor {
		green = fmt.Sprint
		red = fmt.Sprint
		cyan = fmt.Sprint
	}

	fmt.Println()
	fmt.Printf("Validating manifest: %s\n\n", cyan(opts.ManifestPath))

	// Step 1: Load CUE files
	fmt.Print("  Loading CUE files... ")
	loader := config.NewCUELoader()
	value, err := loader.LoadManifest(opts.ManifestPath, opts.ValueFiles, opts.SetValues)
	if err != nil {
		fmt.Printf("%s\n", red("FAILED"))
		fmt.Printf("\n  Error: %v\n\n", err)
		os.Exit(ExitCodeError)
	}
	fmt.Printf("%s\n", green("OK"))

	// Step 2: Validate CUE schema
	fmt.Print("  Validating schema... ")
	if err := value.Validate(); err != nil {
		fmt.Printf("%s\n", red("FAILED"))
		fmt.Printf("\n  Error: %v\n\n", err)
		os.Exit(ExitCodeError)
	}
	fmt.Printf("%s\n", green("OK"))

	// Step 3: Parse manifest
	fmt.Print("  Parsing manifest... ")
	manifest, err := config.ParseManifest(value)
	if err != nil {
		fmt.Printf("%s\n", red("FAILED"))
		fmt.Printf("\n  Error: %v\n\n", err)
		os.Exit(ExitCodeError)
	}
	fmt.Printf("%s\n", green("OK"))

	// Step 4: Validate manifest content
	fmt.Print("  Validating content... ")
	validationErrors := validateManifestContent(manifest)
	if len(validationErrors) > 0 {
		fmt.Printf("%s\n", red("FAILED"))
		fmt.Printf("\n  Errors:\n")
		for _, e := range validationErrors {
			fmt.Printf("    - %s\n", e)
		}
		fmt.Println()
		os.Exit(ExitCodeError)
	}
	fmt.Printf("%s\n", green("OK"))

	// Summary
	fmt.Println()
	fmt.Printf("%s Manifest is valid\n\n", green("✓"))
	fmt.Printf("  Name: %s\n", manifest.Name)
	fmt.Printf("  Task Definitions: %d\n", len(manifest.TaskDefinitions))
	fmt.Printf("  Services: %d\n", len(manifest.Services))
	fmt.Printf("  Scheduled Tasks: %d\n", len(manifest.ScheduledTasks))
	fmt.Println()

	return nil
}

// validateManifestContent performs semantic validation on the parsed manifest
func validateManifestContent(manifest *config.Manifest) []string {
	var errors []string

	// Validate task definitions
	for name, td := range manifest.TaskDefinitions {
		switch td.Type {
		case "managed":
			if td.Family == "" {
				errors = append(errors, fmt.Sprintf("task definition '%s': family is required for managed type", name))
			}
			if len(td.ContainerDefinitions) == 0 {
				errors = append(errors, fmt.Sprintf("task definition '%s': at least one container definition is required", name))
			}
			for i, cd := range td.ContainerDefinitions {
				if cd.Name == "" {
					errors = append(errors, fmt.Sprintf("task definition '%s': container %d: name is required", name, i))
				}
				if cd.Image == "" {
					errors = append(errors, fmt.Sprintf("task definition '%s': container '%s': image is required", name, cd.Name))
				}
			}

		case "merged":
			if td.BaseArn == "" {
				errors = append(errors, fmt.Sprintf("task definition '%s': baseArn is required for merged type", name))
			}

		case "remote":
			if td.Arn == "" {
				errors = append(errors, fmt.Sprintf("task definition '%s': arn is required for remote type", name))
			}

		case "":
			errors = append(errors, fmt.Sprintf("task definition '%s': type is required", name))

		default:
			errors = append(errors, fmt.Sprintf("task definition '%s': invalid type '%s' (must be managed, merged, or remote)", name, td.Type))
		}
	}

	// Validate services
	for name, svc := range manifest.Services {
		if svc.Cluster == "" {
			errors = append(errors, fmt.Sprintf("service '%s': cluster is required", name))
		}
		if svc.TaskDefinition == "" {
			errors = append(errors, fmt.Sprintf("service '%s': taskDefinition is required", name))
		} else {
			// Check task definition reference exists
			if _, exists := manifest.TaskDefinitions[svc.TaskDefinition]; !exists {
				errors = append(errors, fmt.Sprintf("service '%s': references unknown task definition '%s'", name, svc.TaskDefinition))
			}
		}

		// Validate dependencies
		for _, dep := range svc.DependsOn {
			if _, exists := manifest.Services[dep]; !exists {
				errors = append(errors, fmt.Sprintf("service '%s': depends on unknown service '%s'", name, dep))
			}
		}
	}

	// Validate scheduled tasks
	for name, st := range manifest.ScheduledTasks {
		if st.Cluster == "" {
			errors = append(errors, fmt.Sprintf("scheduled task '%s': cluster is required", name))
		}
		if st.TaskDefinition == "" {
			errors = append(errors, fmt.Sprintf("scheduled task '%s': taskDefinition is required", name))
		} else {
			// Check task definition reference exists
			if _, exists := manifest.TaskDefinitions[st.TaskDefinition]; !exists {
				errors = append(errors, fmt.Sprintf("scheduled task '%s': references unknown task definition '%s'", name, st.TaskDefinition))
			}
		}
		if st.ScheduleExpression == "" {
			errors = append(errors, fmt.Sprintf("scheduled task '%s': schedule.expression is required", name))
		}
	}

	return errors
}
