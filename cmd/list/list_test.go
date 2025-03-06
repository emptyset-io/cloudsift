package list

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/undefinedlabs/go-mpatch"

	awspkg "cloudsift/internal/aws"
	"cloudsift/internal/config"
)

// captureOutput captures stdout and returns the captured output
func captureOutput(f func()) string {
	// Save original stdout
	oldStdout := os.Stdout

	// Create a pipe to capture stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Call the function that produces output
	f()

	// Close the writer and restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read the captured output
	var buf bytes.Buffer
	_, err := io.Copy(&buf, r)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error copying output: %v\n", err)
	}

	return buf.String()
}

// Helper function to execute a command and capture its output
func executeCommand(cmd *cobra.Command, args ...string) (string, error) {
	// Save original stdout
	oldStdout := os.Stdout

	// Create a pipe to capture stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Set up command
	cmd.SetOut(w)
	cmd.SetErr(w)
	cmd.SetArgs(args)

	// Execute command
	err := cmd.Execute()

	// Close the writer and restore stdout
	w.Close()
	os.Stdout = oldStdout

	// Read the captured output
	var buf bytes.Buffer
	_, copyErr := io.Copy(&buf, r)
	if copyErr != nil {
		fmt.Fprintf(os.Stderr, "Error copying output: %v\n", copyErr)
	}

	return buf.String(), err
}

// Helper function to safely unpatch
func safeUnpatch(patch *mpatch.Patch) {
	if err := patch.Unpatch(); err != nil {
		fmt.Fprintf(os.Stderr, "Error unpatching: %v\n", err)
	}
}

// Test scanner implementation
type testScanner struct {
	name         string
	argumentName string
	label        string
}

func (s *testScanner) Name() string {
	return s.name
}

func (s *testScanner) ArgumentName() string {
	return s.argumentName
}

func (s *testScanner) Label() string {
	return s.label
}

func (s *testScanner) Scan(opts awspkg.ScanOptions) (awspkg.ScanResults, error) {
	return awspkg.ScanResults{}, nil
}

// TestNewListCmd tests the creation of the list command
func TestNewListCmd(t *testing.T) {
	cmd := NewListCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands
	subcommands := cmd.Commands()
	expectedSubcommands := []string{
		"accounts",
		"profiles",
		"scanners",
	}

	assert.Len(t, subcommands, len(expectedSubcommands))
	for _, subcmd := range subcommands {
		assert.Contains(t, expectedSubcommands, subcmd.Name())
	}
}

// TestNewAccountsCmd tests the creation of the accounts command
func TestNewAccountsCmd(t *testing.T) {
	cmd := NewAccountsCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, "accounts", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)

	// Verify flags
	flags := cmd.Flags()
	orgRoleFlag := flags.Lookup("organization-role")
	assert.NotNil(t, orgRoleFlag)
	assert.Equal(t, "string", orgRoleFlag.Value.Type())
	assert.Equal(t, "", orgRoleFlag.DefValue)
}

// TestNewProfilesCmd tests the creation of the profiles command
func TestNewProfilesCmd(t *testing.T) {
	cmd := NewProfilesCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, "profiles", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)
}

// TestNewScannersCmd tests the creation of the scanners command
func TestNewScannersCmd(t *testing.T) {
	cmd := NewScannersCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, "scanners", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotEmpty(t, cmd.Example)
}

// TestRunAccounts tests the runAccounts function
func TestRunAccounts(t *testing.T) {
	tests := []struct {
		name           string
		orgRole        string
		mockAccounts   []awspkg.Account
		mockError      error
		expectedOutput string
		expectError    bool
	}{
		{
			name:    "list accounts without organization role",
			orgRole: "",
			mockAccounts: []awspkg.Account{
				{ID: "123456789012", Name: "Current"},
			},
			mockError:      nil,
			expectedOutput: "Available accounts:\n  123456789012 - Current\n",
			expectError:    false,
		},
		{
			name:    "list organization accounts",
			orgRole: "OrganizationRole",
			mockAccounts: []awspkg.Account{
				{ID: "123456789012", Name: "Account1"},
				{ID: "098765432109", Name: "Account2"},
			},
			mockError:      nil,
			expectedOutput: "Available accounts:\n  123456789012 - Account1\n  098765432109 - Account2\n",
			expectError:    false,
		},
		{
			name:           "no accounts found",
			orgRole:        "",
			mockAccounts:   []awspkg.Account{},
			mockError:      nil,
			expectedOutput: "No accounts found\n",
			expectError:    false,
		},
		{
			name:           "error listing accounts",
			orgRole:        "",
			mockAccounts:   nil,
			mockError:      fmt.Errorf("mock error"),
			expectedOutput: "",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset viper config before each test
			viper.Reset()
			config.Config = &config.GlobalConfig{
				OrganizationRole: tt.orgRole,
			}

			// Patch the ListAccounts function
			patch, err := mpatch.PatchMethod(awspkg.ListAccounts, func(orgRole string) ([]awspkg.Account, error) {
				if tt.name == "list organization accounts" {
					assert.Equal(t, "OrganizationRole", orgRole)
				} else {
					assert.Equal(t, tt.orgRole, orgRole)
				}
				return tt.mockAccounts, tt.mockError
			})
			require.NoError(t, err)
			defer safeUnpatch(patch)

			// Create command
			cmd := NewAccountsCmd()

			if tt.name == "list organization accounts" {
				err := cmd.Flags().Set("organization-role", "OrganizationRole")
				require.NoError(t, err)
			}

			// Capture output and execute command
			var output string
			var cmdErr error

			if tt.expectError {
				cmdErr = runAccounts(cmd)
				assert.Error(t, cmdErr)
				return
			}

			output = captureOutput(func() {
				cmdErr = runAccounts(cmd)
			})

			assert.NoError(t, cmdErr)
			expectedOutput := strings.ReplaceAll(tt.expectedOutput, "\n", "\n")
			output = strings.ReplaceAll(output, "\r\n", "\n")
			assert.Equal(t, expectedOutput, output)
		})
	}
}

