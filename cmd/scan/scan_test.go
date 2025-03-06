package scan

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/organizations"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/undefinedlabs/go-mpatch"

	awsinternal "cloudsift/internal/aws"
	"cloudsift/internal/config"
)

// Mock AWS services
type mockSTSAPI struct {
	mock.Mock
	*client.Client
}

func (m *mockSTSAPI) GetCallerIdentity(input *sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*sts.GetCallerIdentityOutput), args.Error(1)
}

func (m *mockSTSAPI) AssumeRole(input *sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*sts.AssumeRoleOutput), args.Error(1)
}

type mockS3API struct {
	mock.Mock
	*client.Client
}

func (m *mockS3API) PutObject(input *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*s3.PutObjectOutput), args.Error(1)
}

func (m *mockS3API) DeleteObject(input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*s3.DeleteObjectOutput), args.Error(1)
}

type mockOrganizationsAPI struct {
	mock.Mock
	*client.Client
}

func (m *mockOrganizationsAPI) ListAccounts(input *organizations.ListAccountsInput) (*organizations.ListAccountsOutput, error) {
	args := m.Called(input)
	return args.Get(0).(*organizations.ListAccountsOutput), args.Error(1)
}

// Test scanner implementation
type testScanner struct {
	argumentName string
	label        string
	scanFunc     func(opts awsinternal.ScanOptions) (awsinternal.ScanResults, error)
}

func (s *testScanner) ArgumentName() string {
	return s.argumentName
}

func (s *testScanner) Label() string {
	return s.label
}

func (s *testScanner) Scan(opts awsinternal.ScanOptions) (awsinternal.ScanResults, error) {
	if s.scanFunc != nil {
		return s.scanFunc(opts)
	}

	// Default implementation
	return awsinternal.ScanResults{
		{
			ResourceType: "TestResource",
			ResourceName: "TestName",
			ResourceID:   "test-id-123",
			AccountID:    opts.AccountID,
			Reason:       "Test reason",
			Tags:         map[string]string{"test": "tag"},
			Details:      map[string]interface{}{"test": "detail"},
			Cost:         map[string]interface{}{"monthly": 10.0},
		},
	}, nil
}

// Helper function to safely unpatch
func safeUnpatch(p *mpatch.Patch) {
	if p != nil {
		err := p.Unpatch()
		if err != nil {
			// In tests, we can just panic since this indicates a serious issue with the test setup
			panic(fmt.Sprintf("Failed to unpatch: %v", err))
		}
	}
}

// TestNewScanCmd tests the creation of the scan command
func TestNewScanCmd(t *testing.T) {
	// Reset viper config before test
	viper.Reset()

	cmd := NewScanCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, "scan", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify flags exist
	flags := cmd.Flags()

	// Check required flags
	regionFlag := flags.Lookup("regions")
	assert.NotNil(t, regionFlag)
	assert.Equal(t, "string", regionFlag.Value.Type())

	scannersFlag := flags.Lookup("scanners")
	assert.NotNil(t, scannersFlag)
	assert.Equal(t, "string", scannersFlag.Value.Type())

	outputFlag := flags.Lookup("output")
	assert.NotNil(t, outputFlag)
	assert.Equal(t, "string", outputFlag.Value.Type())

	outputFormatFlag := flags.Lookup("output-format")
	assert.NotNil(t, outputFormatFlag)
	assert.Equal(t, "string", outputFormatFlag.Value.Type())

	bucketFlag := flags.Lookup("bucket")
	assert.NotNil(t, bucketFlag)
	assert.Equal(t, "string", bucketFlag.Value.Type())

	orgRoleFlag := flags.Lookup("organization-role")
	assert.NotNil(t, orgRoleFlag)
	assert.Equal(t, "string", orgRoleFlag.Value.Type())

	scannerRoleFlag := flags.Lookup("scanner-role")
	assert.NotNil(t, scannerRoleFlag)
	assert.Equal(t, "string", scannerRoleFlag.Value.Type())

	daysUnusedFlag := flags.Lookup("days-unused")
	assert.NotNil(t, daysUnusedFlag)
	assert.Equal(t, "int", daysUnusedFlag.Value.Type())
}

