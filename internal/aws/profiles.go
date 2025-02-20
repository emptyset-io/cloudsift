package aws

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go/aws/defaults"
	"gopkg.in/ini.v1"
)

// ListProfiles returns a list of available AWS profiles
func ListProfiles() ([]string, error) {
	// Get the credentials file path
	credsPath := os.Getenv("AWS_SHARED_CREDENTIALS_FILE")
	if credsPath == "" {
		credsPath = filepath.Join(defaults.SharedCredentialsFilename())
	}

	// Get the config file path
	configPath := os.Getenv("AWS_CONFIG_FILE")
	if configPath == "" {
		configPath = filepath.Join(defaults.SharedConfigFilename())
	}

	profiles := make(map[string]struct{})

	// Read profiles from credentials file
	if _, err := os.Stat(credsPath); err == nil {
		credsFile, err := ini.Load(credsPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load credentials file: %w", err)
		}

		for _, section := range credsFile.Sections() {
			if section.Name() != "DEFAULT" && section.Name() != ini.DefaultSection {
				profiles[section.Name()] = struct{}{}
			}
		}
	}

	// Read profiles from config file
	if _, err := os.Stat(configPath); err == nil {
		configFile, err := ini.Load(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}

		for _, section := range configFile.Sections() {
			if section.Name() != "DEFAULT" && section.Name() != ini.DefaultSection {
				name := strings.TrimPrefix(section.Name(), "profile ")
				profiles[name] = struct{}{}
			}
		}
	}

	// Convert map to sorted slice
	result := make([]string, 0, len(profiles))
	for profile := range profiles {
		result = append(result, profile)
	}
	sort.Strings(result)

	return result, nil
}

// IsValidProfile checks if a profile exists
func IsValidProfile(profile string) bool {
	profiles, err := ListProfiles()
	if err != nil {
		return false
	}

	for _, p := range profiles {
		if p == profile {
			return true
		}
	}

	return false
}
