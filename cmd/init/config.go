package init

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const defaultConfigContent = `# CloudSift Configuration File

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
   # - us-west-2  # Example region
   # - us-east-1  # Example region
  
  # List of scanners to run (default: all available scanners)
  scanners:
   # - ec2-instances  # Example scanner
   # - ebs-volumes   # Example scanner
  
  # List of AWS account IDs to scan (default: all accounts in organization)
  accounts:
   # - 123456789012  # Example account ID
   # - 098765432109  # Example account ID
  
  output: filesystem  # Output type (filesystem or s3)
  output_format: html  # Output format (json or html)
  bucket: ""  # S3 bucket name (required when output=s3)
  bucket_region: ""  # S3 bucket region (required when output=s3)
  days_unused: 90  # Number of days a resource must be unused to be reported

  # Ignore list configuration
  # Resources matching any of these criteria will be excluded from scan results
  # All matching is case-insensitive (e.g., "Production" will match "PRODUCTION")
  ignore:
    # Ignore by resource ID (case-insensitive)
    # Examples:
    #   i-1234567890abcdef0 will match I-1234567890ABCDEF0
    #   vol-1234567890abcdef0 will match VOL-1234567890ABCDEF0
    resource_ids:
      # - i-1234567890abcdef0
      # - vol-1234567890abcdef0

    # Ignore by resource name (case-insensitive)
    # Examples:
    #   my-important-instance will match MY-IMPORTANT-INSTANCE
    #   critical-data-volume will match Critical-Data-Volume
    resource_names:
      # - my-important-instance
      # - critical-data-volume

    # Ignore by resource tags (case-insensitive)
    # Both tag keys and values are matched case-insensitively
    # Examples:
    #   Environment: production will match ENVIRONMENT: PRODUCTION
    #   KeepAlive: true will match keepalive: TRUE
    tags:
      # Environment: production
      # KeepAlive: true
      # Project: critical-service`

// NewConfigCmd creates the config subcommand
func NewConfigCmd() *cobra.Command {
	var force bool
	var output string

	cmd := &cobra.Command{
		Use:   "config",
		Short: "Create a default config.yaml file",
		Long: `Create a default config.yaml file with recommended settings.

The file will be created in the current directory by default.
You can specify a different location using the --output flag.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				output = "config.yaml"
			}

			// Convert to absolute path
			absPath, err := filepath.Abs(output)
			if err != nil {
				return fmt.Errorf("failed to resolve absolute path: %w", err)
			}

			// Check if file exists
			if _, err := os.Stat(absPath); err == nil && !force {
				return fmt.Errorf("file %s already exists. Use --force to overwrite", absPath)
			}

			// Create directory if it doesn't exist
			dir := filepath.Dir(absPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dir, err)
			}

			// Write the file
			if err := os.WriteFile(absPath, []byte(defaultConfigContent), 0644); err != nil {
				return fmt.Errorf("failed to write config file: %w", err)
			}

			fmt.Printf("Created config file: %s\n", absPath)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing file")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path (default: ./config.yaml)")

	return cmd
}