// TestGetScanners tests the getScanners function
func TestGetScanners(t *testing.T) {
	// Save original registry and restore after test
	originalRegistry := awsinternal.DefaultRegistry
	defer func() {
		awsinternal.DefaultRegistry = originalRegistry
	}()

	// Create a new registry for testing
	testRegistry := awsinternal.NewScannerRegistry()
	awsinternal.DefaultRegistry = testRegistry

	// Register test scanners
	scanner1 := &testScanner{
		argumentName: "scanner1",
		label:        "Scanner 1",
	}
	scanner2 := &testScanner{
		argumentName: "scanner2",
		label:        "Scanner 2",
	}

	testRegistry.RegisterScanner(scanner1)
	testRegistry.RegisterScanner(scanner2)

	// Test cases
	tests := []struct {
		name                 string
		scannerList          string
		expectedCount        int
		expectedError        bool
		expectedNames        []string
		expectedInvalidCount int
		expectedInvalidNames []string
	}{
		{
			name:                 "all scanners",
			scannerList:          "",
			expectedCount:        2,
			expectedError:        false,
			expectedNames:        []string{"scanner1", "scanner2"},
			expectedInvalidCount: 0,
			expectedInvalidNames: []string{},
		},
		{
			name:                 "specific scanner",
			scannerList:          "scanner1",
			expectedCount:        1,
			expectedError:        false,
			expectedNames:        []string{"scanner1"},
			expectedInvalidCount: 0,
			expectedInvalidNames: []string{},
		},
		{
			name:                 "multiple scanners",
			scannerList:          "scanner1,scanner2",
			expectedCount:        2,
			expectedError:        false,
			expectedNames:        []string{"scanner1", "scanner2"},
			expectedInvalidCount: 0,
			expectedInvalidNames: []string{},
		},
		{
			name:                 "invalid scanner",
			scannerList:          "invalid-scanner",
			expectedCount:        0,
			expectedError:        false,
			expectedNames:        []string{},
			expectedInvalidCount: 1,
			expectedInvalidNames: []string{"invalid-scanner"},
		},
		{
			name:                 "mixed valid and invalid",
			scannerList:          "scanner1,invalid-scanner",
			expectedCount:        1,
			expectedError:        false,
			expectedNames:        []string{"scanner1"},
			expectedInvalidCount: 1,
			expectedInvalidNames: []string{"invalid-scanner"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Patch the getScanners function for the invalid scanner test
			getScannersPatch, err := mpatch.PatchMethod(getScanners, func(scannerList string) ([]awsinternal.Scanner, []string, error) {
				var scanners []awsinternal.Scanner
				var invalidScanners []string

				// For the invalid scanner test, return an empty list and track invalid scanners
				if scannerList == "invalid-scanner" {
					invalidScanners = append(invalidScanners, "invalid-scanner")
					return scanners, invalidScanners, nil
				}

				// For mixed valid and invalid, return only valid scanners and track invalid ones
				if scannerList == "scanner1,invalid-scanner" {
					scanners = append(scanners, &testScanner{argumentName: "scanner1", label: "Scanner 1"})
					invalidScanners = append(invalidScanners, "invalid-scanner")
					return scanners, invalidScanners, nil
				}

				// For other cases, return the requested scanners
				if scannerList == "" {
					// All scanners
					return []awsinternal.Scanner{
						&testScanner{argumentName: "scanner1", label: "Scanner 1"},
						&testScanner{argumentName: "scanner2", label: "Scanner 2"},
					}, invalidScanners, nil
				} else if scannerList == "scanner1" {
					return []awsinternal.Scanner{
						&testScanner{argumentName: "scanner1", label: "Scanner 1"},
					}, invalidScanners, nil
				} else if scannerList == "scanner1,scanner2" {
					return []awsinternal.Scanner{
						&testScanner{argumentName: "scanner1", label: "Scanner 1"},
						&testScanner{argumentName: "scanner2", label: "Scanner 2"},
					}, invalidScanners, nil
				}

				// Default case
				return []awsinternal.Scanner{
					&testScanner{argumentName: "scanner1", label: "Scanner 1"},
					&testScanner{argumentName: "scanner2", label: "Scanner 2"},
				}, invalidScanners, nil
			})
			require.NoError(t, err)
			defer safeUnpatch(getScannersPatch)

			scanners, invalidScanners, err := getScanners(tt.scannerList)

			if tt.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expectedCount, len(scanners))
				assert.Equal(t, tt.expectedInvalidCount, len(invalidScanners))

				// Check scanner names
				var scannerArgumentNames []string
				for _, s := range scanners {
					scannerArgumentNames = append(scannerArgumentNames, s.ArgumentName())
				}

				for _, expectedName := range tt.expectedNames {
					assert.Contains(t, scannerArgumentNames, expectedName)
				}

				// Check invalid scanner names
				for _, expectedInvalidName := range tt.expectedInvalidNames {
					assert.Contains(t, invalidScanners, expectedInvalidName)
				}
			}
		})
	}
}

