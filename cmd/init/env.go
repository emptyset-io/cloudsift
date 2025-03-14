package init

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const defaultEnvContent = `# CloudSift Environment Configuration
# Generated by cloudsift init env

#######################
# AWS Configuration
#######################

# AWS profile to use (supports SSO profiles)
# Default: default
CLOUDSIFT_AWS_PROFILE=default

# Role to assume for organization-wide operations
# Leave empty to only scan the current account
CLOUDSIFT_AWS_ORGANIZATION_ROLE=

# Role to assume for scanning operations in member accounts
# Required when scanning organization accounts
CLOUDSIFT_AWS_SCANNER_ROLE=

#######################
# Application Settings
#######################

# Maximum number of concurrent workers
# Default: Number of CPU cores
CLOUDSIFT_APP_MAX_WORKERS=8

# Log output format (text or json)
# Default: text
CLOUDSIFT_APP_LOG_FORMAT=text

# Log level (DEBUG, INFO, WARN, ERROR)
# Default: INFO
CLOUDSIFT_APP_LOG_LEVEL=INFO

#######################
# Scan Configuration
#######################

# Comma-separated list of AWS regions to scan
# Leave empty to scan all available regions
# Example: us-west-2,us-east-1,eu-west-1
CLOUDSIFT_SCAN_REGIONS=

# Comma-separated list of resource scanners to run
# Leave empty to run all available scanners
# Example: ec2-instances,ebs-volumes
CLOUDSIFT_SCAN_SCANNERS=

# Comma-separated list of AWS account IDs to scan
# Leave empty to scan all accounts in the organization
# Example: 123456789012,098765432109
CLOUDSIFT_SCAN_ACCOUNTS=

# Output type (filesystem or s3)
# Default: filesystem
CLOUDSIFT_SCAN_OUTPUT=filesystem

# Output format (json or html)
# Default: html
CLOUDSIFT_SCAN_OUTPUT_FORMAT=html

# S3 bucket name for output
# Required when CLOUDSIFT_SCAN_OUTPUT=s3
CLOUDSIFT_SCAN_BUCKET=

# S3 bucket region
# Required when CLOUDSIFT_SCAN_OUTPUT=s3
CLOUDSIFT_SCAN_BUCKET_REGION=

# Number of days a resource must be unused to be reported
# Default: 90
CLOUDSIFT_SCAN_DAYS_UNUSED=90

#######################
# Ignore List Configuration
#######################

# Comma-separated list of resource IDs to ignore (case-insensitive)
# Example: i-1234567890abcdef0 will match I-1234567890ABCDEF0
#         vol-1234567890abcdef0 will match VOL-1234567890ABCDEF0
CLOUDSIFT_SCAN_IGNORE_RESOURCE_IDS=

# Comma-separated list of resource names to ignore (case-insensitive)
# Example: my-important-instance will match MY-IMPORTANT-INSTANCE
#         critical-data-volume will match Critical-Data-Volume
CLOUDSIFT_SCAN_IGNORE_RESOURCE_NAMES=

# Resource tags to ignore in KEY=VALUE format, comma-separated (case-insensitive)
# Both tag keys and values are matched case-insensitively
# Example: Environment=production will match ENVIRONMENT=PRODUCTION
#         KeepAlive=true will match keepalive=TRUE
#         Project=critical-service will match PROJECT=CRITICAL-SERVICE
CLOUDSIFT_SCAN_IGNORE_TAGS=

`

// NewEnvCmd creates the env subcommand
func NewEnvCmd() *cobra.Command {
	var force bool
	var output string

	cmd := &cobra.Command{
		Use:   "env",
		Short: "Create a default .env file",
		Long: `Create a default .env file with recommended settings.

The file will be created in the current directory by default.
You can specify a different location using the --output flag.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if output == "" {
				output = ".env"
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
			if err := os.WriteFile(absPath, []byte(defaultEnvContent), 0644); err != nil {
				return fmt.Errorf("failed to write env file: %w", err)
			}

			fmt.Printf("Created env file: %s\n", absPath)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Overwrite existing file")
	cmd.Flags().StringVarP(&output, "output", "o", "", "Output file path (default: ./.env)")

	return cmd
}
