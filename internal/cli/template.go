package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/x-qdo/ecsmate/internal/aws"
	"github.com/x-qdo/ecsmate/internal/config"
	"github.com/x-qdo/ecsmate/internal/log"
)

var (
	outputFormat         string
	templateResolveSSM   bool
	newTemplateSSMClient = func(ctx context.Context, region string) (config.SSMResolver, error) {
		return aws.NewSSMClient(ctx, region)
	}
)

var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Render manifest without comparing to remote state",
	Long: `Render the desired state from manifests without connecting to AWS ECS.

Outputs the rendered manifest in YAML or JSON format. SSM parameter
references are preserved by default to avoid printing secrets; use
--resolve-ssm to resolve them explicitly.

This is useful for:
- Debugging manifest configuration
- CI/CD pipelines to validate rendered output
- Generating documentation

Examples:
  # Render manifest as YAML (default)
  ecsmate template -m ./deploy

  # Render as JSON
  ecsmate template -m ./deploy -o json

  # Render with specific values, preserving SSM references
  ecsmate template -m ./deploy -f values/prod.cue

  # Explicitly resolve SSM references before rendering
  ecsmate template -m ./deploy --resolve-ssm`,
	RunE: runTemplate,
}

func init() {
	templateCmd.Flags().StringVarP(&outputFormat, "output", "o", "yaml", "Output format: yaml or json")
	templateCmd.Flags().BoolVar(&templateResolveSSM, "resolve-ssm", false, "Resolve SSM parameter references before rendering (may print secrets)")
}

func runTemplate(cmd *cobra.Command, args []string) error {
	opts := GetGlobalOptions()
	log.Debug("running template", "manifest", opts.ManifestPath, "values", opts.ValueFiles, "output", outputFormat)

	ctx := context.Background()

	// Do not resolve SSM references by default: template output is commonly
	// captured in logs and artifacts, so resolving SecureString values here
	// can disclose secrets. Users who explicitly need resolved output can opt in.
	ssmClient, err := templateSSMResolver(ctx, &opts)
	if err != nil {
		log.Warn("failed to initialize SSM client, SSM references will not be resolved", "error", err)
	}

	manifest, err := loadManifest(ctx, &opts, ssmClient)
	if err != nil {
		printManifestError(err, !opts.NoColor)
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

func templateSSMResolver(ctx context.Context, opts *GlobalOptions) (config.SSMResolver, error) {
	if !templateResolveSSM || opts.NoSSM {
		return nil, nil
	}

	return newTemplateSSMClient(ctx, opts.Region)
}
