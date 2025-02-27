package config

import "runtime"

// GlobalConfig holds the global configuration for the application
type GlobalConfig struct {
	// Profile is the AWS profile to use
	Profile string

	// OrganizationRole is the role name to assume for organization-wide operations
	OrganizationRole string

	// ScannerRole is the role name to assume for scanning operations
	ScannerRole string

	// MaxWorkers defines the maximum number of concurrent workers
	MaxWorkers int

	// LogFormat is the format for logging
	LogFormat string

	// LogLevel is the level for logging
	LogLevel string

	// ScanRegions is the list of regions to scan
	ScanRegions string

	// ScanScanners is the list of scanners to use
	ScanScanners string

	// ScanOutput is the output file for scan results
	ScanOutput string

	// ScanOutputFormat is the format for scan output
	ScanOutputFormat string

	// ScanBucket is the S3 bucket to scan
	ScanBucket string

	// ScanBucketRegion is the region of the S3 bucket to scan
	ScanBucketRegion string

	// ScanDaysUnused is the number of days since last usage to consider an object unused
	ScanDaysUnused int

	// ScanIgnoreResourceIDs is the list of resource IDs to ignore
	ScanIgnoreResourceIDs []string

	// ScanIgnoreResourceNames is the list of resource names to ignore
	ScanIgnoreResourceNames []string

	// ScanIgnoreTags is the map of tags to ignore
	ScanIgnoreTags map[string]string

	// ScanAccounts is the list of account IDs to scan
	ScanAccounts []string
}

// Config is the global configuration instance
var Config = &GlobalConfig{
	Profile:    "default",            // Default to "default" profile
	MaxWorkers: runtime.NumCPU() * 8, // Default to 4x CPU cores since tasks are I/O bound
}
