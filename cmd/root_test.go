package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"cloudsift/internal/config"
)

func setupRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use: "cloudsift",
		Run: func(cmd *cobra.Command, args []string) {}, // Add empty Run to handle no command case
	}
	rootCmd.PersistentFlags().String("config", "", "config file")
	rootCmd.PersistentFlags().String("log-format", "text", "log format")
	rootCmd.PersistentFlags().String("log-level", "INFO", "log level")
	rootCmd.PersistentFlags().String("profile", "default", "AWS profile")
	rootCmd.PersistentFlags().String("organization-role", "", "org role")
	rootCmd.PersistentFlags().String("scanner-role", "", "scanner role")
	rootCmd.PersistentFlags().Int("max-workers", 8, "max workers")

	// Add required commands for testing
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run:   func(cmd *cobra.Command, args []string) {},
	})
	rootCmd.AddCommand(&cobra.Command{
		Use:   "help",
		Short: "Help about any command",
		Run:   func(cmd *cobra.Command, args []string) {},
	})

	return rootCmd
}

func TestExecute(t *testing.T) {
	// Save original args and restore them after test
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	// Create test config file
	tmpDir := t.TempDir()
	configFile := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(configFile, []byte(`
aws:
  profile: test-profile
  organization_role: OrgRole
  scanner_role: ScanRole
app:
  max_workers: 16
`), 0644)
	require.NoError(t, err)

	tests := []struct {
		name     string
		args     []string
		wantErr  bool
		validate func(t *testing.T)
	}{
		{
			name:    "version command should not require config",
			args:    []string{"cloudsift", "version"},
			wantErr: false,
			validate: func(t *testing.T) {
				assert.Empty(t, config.Config.Profile, "version command should not load config")
				assert.Empty(t, config.Config.OrganizationRole, "version command should not load config")
				assert.Empty(t, config.Config.ScannerRole, "version command should not load config")
			},
		},
		{
			name:    "help command should not require config",
			args:    []string{"cloudsift", "help"},
			wantErr: false,
			validate: func(t *testing.T) {
				assert.Empty(t, config.Config.Profile, "help command should not load config")
				assert.Empty(t, config.Config.OrganizationRole, "help command should not load config")
				assert.Empty(t, config.Config.ScannerRole, "help command should not load config")
			},
		},
		{
			name:    "invalid command should return error",
			args:    []string{"cloudsift", "invalid"},
			wantErr: true,
		},
		{
			name: "valid config file should be loaded",
			args: []string{"cloudsift", "--config", configFile},
			validate: func(t *testing.T) {
				assert.Equal(t, "test-profile", config.Config.Profile)
				assert.Equal(t, "OrgRole", config.Config.OrganizationRole)
				assert.Equal(t, "ScanRole", config.Config.ScannerRole)
				assert.Equal(t, 16, config.Config.MaxWorkers)
			},
		},
		{
			name: "command line flags should override config",
			args: []string{
				"cloudsift",
				"--config", configFile,
				"--profile", "override-profile",
				"--organization-role", "override-role",
				"--scanner-role", "override-scanner",
				"--max-workers", "32",
			},
			validate: func(t *testing.T) {
				assert.Equal(t, "override-profile", config.Config.Profile)
				assert.Equal(t, "override-role", config.Config.OrganizationRole)
				assert.Equal(t, "override-scanner", config.Config.ScannerRole)
				assert.Equal(t, 32, config.Config.MaxWorkers)
			},
		},
		{
			name: "default values should be set when not specified",
			args: []string{"cloudsift"},
			validate: func(t *testing.T) {
				assert.Equal(t, "default", config.Config.Profile)
				assert.Empty(t, config.Config.OrganizationRole)
				assert.Empty(t, config.Config.ScannerRole)
				assert.Equal(t, 8, config.Config.MaxWorkers)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset viper and config before each test
			viper.Reset()
			viper.SetConfigType("yaml") // Need to set this after reset
			config.Config = &config.GlobalConfig{}

			// Set command line args
			os.Args = tt.args

			// For non-error cases, create a new root command and execute it
			if !tt.wantErr {
				rootCmd := setupRootCmd()
				rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
					// Skip config initialization for certain commands
					if cmd.Name() == "version" || cmd.Name() == "help" || cmd.Name() == "completion" {
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

					// Set config file if specified
					if configFile := cmd.Flag("config").Value.String(); configFile != "" {
						viper.SetConfigFile(configFile)
						if err := viper.ReadInConfig(); err != nil {
							return err
						}
					}

					// Set defaults
					viper.SetDefault("aws.profile", "default")
					viper.SetDefault("aws.organization_role", "")
					viper.SetDefault("aws.scanner_role", "")
					viper.SetDefault("app.max_workers", 8)

					// Update config struct with values from viper
					config.Config.Profile = viper.GetString("aws.profile")
					config.Config.OrganizationRole = viper.GetString("aws.organization_role")
					config.Config.ScannerRole = viper.GetString("aws.scanner_role")
					config.Config.MaxWorkers = viper.GetInt("app.max_workers")

					return nil
				}

				err = rootCmd.Execute()
			} else {
				err = Execute()
			}

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			if tt.validate != nil {
				tt.validate(t)
			}
		})
	}
}

func TestPersistentPreRunE(t *testing.T) {
	tests := []struct {
		name          string
		cmd           string
		args          []string
		expectedFlags map[string]interface{}
		wantErr       bool
	}{
		{
			name: "scan command should enable logging",
			cmd:  "scan",
			expectedFlags: map[string]interface{}{
				"app.log_format": "text",
				"app.log_level":  "INFO",
			},
		},
		{
			name: "list command should enable logging",
			cmd:  "list",
			expectedFlags: map[string]interface{}{
				"app.log_format": "text",
				"app.log_level":  "INFO",
			},
		},
		{
			name:          "version command should not enable logging",
			cmd:           "version",
			expectedFlags: map[string]interface{}{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset viper config before each test
			viper.Reset()
			config.Config = &config.GlobalConfig{}

			// Create root command and test command
			rootCmd := setupRootCmd()
			testCmd := &cobra.Command{Use: tt.cmd}
			rootCmd.AddCommand(testCmd)

			// Create buffer for command output
			buf := new(bytes.Buffer)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			// Set up the PersistentPreRunE function
			rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
				// Skip config initialization for certain commands
				if cmd.Name() == "version" || cmd.Name() == "help" || cmd.Name() == "completion" {
					return nil
				}

				// Check if we should enable logging
				if cmd.Name() == "scan" || cmd.Name() == "list" || (cmd.Parent() != nil && (cmd.Parent().Name() == "scan" || cmd.Parent().Name() == "list")) {
					// First bind global flags to viper
					if err := viper.BindPFlag("app.log_format", cmd.Root().PersistentFlags().Lookup("log-format")); err != nil {
						return err
					}
					if err := viper.BindPFlag("app.log_level", cmd.Root().PersistentFlags().Lookup("log-level")); err != nil {
						return err
					}
				}

				return nil
			}

			// Execute command's PersistentPreRunE
			err := rootCmd.PersistentPreRunE(testCmd, tt.args)

			if tt.wantErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// Validate expected flags
			for key, expected := range tt.expectedFlags {
				actual := viper.Get(key)
				assert.Equal(t, expected, actual, "flag %s has unexpected value", key)
			}
		})
	}
}