// TestRunProfiles tests the runProfiles function
func TestRunProfiles(t *testing.T) {
	tests := []struct {
		name           string
		mockProfiles   []string
		mockError      error
		expectedOutput string
		expectError    bool
	}{
		{
			name: "list available profiles",
			mockProfiles: []string{
				"default",
				"dev",
				"prod",
			},
			mockError:      nil,
			expectedOutput: "default\ndev\nprod\n",
			expectError:    false,
		},
		{
			name:           "no profiles found",
			mockProfiles:   []string{},
			mockError:      nil,
			expectedOutput: "",
			expectError:    false,
		},
		{
			name:           "error listing profiles",
			mockProfiles:   nil,
			mockError:      fmt.Errorf("mock error"),
			expectedOutput: "",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset viper config before each test
			viper.Reset()

			// Patch the ListProfiles function
			patch, err := mpatch.PatchMethod(awspkg.ListProfiles, func() ([]string, error) {
				return tt.mockProfiles, tt.mockError
			})
			require.NoError(t, err)
			defer safeUnpatch(patch)

			// Capture output and execute function
			var output string
			var cmdErr error

			if tt.expectError {
				cmdErr = runProfiles()
				assert.Error(t, cmdErr)
				return
			}

			output = captureOutput(func() {
				cmdErr = runProfiles()
			})

			assert.NoError(t, cmdErr)
			expectedOutput := strings.ReplaceAll(tt.expectedOutput, "\n", "\n")
			output = strings.ReplaceAll(output, "\r\n", "\n")
			assert.Equal(t, expectedOutput, output)
		})
	}
}

// TestRunScanners tests the scanners command RunE function
func TestRunScanners(t *testing.T) {
	tests := []struct {
		name           string
		scannerNames   []string
		expectedOutput string
		expectError    bool
	}{
		{
			name:           "list available scanners",
			scannerNames:   []string{"test-scanner"},
			expectedOutput: "Available scanners:\n  - test - Test Scanner\n",
			expectError:    false,
		},
		{
			name:           "no scanners registered",
			scannerNames:   []string{},
			expectedOutput: "No scanners registered\n",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original registry
			originalRegistry := awspkg.DefaultRegistry

			// Create a new registry for testing
			testRegistry := awspkg.NewScannerRegistry()
			awspkg.DefaultRegistry = testRegistry

			if len(tt.scannerNames) > 0 {
				// Register test scanners
				for _, name := range tt.scannerNames {
					scanner := &testScanner{
						name:         name,
						argumentName: "test",
						label:        "Test Scanner",
					}
					testRegistry.RegisterScanner(scanner)
				}
			}

			// Create command
			cmd := NewScannersCmd()

			// Capture output and execute command
			var output string
			var cmdErr error

			output = captureOutput(func() {
				cmdErr = cmd.RunE(cmd, nil)
			})

			// Restore original registry
			awspkg.DefaultRegistry = originalRegistry

			// Verify results
			assert.NoError(t, cmdErr)
			expectedOutput := strings.ReplaceAll(tt.expectedOutput, "\n", "\n")
			output = strings.ReplaceAll(output, "\r\n", "\n")
			assert.Equal(t, expectedOutput, output)
		})
	}
}

// TestIntegration tests the integration of all commands
func TestIntegration(t *testing.T) {
	// Save original values
	originalRegistry := awspkg.DefaultRegistry

	// Create a new registry for testing
	testRegistry := awspkg.NewScannerRegistry()
	awspkg.DefaultRegistry = testRegistry

	// Register a test scanner
	scanner := &testScanner{
		name:         "test-scanner",
		argumentName: "test",
		label:        "Test Scanner",
	}
	testRegistry.RegisterScanner(scanner)

	// Patch the ListAccounts function
	patchAccounts, err := mpatch.PatchMethod(awspkg.ListAccounts, func(orgRole string) ([]awspkg.Account, error) {
		return []awspkg.Account{
			{ID: "123456789012", Name: "TestAccount"},
		}, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(patchAccounts)

	// Patch the ListProfiles function
	patchProfiles, err := mpatch.PatchMethod(awspkg.ListProfiles, func() ([]string, error) {
		return []string{"default", "test"}, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(patchProfiles)

	// Test the list command with each subcommand
	testCases := []struct {
		args           []string
		expectedOutput string
	}{
		{
			args:           []string{"accounts"},
			expectedOutput: "Available accounts:\n  123456789012 - TestAccount\n",
		},
		{
			args:           []string{"profiles"},
			expectedOutput: "default\ntest\n",
		},
		{
			args:           []string{"scanners"},
			expectedOutput: "Available scanners:\n  - test - Test Scanner\n",
		},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("list %s", tc.args[0]), func(t *testing.T) {
			cmd := NewListCmd()
			output, err := executeCommand(cmd, tc.args...)
			require.NoError(t, err)

			expectedOutput := strings.ReplaceAll(tc.expectedOutput, "\n", "\n")
			output = strings.ReplaceAll(output, "\r\n", "\n")
			assert.Contains(t, output, expectedOutput)
		})
	}

	// Restore original registry
	awspkg.DefaultRegistry = originalRegistry
}
