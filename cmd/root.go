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
	var (
		logLevel   string
		configFile string
	)

	// Initialize config
	if err := config.InitConfig(); err != nil {
		return err
	}

	// Create default config if it doesn't exist
	if err := config.CreateDefaultConfig(); err != nil {
		return err
	}

	rootCmd := &cobra.Command{
		Use:   "cloudsift",
		Short: "CloudSift - AWS resource management tool",
		Long: `CloudSift is a command-line tool for managing and inspecting AWS resources.
It provides a simple interface for common AWS tasks and operations.`,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			// Set config file if specified
			if configFile != "" {
				config.SetConfigFile(configFile)
			}

			// Configure logging based on flags
			logFormat := logging.Text

			// Set log format
			if config.Config.LogFormat == "json" {
				logFormat = logging.JSON
			} else {
				// Look for command-specific format flag
				if formatFlag, err := cmd.Flags().GetString("format"); err == nil && formatFlag == "json" {
					logFormat = logging.JSON
				}
			}

			// Set log level
			var level logging.Level
			switch strings.ToUpper(logLevel) {
			case "DEBUG":
				level = logging.DEBUG
			case "INFO":
				level = logging.INFO
			case "WARN":
				level = logging.WARN
			case "ERROR":
				level = logging.ERROR
			default:
				level = logging.INFO
			}

			// Configure logger
			logging.Configure(logging.LogConfig{
				Level:  level,
				Format: logFormat,
			})
		},
	}

	// Add global flags
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Path to config file")
	rootCmd.PersistentFlags().StringVarP(&config.Config.Profile, "profile", "p", "default", "AWS profile to use (supports SSO profiles)")
	rootCmd.PersistentFlags().StringVar(&config.Config.OrganizationRole, "organization-role", "", "Role name to assume for organization-wide operations")
	rootCmd.PersistentFlags().StringVar(&config.Config.ScannerRole, "scanner-role", "", "Role name to assume for scanning operations")
	rootCmd.PersistentFlags().IntVar(&config.Config.MaxWorkers, "max-workers", runtime.NumCPU(), "Maximum number of concurrent workers")
	rootCmd.PersistentFlags().StringVar(&config.Config.LogFormat, "log-format", "text", "Log output format (text or json)")
	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "INFO",
		"Set logging level (DEBUG, INFO, WARN, ERROR)")

	// Add commands
	rootCmd.AddCommand(scan.NewScanCmd())
	rootCmd.AddCommand(list.NewListCmd())
	rootCmd.AddCommand(initCmd.NewInitCmd())
	rootCmd.AddCommand(version.NewVersionCmd())

	return rootCmd.Execute()
}
