package cmd

import (
	"strings"

	initCmd "cloudsift/cmd/init"
	"cloudsift/cmd/list"
	"cloudsift/cmd/scan"
	"cloudsift/cmd/version"
	"cloudsift/internal/config"
	"cloudsift/internal/logging"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// Execute adds all child commands to the root command and sets flags appropriately
func Execute() error {
	var configFile string

	rootCmd := &cobra.Command{
		Use:   "cloudsift",
		Short: "CloudSift - AWS resource management tool",
		Long: `CloudSift is a command-line tool for managing and inspecting AWS resources.
It provides a simple interface for common AWS tasks and operations.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Skip config initialization for certain commands
			if cmd.Name() == "version" || cmd.Name() == "help" || cmd.Name() == "completion" {
				// Reset config to empty values for these commands
				config.Config = &config.GlobalConfig{}
				return nil
			}

			// First bind global flags to viper
			if err := viper.BindPFlag("aws.profile", cmd.Root().PersistentFlags().Lookup("profile")); err != nil {
				return err
			}
			if err := viper.BindPFlag("aws.organization_role", cmd.Root().PersistentFlags().Lookup("organization-role")); err != nil {
				return err
			}
			if err := viper.BindPFlag("aws.scanner_role", cmd.Root().PersistentFlags().Lookup("scanner-role")); err != nil {
				return err
			}
			if err := viper.BindPFlag("app.max_workers", cmd.Root().PersistentFlags().Lookup("max-workers")); err != nil {
				return err
			}
			if err := viper.BindPFlag("app.log_format", cmd.Root().PersistentFlags().Lookup("log-format")); err != nil {
				return err
			}
			if err := viper.BindPFlag("app.log_level", cmd.Root().PersistentFlags().Lookup("log-level")); err != nil {
				return err
			}

			// Set config file if specified
			if configFile != "" {
				if err := config.SetConfigFile(configFile); err != nil {
					return err
				}
			}

			// Initialize config to read from file
			if err := config.InitConfig(false, cmd); err != nil {
				return err
			}

			// Check if we should enable logging
			shouldLog := false
			if cmd.Name() == "scan" || cmd.Name() == "list" || (cmd.Parent() != nil && (cmd.Parent().Name() == "scan" || cmd.Parent().Name() == "list")) {
				shouldLog = true
			}

			// Update config struct with values from viper
			config.Config.Profile = viper.GetString("aws.profile")
			config.Config.OrganizationRole = viper.GetString("aws.organization_role")
			config.Config.ScannerRole = viper.GetString("aws.scanner_role")
			config.Config.MaxWorkers = viper.GetInt("app.max_workers")
			config.Config.LogFormat = viper.GetString("app.log_format")
			config.Config.LogLevel = viper.GetString("app.log_level")

			// Log configuration sources if logging is enabled
			if shouldLog {
				config.LogConfigurationSources(shouldLog, cmd)
			}

			// Configure logging for scan and list commands
			if shouldLog {
				logFormat := logging.Text
				if config.Config.LogFormat == "json" {
					logFormat = logging.JSON
				}

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

				// Configure logging with settings
				logging.Configure(logging.LogConfig{
					Level:  level,
					Format: logFormat,
				})
			}

			return nil
		},
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "Path to config file")
	rootCmd.PersistentFlags().StringVar(&config.Config.LogFormat, "log-format", "text", "Log output format (text or json)")
	rootCmd.PersistentFlags().StringVar(&config.Config.LogLevel, "log-level", "INFO", "Set logging level (DEBUG, INFO, WARN, ERROR)")
	rootCmd.PersistentFlags().IntVar(&config.Config.MaxWorkers, "max-workers", 8, "Maximum number of concurrent workers")
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
