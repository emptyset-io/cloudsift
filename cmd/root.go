package cmd

import (
	"runtime"
	"strings"

	initCmd "cloudsift/cmd/init"
	"cloudsift/cmd/list"
	"cloudsift/cmd/scan"
	"cloudsift/cmd/version"
	"cloudsift/internal/config"
	"cloudsift/internal/logging"

	"github.com/spf13/cobra"
)

// Execute adds all child commands to the root command and sets flags appropriately
func Execute() error {
	var configFile string

	rootCmd := &cobra.Command{
		Use:   "cloudsift",
		Short: "CloudSift - AWS resource management tool",
		Long: `CloudSift is a command-line tool for managing and inspecting AWS resources.
It provides a simple interface for common AWS tasks and operations.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Skip config initialization for certain commands
			if cmd.Name() == "version" || cmd.Name() == "help" || cmd.Name() == "completion" {
				return
			}

			// Check if we should enable logging
			shouldLog := false
			if cmd.Parent() != nil && (cmd.Parent().Name() == "scan" || cmd.Parent().Name() == "list") {
				shouldLog = true
			}

			// Initialize config
			if err := config.InitConfig(shouldLog); err != nil {
				return
			}

			// Set config file if specified
			if configFile != "" {
				if err := config.SetConfigFile(configFile); err != nil {
					// Don't use logging here as it might not be initialized yet
					return
				}
			}

			// Only configure logging for scan and list commands
			if shouldLog {
				logFormat := logging.Text
				if config.Config.LogFormat == "json" {
					logFormat = logging.JSON
				}

				// Parse log level
				level := logging.INFO
				switch strings.ToUpper(config.Config.LogLevel) {
				case "DEBUG":
					level = logging.DEBUG
				case "INFO":
					level = logging.INFO
				case "WARN":
					level = logging.WARN
				case "ERROR":
					level = logging.ERROR
				}

				// Initialize logging
				logging.Configure(logging.LogConfig{
					Level:  level,
					Format: logFormat,
				})
			}
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Path to config file")
	rootCmd.PersistentFlags().StringVar(&config.Config.LogFormat, "log-format", "text", "Log output format (text or json)")
	rootCmd.PersistentFlags().StringVar(&config.Config.LogLevel, "log-level", "INFO", "Set logging level (DEBUG, INFO, WARN, ERROR)")
	rootCmd.PersistentFlags().IntVar(&config.Config.MaxWorkers, "max-workers", runtime.NumCPU()*8, "Maximum number of concurrent workers")
	rootCmd.PersistentFlags().StringVarP(&config.Config.Profile, "profile", "p", "default", "AWS profile to use (supports SSO profiles)")
	rootCmd.PersistentFlags().StringVar(&config.Config.OrganizationRole, "organization-role", "", "Role name to assume for organization-wide operations")
	rootCmd.PersistentFlags().StringVar(&config.Config.ScannerRole, "scanner-role", "", "Role name to assume for scanning operations")

	// Add commands
	rootCmd.AddCommand(
		scan.NewScanCmd(),
		list.NewListCmd(),
		version.NewVersionCmd(),
		initCmd.NewInitCmd(),
	)

	return rootCmd.Execute()
}
