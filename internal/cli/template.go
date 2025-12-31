package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/qdo/ecsmate/internal/aws"
	"github.com/qdo/ecsmate/internal/config"
	"github.com/qdo/ecsmate/internal/log"
)

var (
	outputFormat string
)

var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Render manifest without comparing to remote state",
	Long: `Render the desired state from manifests without connecting to AWS ECS.

Outputs the fully resolved manifest including any SSM parameter values
(unless --no-ssm is specified) in YAML or JSON format.

This is useful for:
- Debugging manifest configuration
- CI/CD pipelines to validate rendered output
- Generating documentation

Examples:
  # Render manifest as YAML (default)
  ecsmate template -m ./deploy

  # Render as JSON
  ecsmate template -m ./deploy -o json

  # Render with specific values, without SSM resolution
  ecsmate template -m ./deploy -f values/prod.cue --no-ssm`,
	RunE: runTemplate,
}

func init() {
	templateCmd.Flags().StringVarP(&outputFormat, "output", "o", "yaml", "Output format: yaml or json")
}

func runTemplate(cmd *cobra.Command, args []string) error {
	opts := GetGlobalOptions()
	log.Debug("running template", "manifest", opts.ManifestPath, "values", opts.ValueFiles, "output", outputFormat)

	ctx := context.Background()

	// Initialize SSM client for parameter resolution
	var ssmClient config.SSMResolver
	if !opts.NoSSM {
		client, err := aws.NewSSMClient(ctx, opts.Region)
		if err != nil {
			log.Warn("failed to initialize SSM client, SSM references will not be resolved", "error", err)
		} else {
			ssmClient = client
		}
	}

	manifest, err := loadManifest(ctx, &opts, ssmClient)
	if err != nil {
		log.Error("failed to load manifest", "error", err)
		os.Exit(ExitCodeError)
	}

	var output []byte
	switch outputFormat {
	case "json":
		output, err = json.MarshalIndent(manifest, "", "  ")
	case "yaml":
		output, err = yaml.Marshal(manifest)
	default:
		log.Error("invalid output format", "format", outputFormat)
		os.Exit(ExitCodeError)
	}

	if err != nil {
		log.Error("failed to marshal manifest", "error", err)
		os.Exit(ExitCodeError)
	}

	fmt.Println(string(output))
	return nil
}
