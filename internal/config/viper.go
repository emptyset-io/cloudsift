package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cloudsift/internal/logging"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// convertSliceToString converts a slice of strings to a comma-separated string
func convertSliceToString(slice []string) string {
	if len(slice) == 0 {
		return ""
	}
	return strings.Join(slice, ",")
}

// parameterSource tracks where each parameter value came from
type parameterSource struct {
	Key    string
	Value  interface{}
	Source string
}

// getParameterSource determines where a parameter value came from (config file, env var, flag, or default)
func getParameterSource(key string, cmd *cobra.Command) parameterSource {
	flagValue := viper.Get(key)
	envKey := "CLOUDSIFT_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))

	// Map config keys to flag names
	flagNames := map[string]string{
		"aws.profile":          "profile",
		"aws.organization_role": "organization-role",
		"aws.scanner_role":     "scanner-role",
		"app.max_workers":      "max-workers",
		"app.log_format":       "log-format",
		"app.log_level":        "log-level",
		"scan.regions":         "regions",
		"scan.scanners":        "scanners",
		"scan.output":          "output",
		"scan.output_format":   "output-format",
		"scan.bucket":          "bucket",
		"scan.bucket_region":   "bucket-region",
		"scan.days_unused":     "days-unused",
	}

	// Get the flag name from the map, or convert the key if not found
	flagName := flagNames[key]
	if flagName == "" {
		// Fall back to converting the key if not in the map
		flagName = strings.Replace(key, ".", "-", -1)
	}

	// Check if flag was set on command line - check both local and persistent flags
	if cmd != nil {
		// Check local flags first
		if f := cmd.Flags().Lookup(flagName); f != nil && f.Changed {
			return parameterSource{key, flagValue, "command line flag"}
		}

		// Walk up the command chain checking persistent flags
		current := cmd
		for current != nil {
			if f := current.PersistentFlags().Lookup(flagName); f != nil && f.Changed {
				return parameterSource{key, flagValue, "command line flag"}
			}
			current = current.Parent()
		}
	}

	// Check if value is set by environment variable
	if _, exists := os.LookupEnv(envKey); exists {
		return parameterSource{key, flagValue, "environment variable"}
	}

	// Check if value is set in config file
	if viper.GetViper().InConfig(key) {
		return parameterSource{key, flagValue, "config file"}
	}

	// Value is using default
	return parameterSource{key, flagValue, "default value"}
}

// LogConfigurationSources logs the source of each configuration parameter
func LogConfigurationSources(shouldLog bool, cmd *cobra.Command) {
	if !shouldLog {
		return
	}

	logging.Debug("Configuration parameter sources:", nil)
	
	// List of all configuration parameters to check
	params := []string{
		"aws.profile",
		"aws.organization_role",
		"aws.scanner_role",
		"app.max_workers",
		"app.log_format",
		"app.log_level",
		"scan.regions",
		"scan.scanners",
		"scan.output",
		"scan.output_format",
		"scan.bucket",
		"scan.bucket_region",
		"scan.days_unused",
	}

	// Log the source of each parameter
	for _, param := range params {
		source := getParameterSource(param, cmd)
		logging.Debug(fmt.Sprintf("  %s = %v (from %s)", source.Key, source.Value, source.Source), nil)
	}
}

// InitConfig initializes the Viper configuration
func InitConfig(shouldLog bool, cmd *cobra.Command) error {
	// Set config name and type
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	// Add config search paths
	viper.AddConfigPath(".") // Current directory only

	// Set environment variable prefix
	viper.SetEnvPrefix("CLOUDSIFT")
	viper.AutomaticEnv()
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	// Set defaults for all configuration values
	viper.SetDefault("aws.profile", "default")
	viper.SetDefault("aws.organization_role", "")
	viper.SetDefault("aws.scanner_role", "")
	viper.SetDefault("app.max_workers", 8)
	viper.SetDefault("app.log_format", "text")
	viper.SetDefault("app.log_level", "INFO")
	viper.SetDefault("scan.regions", "")
	viper.SetDefault("scan.scanners", "")
	viper.SetDefault("scan.output", "filesystem")
	viper.SetDefault("scan.output_format", "html")
	viper.SetDefault("scan.bucket", "")
	viper.SetDefault("scan.bucket_region", "")
	viper.SetDefault("scan.days_unused", 90)

	// Try to read config file but don't error if not found
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Only return error if it's not a missing config file
			return fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found is okay, we'll use defaults and env vars
		if shouldLog {
			logging.Debug("No config file found, using defaults and environment variables", nil)
		}
	} else if shouldLog {
		logging.Debug("Loaded config file", map[string]interface{}{
			"path": viper.ConfigFileUsed(),
		})
	}

	return nil
}

// SetConfigFile sets a custom config file path and reloads the configuration
func SetConfigFile(configFile string) error {
	// Set the config file path
	viper.SetConfigFile(configFile)

	// Read the config file
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("error reading config file: %w", err)
	}

	return nil
}

// CreateDefaultConfig creates a default config file if it doesn't exist
func CreateDefaultConfig() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("error getting home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".cloudsift")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("error creating config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := []byte(`# CloudSift Configuration File

# AWS Configuration
aws:
  profile: default  # AWS profile to use (supports SSO profiles)
  organization_role: ""  # Role name to assume for organization-wide operations
  scanner_role: ""  # Role name to assume for scanning operations

# Application Configuration
app:
  max_workers: 8  # Maximum number of concurrent workers
  log_format: text  # Log output format (text or json)
  log_level: INFO  # Set logging level (DEBUG, INFO, WARN, ERROR)

# Scan Command Configuration
scan:
  # List of regions to scan (default: all available regions)
  regions:
    - us-west-2  # Example region
    - us-east-1  # Example region
  # List of scanners to run (default: all available scanners)
  scanners:
    - ec2-instances  # Example scanner
    - ebs-volumes   # Example scanner
  output: filesystem  # Output type (filesystem or s3)
  output_format: html  # Output format (json or html)
  bucket: ""  # S3 bucket name (required when output=s3)
  bucket_region: ""  # S3 bucket region (required when output=s3)
  days_unused: 90  # Number of days a resource must be unused to be reported
`)
		if err := os.WriteFile(configPath, defaultConfig, 0644); err != nil {
			return fmt.Errorf("error writing default config file: %w", err)
		}
	}

	return nil
}
