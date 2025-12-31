package cli

import (
	"github.com/spf13/cobra"

	"github.com/qdo/ecsmate/internal/log"
)

var (
	manifestPath string
	valueFiles   []string
	setValues    []string
	cluster      string
	region       string
	logLevel     string
	noColor      bool
	noSSM        bool
)

var rootCmd = &cobra.Command{
	Use:   "ecsmate",
	Short: "ECS deployment management CLI",
	Long: `ecsmate is a self-sufficient CLI for managing ECS deployments from CI/CD pipelines.

It uses a declarative approach with CUE templating to define task definitions,
services, and scheduled tasks. It can diff desired vs current state and apply
changes to achieve the desired state.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		log.Init(log.ParseLevel(logLevel), nil)
	},
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&manifestPath, "manifest", "m", ".", "Path to manifest directory")
	rootCmd.PersistentFlags().StringArrayVarP(&valueFiles, "values", "f", nil, "Values files (can be repeated, merged in order)")
	rootCmd.PersistentFlags().StringArrayVar(&setValues, "set", nil, "Override values (key=value)")
	rootCmd.PersistentFlags().StringVarP(&cluster, "cluster", "c", "", "Target ECS cluster")
	rootCmd.PersistentFlags().StringVarP(&region, "region", "r", "", "AWS region")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "Log level (debug|info|warn|error)")
	rootCmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	rootCmd.PersistentFlags().BoolVar(&noSSM, "no-ssm", false, "Disable SSM parameter resolution")

	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(applyCmd)
	rootCmd.AddCommand(rollbackCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(templateCmd)
}

func Execute() error {
	return rootCmd.Execute()
}

type GlobalOptions struct {
	ManifestPath string
	ValueFiles   []string
	SetValues    []string
	Cluster      string
	Region       string
	NoColor      bool
	NoSSM        bool
}

func GetGlobalOptions() GlobalOptions {
	return GlobalOptions{
		ManifestPath: manifestPath,
		ValueFiles:   valueFiles,
		SetValues:    setValues,
		Cluster:      cluster,
		Region:       region,
		NoColor:      noColor,
		NoSSM:        noSSM,
	}
}