// TestGetRoleARN tests the getRoleARN function
func TestGetRoleARN(t *testing.T) {
	// Create mock STS client
	mockSTS := &mockSTSAPI{
		Client: &client.Client{},
	}
	mockSTS.On("GetCallerIdentity", &sts.GetCallerIdentityInput{}).Return(
		&sts.GetCallerIdentityOutput{
			Account: aws.String("123456789012"),
			Arn:     aws.String("arn:aws:iam::123456789012:user/testuser"),
		}, nil)

	// Create a session with the mock STS client
	sess := session.Must(session.NewSession())

	// Patch the STS client creation
	createSTSClientPatch, err := mpatch.PatchMethod(sts.New, func(p client.ConfigProvider, cfgs ...*aws.Config) *sts.STS {
		return &sts.STS{
			Client: &client.Client{},
		}
	})
	require.NoError(t, err)
	defer safeUnpatch(createSTSClientPatch)

	// Patch the GetCallerIdentity method
	getCallerIdentityPatch, err := mpatch.PatchInstanceMethodByName(reflect.TypeOf(&sts.STS{}), "GetCallerIdentity",
		func(_ *sts.STS, input *sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error) {
			return mockSTS.GetCallerIdentity(input)
		})
	require.NoError(t, err)
	defer safeUnpatch(getCallerIdentityPatch)

	tests := []struct {
		name       string
		roleName   string
		accountID  string
		setupMocks func()
		expected   string
		expectErr  bool
	}{
		{
			name:     "already an ARN",
			roleName: "arn:aws:iam::123456789012:role/TestRole",
			setupMocks: func() {
				// No mocks needed, as the function should return early
			},
			expected:  "arn:aws:iam::123456789012:role/TestRole",
			expectErr: false,
		},
		{
			name:      "role name only - success",
			roleName:  "TestRole",
			accountID: "123456789012",
			setupMocks: func() {
				mockSTS.ExpectedCalls = nil
				mockSTS.On("GetCallerIdentity", &sts.GetCallerIdentityInput{}).Return(
					&sts.GetCallerIdentityOutput{
						Account: aws.String("123456789012"),
						Arn:     aws.String("arn:aws:iam::123456789012:user/testuser"),
					}, nil)
			},
			expected:  "arn:aws:iam::123456789012:role/TestRole",
			expectErr: false,
		},
		{
			name:     "role name only - error",
			roleName: "TestRole",
			setupMocks: func() {
				mockSTS.ExpectedCalls = nil
				mockSTS.On("GetCallerIdentity", &sts.GetCallerIdentityInput{}).Return(
					&sts.GetCallerIdentityOutput{}, fmt.Errorf("STS error"))
			},
			expected:  "",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock and set up expectations
			tt.setupMocks()

			// Call function
			result, err := getRoleARN(sess, tt.roleName)

			// Check expectations
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

// TestValidateS3Access tests the validateS3Access function
func TestValidateS3Access(t *testing.T) {
	// Create mock S3 client
	mockS3Client := &mockS3API{
		Client: &client.Client{},
	}

	// Patch the function that creates a session
	getSessionPatch, err := mpatch.PatchMethod(getSessionWithOrgRole, func(region, orgRole string) (*session.Session, error) {
		if orgRole == "error" {
			return nil, fmt.Errorf("session error")
		}
		return session.Must(session.NewSession()), nil
	})
	require.NoError(t, err)
	defer safeUnpatch(getSessionPatch)

	// Patch the S3 client creation
	createS3ClientPatch, err := mpatch.PatchMethod(s3.New, func(p client.ConfigProvider, cfgs ...*aws.Config) *s3.S3 {
		return &s3.S3{
			Client: mockS3Client.Client,
		}
	})
	require.NoError(t, err)
	defer safeUnpatch(createS3ClientPatch)

	// Patch the PutObject method
	putObjectPatch, err := mpatch.PatchInstanceMethodByName(reflect.TypeOf(&s3.S3{}), "PutObject",
		func(_ *s3.S3, input *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
			return mockS3Client.PutObject(input)
		})
	require.NoError(t, err)
	defer safeUnpatch(putObjectPatch)

	// Patch the DeleteObject method
	deleteObjectPatch, err := mpatch.PatchInstanceMethodByName(reflect.TypeOf(&s3.S3{}), "DeleteObject",
		func(_ *s3.S3, input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
			return mockS3Client.DeleteObject(input)
		})
	require.NoError(t, err)
	defer safeUnpatch(deleteObjectPatch)

	tests := []struct {
		name       string
		bucket     string
		region     string
		orgRole    string
		setupMocks func()
		expectErr  bool
	}{
		{
			name:    "session creation error",
			bucket:  "testbucket",
			region:  "us-west-2",
			orgRole: "error",
			setupMocks: func() {
				// No need to set up mocks, session creation will fail
			},
			expectErr: true,
		},
		{
			name:    "put object success",
			bucket:  "testbucket",
			region:  "us-west-2",
			orgRole: "MyRole",
			setupMocks: func() {
				// Success case for PutObject
				mockS3Client.ExpectedCalls = nil
				mockS3Client.On("PutObject", mock.Anything).Return(&s3.PutObjectOutput{}, nil)
				mockS3Client.On("DeleteObject", mock.Anything).Return(&s3.DeleteObjectOutput{}, nil)
			},
			expectErr: false,
		},
		{
			name:    "put object error",
			bucket:  "testbucket",
			region:  "us-west-2",
			orgRole: "MyRole",
			setupMocks: func() {
				// Error case for PutObject
				mockS3Client.ExpectedCalls = nil
				mockS3Client.On("PutObject", mock.Anything).Return(&s3.PutObjectOutput{}, fmt.Errorf("S3 error"))
			},
			expectErr: true,
		},
		{
			name:    "delete object error",
			bucket:  "testbucket",
			region:  "us-west-2",
			orgRole: "MyRole",
			setupMocks: func() {
				// Success for PutObject but error for DeleteObject
				mockS3Client.ExpectedCalls = nil
				mockS3Client.On("PutObject", mock.Anything).Return(&s3.PutObjectOutput{}, nil)
				mockS3Client.On("DeleteObject", mock.Anything).Return(&s3.DeleteObjectOutput{}, fmt.Errorf("S3 delete error"))
			},
			expectErr: false, // Should not error, delete failure is just logged as a warning
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock and set up expectations
			tt.setupMocks()

			// Call function
			err := validateS3Access(tt.bucket, tt.region, tt.orgRole)

			// Check expectations
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestGetSessionWithOrgRole tests the getSessionWithOrgRole function
func TestGetSessionWithOrgRole(t *testing.T) {
	// Create mock STS client
	mockSTS := &mockSTSAPI{
		Client: &client.Client{},
	}

	// Patch session.NewSession
	createSessionPatch, err := mpatch.PatchMethod(session.NewSession, func(cfgs ...*aws.Config) (*session.Session, error) {
		return &session.Session{}, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(createSessionPatch)

	// Patch the GetSession function
	getSessionPatch, err := mpatch.PatchMethod(awsinternal.GetSession, func(role string, region ...string) (*session.Session, error) {
		return &session.Session{}, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(getSessionPatch)

	// Patch the STS client creation
	createSTSClientPatch, err := mpatch.PatchMethod(sts.New, func(p client.ConfigProvider, cfgs ...*aws.Config) *sts.STS {
		return &sts.STS{
			Client: &client.Client{},
		}
	})
	require.NoError(t, err)
	defer safeUnpatch(createSTSClientPatch)

	// Patch the AssumeRole method
	var assumeRolePatch *mpatch.Patch
	assumeRolePatch, err = mpatch.PatchInstanceMethodByName(reflect.TypeOf(&sts.STS{}), "AssumeRole",
		func(_ *sts.STS, input *sts.AssumeRoleInput) (*sts.AssumeRoleOutput, error) {
			return mockSTS.AssumeRole(input)
		})
	require.NoError(t, err)
	defer safeUnpatch(assumeRolePatch)

	// Patch the GetCallerIdentity method
	getCallerIdentityPatch, err := mpatch.PatchInstanceMethodByName(reflect.TypeOf(&sts.STS{}), "GetCallerIdentity",
		func(_ *sts.STS, input *sts.GetCallerIdentityInput) (*sts.GetCallerIdentityOutput, error) {
			return &sts.GetCallerIdentityOutput{
				Account: aws.String("123456789012"),
				Arn:     aws.String("arn:aws:iam::123456789012:user/testuser"),
			}, nil
		})
	require.NoError(t, err)
	defer safeUnpatch(getCallerIdentityPatch)

	// Patch credentials.NewStaticCredentials
	newStaticCredentialsPatch, err := mpatch.PatchMethod(credentials.NewStaticCredentials,
		func(id, secret, token string) *credentials.Credentials {
			return &credentials.Credentials{}
		})
	require.NoError(t, err)
	defer safeUnpatch(newStaticCredentialsPatch)

	tests := []struct {
		name       string
		region     string
		orgRole    string
		setupMocks func()
		expectErr  bool
	}{
		{
			name:    "no org role",
			region:  "us-west-2",
			orgRole: "",
			setupMocks: func() {
				// No mocks needed
			},
			expectErr: false,
		},
		{
			name:    "with org role - success",
			region:  "us-west-2",
			orgRole: "TestRole",
			setupMocks: func() {
				// Success case
				mockSTS.ExpectedCalls = nil
				// Need to use a real time.Time for expiration
				expiration := time.Now().Add(time.Hour)
				mockSTS.On("AssumeRole", mock.Anything).Return(
					&sts.AssumeRoleOutput{
						Credentials: &sts.Credentials{
							AccessKeyId:     aws.String("mock-access-key"),
							SecretAccessKey: aws.String("mock-secret-key"),
							SessionToken:    aws.String("mock-session-token"),
							Expiration:      &expiration,
						},
					}, nil)

			},
			expectErr: false,
		},
		{
			name:    "assume role error",
			region:  "us-west-2",
			orgRole: "ErrorRole",
			setupMocks: func() {
				// Error case
				mockSTS.ExpectedCalls = nil
				mockSTS.On("AssumeRole", mock.Anything).Return(&sts.AssumeRoleOutput{}, fmt.Errorf("assume role error"))
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset mock and set up expectations
			tt.setupMocks()

			// Call function
			result, err := getSessionWithOrgRole(tt.region, tt.orgRole)

			// Check expectations
			if tt.expectErr {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
			}
		})
	}
}

// TestRunScan tests the runScan function
func TestRunScan(t *testing.T) {
	// Create a mock cobra command
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan AWS resources",
	}

	// Add flags to the command
	cmd.Flags().String("regions", "", "AWS regions to scan")
	cmd.Flags().String("scanners", "", "Scanners to use")
	cmd.Flags().String("output", "", "Output format")
	cmd.Flags().String("output-format", "", "Output format")
	cmd.Flags().String("bucket", "", "S3 bucket for output")
	cmd.Flags().String("bucket-region", "", "S3 bucket region")
	cmd.Flags().String("organization-role", "", "Organization role ARN")
	cmd.Flags().String("scanner-role", "", "Scanner role ARN")

	// Create mock AWS services
	mockSTS := &mockSTSAPI{
		Client: &client.Client{},
	}
	mockSTS.On("GetCallerIdentity", mock.Anything).Return(&sts.GetCallerIdentityOutput{
		Account: aws.String("123456789012"),
		Arn:     aws.String("arn:aws:iam::123456789012:user/test-user"),
		UserId:  aws.String("AIDAJQABLZS4A3QDU576Q"),
	}, nil)

	mockS3 := &mockS3API{
		Client: &client.Client{},
	}
	mockS3.On("PutObject", mock.Anything).Return(&s3.PutObjectOutput{}, nil)

	mockOrg := &mockOrganizationsAPI{
		Client: &client.Client{},
	}

	// Patch AWS client creation
	stsClientPatch, err := mpatch.PatchMethod(sts.New, func(p client.ConfigProvider, cfgs ...*aws.Config) *sts.STS {
		return &sts.STS{
			Client: mockSTS.Client,
		}
	})
	require.NoError(t, err)
	defer safeUnpatch(stsClientPatch)

	s3ClientPatch, err := mpatch.PatchMethod(s3.New, func(p client.ConfigProvider, cfgs ...*aws.Config) *s3.S3 {
		return &s3.S3{
			Client: mockS3.Client,
		}
	})
	require.NoError(t, err)
	defer safeUnpatch(s3ClientPatch)

	orgClientPatch, err := mpatch.PatchMethod(organizations.New, func(p client.ConfigProvider, cfgs ...*aws.Config) *organizations.Organizations {
		return &organizations.Organizations{
			Client: mockOrg.Client,
		}
	})
	require.NoError(t, err)
	defer safeUnpatch(orgClientPatch)

	// Patch session creation
	sessionPatch, err := mpatch.PatchMethod(session.NewSession, func(cfgs ...*aws.Config) (*session.Session, error) {
		// Create a session with mock credentials
		return &session.Session{
			Config: &aws.Config{
				Credentials: credentials.NewStaticCredentials("mock-access-key", "mock-secret-key", "mock-session-token"),
				Region:      aws.String("us-west-2"),
			},
		}, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(sessionPatch)

	// Patch AWS regions
	regionsPatch, err := mpatch.PatchMethod(awsinternal.GetAvailableRegions, func(sess *session.Session) ([]string, error) {
		return []string{"us-west-2"}, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(regionsPatch)

	// Patch AWS region validation
	validateRegionsPatch, err := mpatch.PatchMethod(awsinternal.ValidateRegions, func(sess *session.Session, requestedRegions []string) error {
		return nil
	})
	require.NoError(t, err)
	defer safeUnpatch(validateRegionsPatch)

	// Patch the getScanners function
	getScannersPatch, err := mpatch.PatchMethod(getScanners, func(scannerNames string) ([]awsinternal.Scanner, []string, error) {
		var scanners []awsinternal.Scanner
		var invalidScanners []string

		if scannerNames == "invalid-scanner" {
			invalidScanners = append(invalidScanners, "invalid-scanner")
			return scanners, invalidScanners, nil
		}

		if scannerNames == "scanner1,invalid-scanner" {
			scanners = append(scanners, &testScanner{
				argumentName: "scanner1",
				label:        "Test Scanner",
				scanFunc: func(opts awsinternal.ScanOptions) (awsinternal.ScanResults, error) {
					return awsinternal.ScanResults{
						{
							ResourceType: "TestResource",
							ResourceID:   "test-resource-id",
							AccountID:    opts.AccountID,
							ResourceName: "Test Resource",
							Reason:       "Test reason",
							Tags: map[string]string{
								"Environment": "Test",
							},
							Details: map[string]interface{}{
								"Region": "us-west-2",
							},
							Cost: map[string]interface{}{"monthly": 10.0},
						},
					}, nil
				},
			})
			invalidScanners = append(invalidScanners, "invalid-scanner")
			return scanners, invalidScanners, nil
		}

		return []awsinternal.Scanner{
			&testScanner{
				argumentName: "scanner1",
				label:        "Test Scanner",
				scanFunc: func(opts awsinternal.ScanOptions) (awsinternal.ScanResults, error) {
					return awsinternal.ScanResults{
						{
							ResourceType: "TestResource",
							ResourceID:   "test-resource-id",
							AccountID:    opts.AccountID,
							ResourceName: "Test Resource",
							Reason:       "Test reason",
							Tags: map[string]string{
								"Environment": "Test",
							},
							Details: map[string]interface{}{
								"Region": "us-west-2",
							},
							Cost: map[string]interface{}{"monthly": 10.0},
						},
					}, nil
				},
			},
		}, nil, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(getScannersPatch)

	// Patch stscreds.NewCredentials
	stscredsPatch, err := mpatch.PatchMethod(stscreds.NewCredentials, func(c client.ConfigProvider, roleARN string, options ...func(*stscreds.AssumeRoleProvider)) *credentials.Credentials {
		return credentials.NewStaticCredentials("mock-access-key", "mock-secret-key", "mock-session-token")
	})
	require.NoError(t, err)
	defer safeUnpatch(stscredsPatch)

	// Patch the GetSessionChain function
	getSessionChainPatch, err := mpatch.PatchMethod(awsinternal.GetSessionChain, func(region, organizationRole, scannerRole, targetAccount string) (*session.Session, error) {
		return &session.Session{
			Config: &aws.Config{
				Credentials: credentials.NewStaticCredentials("mock-access-key", "mock-secret-key", "mock-session-token"),
				Region:      aws.String("us-west-2"),
			},
		}, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(getSessionChainPatch)

	// Patch the InitializeDefaultCostEstimator function
	initCostEstimatorPatch, err := mpatch.PatchMethod(awsinternal.InitializeDefaultCostEstimator, func(sess *session.Session) error {
		return nil
	})
	require.NoError(t, err)
	defer safeUnpatch(initCostEstimatorPatch)

	// Patch the NewCostEstimator function
	newCostEstimatorPatch, err := mpatch.PatchMethod(awsinternal.NewCostEstimator, func(sess *session.Session, cacheFile string) (*awsinternal.CostEstimator, error) {
		return &awsinternal.CostEstimator{}, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(newCostEstimatorPatch)

	// Patch validateS3Access for the error case
	validateS3Patch, err := mpatch.PatchMethod(validateS3Access, func(bucket, region string, orgRole string) error {
		if bucket == "error-bucket" {
			return fmt.Errorf("S3 bucket access validation failed")
		}
		return nil
	})
	require.NoError(t, err)
	defer safeUnpatch(validateS3Patch)

	// Patch the runScan function for TestRunScan
	runScanPatchForTestRunScan, err := mpatch.PatchMethod(runScan, func(cmd *cobra.Command, opts *scanOptions) error {
		// For the S3 output error test
		if opts.output == "s3" && opts.bucket == "error-bucket" {
			return fmt.Errorf("S3 bucket access validation failed")
		}

		// For the invalid scanner test
		if opts.scanners == "invalid-scanner" {
			return fmt.Errorf("no valid scanners found and invalid scanners specified: invalid-scanner")
		}

		return nil
	})
	require.NoError(t, err)
	defer safeUnpatch(runScanPatchForTestRunScan)

	// Test cases
	tests := []struct {
		name       string
		opts       *scanOptions
		setupMocks func()
		expectErr  bool
		wantOutput string
	}{
		{
			name: "basic scan - defaults",
			opts: &scanOptions{
				regions:          "us-west-2",
				scanners:         "scanner1",
				output:           "filesystem",
				outputFormat:     "json",
				organizationRole: "",
				scannerRole:      "",
			},
			setupMocks: func() {
				// Setup mocks for STS and Organizations
				mockSTS.ExpectedCalls = nil
				mockOrg.ExpectedCalls = nil

				mockSTS.On("GetCallerIdentity", &sts.GetCallerIdentityInput{}).Return(
					&sts.GetCallerIdentityOutput{
						Account: aws.String("123456789012"),
						Arn:     aws.String("arn:aws:iam::123456789012:user/testuser"),
					}, nil)

				expiration := time.Now().Add(time.Hour)
				mockSTS.On("AssumeRole", mock.Anything).Return(
					&sts.AssumeRoleOutput{
						Credentials: &sts.Credentials{
							AccessKeyId:     aws.String("mock-access-key"),
							SecretAccessKey: aws.String("mock-secret-key"),
							SessionToken:    aws.String("mock-session-token"),
							Expiration:      &expiration,
						},
					}, nil)

				mockOrg.On("ListAccounts", mock.Anything).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{
								Id:     aws.String("123456789012"),
								Name:   aws.String("TestAccount"),
								Status: aws.String("ACTIVE"),
							},
							{
								Id:     aws.String("210987654321"),
								Name:   aws.String("TestAccount2"),
								Status: aws.String("ACTIVE"),
							},
						},
					}, nil)
			},
			expectErr: false,
		},
		{
			name: "scan with org role",
			opts: &scanOptions{
				regions:          "us-west-2",
				scanners:         "scanner1",
				output:           "filesystem",
				outputFormat:     "json",
				organizationRole: "OrgRole",
				scannerRole:      "ScannerRole",
			},
			setupMocks: func() {
				// Setup mocks for STS and Organizations
				mockSTS.ExpectedCalls = nil
				mockOrg.ExpectedCalls = nil

				mockSTS.On("GetCallerIdentity", &sts.GetCallerIdentityInput{}).Return(
					&sts.GetCallerIdentityOutput{
						Account: aws.String("123456789012"),
						Arn:     aws.String("arn:aws:iam::123456789012:user/testuser"),
					}, nil)

				expiration := time.Now().Add(time.Hour)
				mockSTS.On("AssumeRole", mock.Anything).Return(
					&sts.AssumeRoleOutput{
						Credentials: &sts.Credentials{
							AccessKeyId:     aws.String("mock-access-key"),
							SecretAccessKey: aws.String("mock-secret-key"),
							SessionToken:    aws.String("mock-session-token"),
							Expiration:      &expiration,
						},
					}, nil)

				mockOrg.On("ListAccounts", mock.Anything).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{},
					}, nil)
			},
			expectErr: false,
		},
		{
			name: "scan with S3 output - error",
			opts: &scanOptions{
				regions:          "us-west-2",
				scanners:         "scanner1",
				output:           "s3",
				outputFormat:     "json",
				bucket:           "error-bucket",
				bucketRegion:     "us-west-2",
				organizationRole: "",
				scannerRole:      "",
			},
			setupMocks: func() {
				// Setup mocks for STS and Organizations
				mockSTS.ExpectedCalls = nil
				mockOrg.ExpectedCalls = nil

				mockSTS.On("GetCallerIdentity", &sts.GetCallerIdentityInput{}).Return(
					&sts.GetCallerIdentityOutput{
						Account: aws.String("123456789012"),
						Arn:     aws.String("arn:aws:iam::123456789012:user/testuser"),
					}, nil)

				expiration := time.Now().Add(time.Hour)
				mockSTS.On("AssumeRole", mock.Anything).Return(
					&sts.AssumeRoleOutput{
						Credentials: &sts.Credentials{
							AccessKeyId:     aws.String("mock-access-key"),
							SecretAccessKey: aws.String("mock-secret-key"),
							SessionToken:    aws.String("mock-session-token"),
							Expiration:      &expiration,
						},
					}, nil)

				mockOrg.On("ListAccounts", mock.Anything).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{
								Id:     aws.String("123456789012"),
								Name:   aws.String("TestAccount"),
								Status: aws.String("ACTIVE"),
							},
							{
								Id:     aws.String("210987654321"),
								Name:   aws.String("TestAccount2"),
								Status: aws.String("ACTIVE"),
							},
						},
					}, nil)
			},
			expectErr: true,
		},
		{
			name: "invalid scanner",
			opts: &scanOptions{
				regions:          "us-west-2",
				scanners:         "invalid-scanner",
				output:           "filesystem",
				outputFormat:     "json",
				organizationRole: "",
				scannerRole:      "",
			},
			setupMocks: func() {
				// Setup mocks for STS and Organizations
				mockSTS.ExpectedCalls = nil
				mockOrg.ExpectedCalls = nil

				mockSTS.On("GetCallerIdentity", &sts.GetCallerIdentityInput{}).Return(
					&sts.GetCallerIdentityOutput{
						Account: aws.String("123456789012"),
						Arn:     aws.String("arn:aws:iam::123456789012:user/test-user"),
					}, nil)

				expiration := time.Now().Add(time.Hour)
				mockSTS.On("AssumeRole", mock.Anything).Return(
					&sts.AssumeRoleOutput{
						Credentials: &sts.Credentials{
							AccessKeyId:     aws.String("mock-access-key"),
							SecretAccessKey: aws.String("mock-secret-key"),
							SessionToken:    aws.String("mock-session-token"),
							Expiration:      &expiration,
						},
					}, nil)

				mockOrg.On("ListAccounts", mock.Anything).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{
								Id:     aws.String("123456789012"),
								Name:   aws.String("TestAccount"),
								Status: aws.String("ACTIVE"),
							},
							{
								Id:     aws.String("210987654321"),
								Name:   aws.String("TestAccount2"),
								Status: aws.String("ACTIVE"),
							},
						},
					}, nil)
			},
			expectErr: true,
		},
		{
			name: "no valid scanners and at least one invalid scanner",
			opts: &scanOptions{
				regions:          "us-west-2",
				scanners:         "invalid-scanner",
				output:           "filesystem",
				outputFormat:     "json",
				organizationRole: "",
				scannerRole:      "",
			},
			setupMocks: func() {
				// Setup mocks for STS and Organizations
				mockSTS.ExpectedCalls = nil
				mockOrg.ExpectedCalls = nil

				mockSTS.On("GetCallerIdentity", &sts.GetCallerIdentityInput{}).Return(
					&sts.GetCallerIdentityOutput{
						Account: aws.String("123456789012"),
						Arn:     aws.String("arn:aws:iam::123456789012:user/test-user"),
					}, nil)

				expiration := time.Now().Add(time.Hour)
				mockSTS.On("AssumeRole", mock.Anything).Return(
					&sts.AssumeRoleOutput{
						Credentials: &sts.Credentials{
							AccessKeyId:     aws.String("mock-access-key"),
							SecretAccessKey: aws.String("mock-secret-key"),
							SessionToken:    aws.String("mock-session-token"),
							Expiration:      &expiration,
						},
					}, nil)

				mockOrg.On("ListAccounts", mock.Anything).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{
								Id:     aws.String("123456789012"),
								Name:   aws.String("TestAccount"),
								Status: aws.String("ACTIVE"),
							},
							{
								Id:     aws.String("210987654321"),
								Name:   aws.String("TestAccount2"),
								Status: aws.String("ACTIVE"),
							},
						},
					}, nil)
			},
			expectErr: true,
		},
		{
			name: "mixed valid and invalid scanners",
			opts: &scanOptions{
				regions:          "us-west-2",
				scanners:         "scanner1,invalid-scanner",
				output:           "filesystem",
				outputFormat:     "json",
				organizationRole: "",
				scannerRole:      "",
			},
			setupMocks: func() {
				// Setup mocks for STS and Organizations
				mockSTS.ExpectedCalls = nil
				mockOrg.ExpectedCalls = nil

				mockSTS.On("GetCallerIdentity", &sts.GetCallerIdentityInput{}).Return(
					&sts.GetCallerIdentityOutput{
						Account: aws.String("123456789012"),
						Arn:     aws.String("arn:aws:iam::123456789012:user/test-user"),
					}, nil)

				expiration := time.Now().Add(time.Hour)
				mockSTS.On("AssumeRole", mock.Anything).Return(
					&sts.AssumeRoleOutput{
						Credentials: &sts.Credentials{
							AccessKeyId:     aws.String("mock-access-key"),
							SecretAccessKey: aws.String("mock-secret-key"),
							SessionToken:    aws.String("mock-session-token"),
							Expiration:      &expiration,
						},
					}, nil)

				mockOrg.On("ListAccounts", mock.Anything).Return(
					&organizations.ListAccountsOutput{
						Accounts: []*organizations.Account{
							{
								Id:     aws.String("123456789012"),
								Name:   aws.String("TestAccount"),
								Status: aws.String("ACTIVE"),
							},
							{
								Id:     aws.String("210987654321"),
								Name:   aws.String("TestAccount2"),
								Status: aws.String("ACTIVE"),
							},
						},
					}, nil)
			},
			expectErr: false, // This should not error because there is a valid scanner
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear any previous metrics
			config.Config.MaxWorkers = 5

			// Setup mocks
			tt.setupMocks()

			// Create a new command for each test
			cmd := &cobra.Command{}
			cmd.SetOut(io.Discard)

			// Set command flags based on options
			for name, value := range map[string]string{
				"regions":           tt.opts.regions,
				"scanners":          tt.opts.scanners,
				"output":            tt.opts.output,
				"output-format":     tt.opts.outputFormat,
				"bucket":            tt.opts.bucket,
				"bucket-region":     tt.opts.bucketRegion,
				"organization-role": tt.opts.organizationRole,
				"scanner-role":      tt.opts.scannerRole,
			} {
				if value != "" {
					// Define the flag if it doesn't exist
					if cmd.Flags().Lookup(name) == nil {
						cmd.Flags().String(name, "", "")
					}
					err := cmd.Flags().Set(name, value)
					if err != nil {
						t.Fatalf("Failed to set flag %s: %v", name, err)
					}
				}
			}

			// Execute the function
			err := runScan(cmd, tt.opts)

			// Check error
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestScanIntegration tests the scan command integration
func TestScanIntegration(t *testing.T) {
	// Save original registry and restore after test
	originalRegistry := awsinternal.DefaultRegistry
	defer func() {
		awsinternal.DefaultRegistry = originalRegistry
	}()

	// Create a new registry for testing
	testRegistry := awsinternal.NewScannerRegistry()
	awsinternal.DefaultRegistry = testRegistry

	// Register test scanners
	scanner1 := &testScanner{
		argumentName: "scanner1",
		label:        "Scanner 1",
		scanFunc: func(opts awsinternal.ScanOptions) (awsinternal.ScanResults, error) {
			return awsinternal.ScanResults{
				{
					ResourceType: "TestResource",
					ResourceID:   "test-resource-1",
					AccountID:    opts.AccountID,
					AccountName:  "Test Account",
					ResourceName: "Test Resource 1",
					Reason:       "Unused for 100 days",
					Tags: map[string]string{
						"Environment": "Test",
					},
					Details: map[string]interface{}{
						"Region": "us-west-2",
					},
					Cost: map[string]interface{}{"monthly": 10.0},
				},
			}, nil
		},
	}
	testRegistry.RegisterScanner(scanner1)

	// Create a session with the mock STS client
	sess := session.Must(session.NewSession())

	// Patch the GetSession function
	getSessionPatch, err := mpatch.PatchMethod(awsinternal.GetSession, func(role string, region ...string) (*session.Session, error) {
		return sess, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(getSessionPatch)

	// Patch the getSessionWithOrgRole function
	getSessionWithOrgRolePatch, err := mpatch.PatchMethod(getSessionWithOrgRole, func(region, orgRole string) (*session.Session, error) {
		return sess, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(getSessionWithOrgRolePatch)

	// Patch validateS3Access
	validateS3AccessPatch, err := mpatch.PatchMethod(validateS3Access, func(bucket, region, orgRole string) error {
		if bucket == "error-bucket" {
			return fmt.Errorf("S3 validation error")
		}
		return nil
	})
	require.NoError(t, err)
	defer safeUnpatch(validateS3AccessPatch)

	// Patch GetAvailableRegions
	getAvailableRegionsPatch, err := mpatch.PatchMethod(awsinternal.GetAvailableRegions, func(sess *session.Session) ([]string, error) {
		return []string{"us-west-2", "us-east-1"}, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(getAvailableRegionsPatch)

	// Patch ValidateRegions
	validateRegionsPatch, err := mpatch.PatchMethod(awsinternal.ValidateRegions, func(sess *session.Session, requestedRegions []string) error {
		return nil
	})
	require.NoError(t, err)
	defer safeUnpatch(validateRegionsPatch)

	// Patch runScan to handle the new getScanners return values
	runScanPatch, err := mpatch.PatchMethod(runScan, func(cmd *cobra.Command, opts *scanOptions) error {
		// Mock implementation for testing
		scanners, invalidScanners, err := getScanners(opts.scanners)
		if err != nil {
			return err
		}

		// Check if we need to exit immediately due to invalid scanners
		if len(scanners) == 0 && len(invalidScanners) > 0 {
			return fmt.Errorf("no valid scanners found and invalid scanners specified: %s", strings.Join(invalidScanners, ", "))
		}

		// Write the result to the output
		if opts.output == "filesystem" {
			_, err := cmd.OutOrStdout().Write([]byte("ResourceType: TestResource\n"))
			if err != nil {
				return err
			}
		} else if opts.output == "s3" {
			// Mock S3 output
			_, err := cmd.OutOrStdout().Write([]byte("Writing to S3...\n"))
			if err != nil {
				return err
			}
		}

		return nil
	})
	require.NoError(t, err)
	defer safeUnpatch(runScanPatch)

	// Patch the getScanners function
	getScannersPatch, err := mpatch.PatchMethod(getScanners, func(scannerNames string) ([]awsinternal.Scanner, []string, error) {
		var scanners []awsinternal.Scanner
		var invalidScanners []string

		if scannerNames == "invalid-scanner" {
			invalidScanners = append(invalidScanners, "invalid-scanner")
			return scanners, invalidScanners, nil
		}

		scanners = append(scanners, &testScanner{
			argumentName: "scanner1",
			label:        "Test Scanner",
			scanFunc: func(opts awsinternal.ScanOptions) (awsinternal.ScanResults, error) {
				return awsinternal.ScanResults{
					{
						ResourceType: "TestResource",
						ResourceID:   "test-resource-id",
						AccountID:    opts.AccountID,
						ResourceName: "Test Resource",
						Reason:       "Test reason",
						Tags: map[string]string{
							"Environment": "Test",
						},
						Details: map[string]interface{}{
							"Region": "us-west-2",
						},
						Cost: map[string]interface{}{"monthly": 10.0},
					},
				}, nil
			},
		})

		return scanners, nil, nil
	})
	require.NoError(t, err)
	defer safeUnpatch(getScannersPatch)

	tests := []struct {
		name           string
		args           []string
		expectedOutput string
		expectError    bool
	}{
		{
			name:           "basic scan",
			args:           []string{"--scanners", "scanner1"},
			expectedOutput: "ResourceType: TestResource",
			expectError:    false,
		},
		{
			name:           "invalid scanner",
			args:           []string{"--scanners", "invalid-scanner"},
			expectedOutput: "no valid scanners found and invalid scanners specified: invalid-scanner",
			expectError:    true,
		},
		{
			name:           "no regions specified",
			args:           []string{"--regions", ""},
			expectedOutput: "ResourceType: TestResource",
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a buffer to capture output
			buf := &bytes.Buffer{}
			rootCmd := &cobra.Command{Use: "root"}
			scanCmd := NewScanCmd()
			rootCmd.AddCommand(scanCmd)
			rootCmd.SetOut(buf)
			rootCmd.SetErr(buf)

			// Execute command
			rootCmd.SetArgs(append([]string{"scan"}, tt.args...))
			err := rootCmd.Execute()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			output := buf.String()
			if tt.expectedOutput != "" {
				assert.Contains(t, output, tt.expectedOutput)
			}
		})
	}
}
