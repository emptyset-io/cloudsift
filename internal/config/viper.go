package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"cloudsift/internal/logging"
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

// getParameterSource determines where a parameter value came from (config file, env var, or flag)
func getParameterSource(key string) parameterSource {
	flagValue := viper.Get(key)
	envKey := "CLOUDSIFT_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	
	// Check if value is set by flag
	if viper.IsSet(key) && viper.GetViper().InConfig(key) {
		return parameterSource{key, flagValue, "config"}
	}
	
	// Check if value is set by environment variable
	if _, exists := os.LookupEnv(envKey); exists {
		return parameterSource{key, flagValue, "environment"}
	}
	
	// If value is set but not in config or env, it must be from flag
	if viper.IsSet(key) {
		return parameterSource{key, flagValue, "flag"}
	}
	
	// Value is using default
	return parameterSource{key, flagValue, "default"}
}

// logConfigurationSources logs the source of each configuration parameter
func logConfigurationSources(shouldLog bool) {
	if !shouldLog {
		return
	}

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
		"scan.ignore.resource_ids",
		"scan.ignore.resource_names",
		"scan.ignore.tags",
	}

	// Group parameters by their source
	sourceMap := make(map[string][]parameterSource)
	for _, param := range params {
		source := getParameterSource(param)
		if source.Value != nil && source.Value != "" {
			sourceMap[source.Source] = append(sourceMap[source.Source], source)
		}
	}

	// Log parameters grouped by source
	sources := []string{"flag", "environment", "config", "default"}
	for _, source := range sources {
		if params, ok := sourceMap[source]; ok {
			values := make(map[string]interface{})
			for _, p := range params {
				values[p.Key] = p.Value
			}
			if len(values) > 0 {
				logging.Info(fmt.Sprintf("Parameters from %s:", source), values)
			}
		}
	}
}

// InitConfig initializes the Viper configuration
func InitConfig(shouldLog bool) error {
	// Set config name and type
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	// Add config search paths
	viper.AddConfigPath(".")                                // Current directory
	viper.AddConfigPath("$HOME/.cloudsift")                // Home directory
	viper.AddConfigPath("/etc/cloudsift/")                 // System-wide directory
	
	// Set environment variable prefix
	viper.SetEnvPrefix("CLOUDSIFT")
	viper.AutomaticEnv()

	// Read config file
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

	// Set defaults for all configuration values
	viper.SetDefault("aws.profile", "default")
	viper.SetDefault("aws.organization_role", "")
	viper.SetDefault("aws.scanner_role", "")
	viper.SetDefault("app.max_workers", 8)
	viper.SetDefault("app.log_format", "text")
	viper.SetDefault("app.log_level", "INFO")
	viper.SetDefault("list.format", "text")

	// Scan command defaults
	viper.SetDefault("scan.regions", []string{})
	viper.SetDefault("scan.scanners", []string{})
	viper.SetDefault("scan.output", "filesystem")
	viper.SetDefault("scan.output_format", "html")
	viper.SetDefault("scan.bucket", "")
	viper.SetDefault("scan.bucket_region", "")
	viper.SetDefault("scan.days_unused", 90)
	viper.SetDefault("scan.ignore.resource_ids", []string{})
	viper.SetDefault("scan.ignore.resource_names", []string{})
	viper.SetDefault("scan.ignore.tags", map[string]string{})

	// Replace - with _ in environment variables
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))

	// Log configuration sources only for scan and list commands
	logConfigurationSources(shouldLog)

	// Update config struct
	Config.Profile = viper.GetString("aws.profile")
	Config.OrganizationRole = viper.GetString("aws.organization_role")
	Config.ScannerRole = viper.GetString("aws.scanner_role")
	Config.MaxWorkers = viper.GetInt("app.max_workers")
	Config.LogFormat = viper.GetString("app.log_format")

	// Convert YAML lists to comma-separated strings
	Config.ScanRegions = convertSliceToString(viper.GetStringSlice("scan.regions"))
	Config.ScanScanners = convertSliceToString(viper.GetStringSlice("scan.scanners"))
	Config.ScanOutput = viper.GetString("scan.output")
	Config.ScanOutputFormat = viper.GetString("scan.output_format")
	Config.ScanBucket = viper.GetString("scan.bucket")
	Config.ScanBucketRegion = viper.GetString("scan.bucket_region")
	Config.ScanDaysUnused = viper.GetInt("scan.days_unused")
	Config.ScanIgnoreResourceIDs = viper.GetStringSlice("scan.ignore.resource_ids")
	Config.ScanIgnoreResourceNames = viper.GetStringSlice("scan.ignore.resource_names")
	Config.ScanIgnoreTags = viper.GetStringMapString("scan.ignore.tags")

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

	// Update config struct with new values
	Config.Profile = viper.GetString("aws.profile")
	Config.OrganizationRole = viper.GetString("aws.organization_role")
	Config.ScannerRole = viper.GetString("aws.scanner_role")
	Config.MaxWorkers = viper.GetInt("app.max_workers")
	Config.LogFormat = viper.GetString("app.log_format")
	Config.ScanRegions = convertSliceToString(viper.GetStringSlice("scan.regions"))
	Config.ScanScanners = convertSliceToString(viper.GetStringSlice("scan.scanners"))
	Config.ScanOutput = viper.GetString("scan.output")
	Config.ScanOutputFormat = viper.GetString("scan.output_format")
	Config.ScanBucket = viper.GetString("scan.bucket")
	Config.ScanBucketRegion = viper.GetString("scan.bucket_region")
	Config.ScanDaysUnused = viper.GetInt("scan.days_unused")
	Config.ScanIgnoreResourceIDs = viper.GetStringSlice("scan.ignore.resource_ids")
	Config.ScanIgnoreResourceNames = viper.GetStringSlice("scan.ignore.resource_names")
	Config.ScanIgnoreTags = viper.GetStringMapString("scan.ignore.tags")

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

# List Command Configuration
list:
  format: text  # Output format for list commands (text or json)

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
  ignore:
    resource_ids: []  # List of resource IDs to ignore
    resource_names: []  # List of resource names to ignore
    tags: {}  # Map of tags to ignore (key=value)
`)
		if err := os.WriteFile(configPath, defaultConfig, 0644); err != nil {
			return fmt.Errorf("error writing default config file: %w", err)
		}
	}

	return nil
}
