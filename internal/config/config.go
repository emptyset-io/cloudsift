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
}

// Config is the global configuration instance
var Config = &GlobalConfig{
	Profile:    "default", // Default to "default" profile
	MaxWorkers: runtime.NumCPU() * 4, // Default to 4x CPU cores since tasks are I/O bound
}
