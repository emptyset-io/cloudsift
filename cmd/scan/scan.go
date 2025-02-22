package scan

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/spf13/cobra"

	"cloudsift/internal/aws"
	"cloudsift/internal/config"
	"cloudsift/internal/logging"
	"cloudsift/internal/output"
	"cloudsift/internal/output/html"
	"cloudsift/internal/worker"
)

type scanOptions struct {
	regions          string
	scanners         string
	output           string // filesystem or s3
	outputFormat     string // html or json
	bucket           string
	bucketRegion     string
	organizationRole string // Role to assume for listing organization accounts
	scannerRole      string // Role to assume for scanning accounts
	daysUnused       int    // Number of days a resource must be unused to be reported
}

// NewScanCmd creates the scan command
func NewScanCmd() *cobra.Command {
	opts := &scanOptions{}

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan AWS resources",
		Long: `Scan AWS resources for potential cost savings.

When no scanners or regions are specified, all available scanners will be run in all available regions.
When no organization-role is specified, only the current account will be scanned.
When both organization-role and scanner-role are specified, all accounts in the organization will be scanned.

Examples:
  # Scan all resources in all regions of current account
  cloudsift scan

  # Scan EBS volumes in us-west-2 of current account
  cloudsift scan --scanners ebs-volumes --regions us-west-2

  # Scan multiple resource types in multiple regions of all organization accounts
  cloudsift scan --scanners ebs-volumes,ebs-snapshots --regions us-west-2,us-east-1 --organization-role OrganizationAccessRole --scanner-role SecurityAuditRole

  # Output HTML report to S3
  cloudsift scan --output s3 --output-format html --bucket my-bucket --bucket-region us-west-2

  # Output JSON results to S3
  cloudsift scan --output s3 --output-format json --bucket my-bucket --bucket-region us-west-2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Validate output format
			switch opts.outputFormat {
			case "json", "html":
				// Valid formats
			default:
				return fmt.Errorf("invalid output format: %s", opts.outputFormat)
			}

			// Validate output type
			switch opts.output {
			case "filesystem", "s3":
				// Valid output types
			default:
				return fmt.Errorf("invalid output type: %s", opts.output)
			}

			// Validate S3 parameters
			if opts.output == "s3" {
				if opts.bucket == "" {
					return fmt.Errorf("--bucket is required when --output=s3")
				}
				if opts.bucketRegion == "" {
					return fmt.Errorf("--bucket-region is required when --output=s3")
				}
			}

			return runScan(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.regions, "regions", "", "Comma-separated list of regions to scan (default: all available regions)")
	cmd.Flags().StringVar(&opts.scanners, "scanners", "", "Comma-separated list of scanners to run (default: all available scanners)")
	cmd.Flags().StringVar(&opts.output, "output", "filesystem", "Output type (filesystem, s3)")
	cmd.Flags().StringVarP(&opts.outputFormat, "output-format", "o", "html", "Output format (json, html)")
	cmd.Flags().StringVar(&opts.bucket, "bucket", "", "S3 bucket name (required when --output=s3)")
	cmd.Flags().StringVar(&opts.bucketRegion, "bucket-region", "", "S3 bucket region (required when --output=s3)")
	cmd.Flags().StringVar(&opts.organizationRole, "organization-role", "", "Role to assume for listing organization accounts")
	cmd.Flags().StringVar(&opts.scannerRole, "scanner-role", "", "Role to assume for scanning accounts")
	cmd.Flags().IntVar(&opts.daysUnused, "days-unused", 90, "Number of days a resource must be unused to be reported")

	return cmd
}

type scanResult struct {
	AccountID   string                     `json:"account_id"`
	AccountName string                     `json:"account_name"`
	Results     map[string]aws.ScanResults `json:"results"` // Map of scanner name to results
}

// isIAMScanner returns true if the scanner is for IAM resources
func isIAMScanner(scanner aws.Scanner) bool {
	return scanner.Label() == "IAM Roles" || scanner.Label() == "IAM Users"
}

func getScanners(scannerList string) ([]aws.Scanner, error) {
	var scanners []aws.Scanner

	// If no scanners specified, get all available scanners
	if scannerList == "" {
		names := aws.DefaultRegistry.ListScanners()
		if len(names) == 0 {
			return nil, fmt.Errorf("no scanners available in registry")
		}

		for _, name := range names {
			scanner, err := aws.DefaultRegistry.GetScanner(name)
			if err != nil {
				return nil, fmt.Errorf("failed to get scanner '%s': %w", name, err)
			}
			scanners = append(scanners, scanner)
		}
		return scanners, nil
	}

	// Parse comma-separated list of scanners
	names := strings.Split(scannerList, ",")
	for _, name := range names {
		scanner, err := aws.DefaultRegistry.GetScanner(name)
		if err != nil {
			return nil, fmt.Errorf("failed to get scanner '%s': %w", name, err)
		}
		scanners = append(scanners, scanner)
	}

	return scanners, nil
}

func runScan(cmd *cobra.Command, opts *scanOptions) error {
	// Get and validate scanners
	scanners, err := getScanners(opts.scanners)
	if err != nil {
		return err
	}

	if len(scanners) == 0 {
		return fmt.Errorf("no scanners available")
	}

	// Get list of accounts to scan using organization role if provided
	accounts, err := aws.ListAccounts(opts.organizationRole)
	if err != nil {
		return fmt.Errorf("failed to list accounts: %w", err)
	}

	// Create base sessions for each account
	accountSessions := make(map[string]*session.Session)
	for _, account := range accounts {
		// Create session chain for scanning
		// If we have both roles, this will chain: profile -> org role -> scanner role
		scanSession, err := aws.GetSessionChain(opts.organizationRole, opts.scannerRole, "")
		if err != nil {
			return fmt.Errorf("failed to create session chain for account %s: %w", account.ID, err)
		}
		accountSessions[account.ID] = scanSession
	}

	// Get and validate regions
	var regions []string
	if opts.regions == "" {
		// If no regions specified, get all available regions
		regions, err = aws.GetAvailableRegions(accountSessions[accounts[0].ID])
		if err != nil {
			return fmt.Errorf("failed to get available regions: %w", err)
		}
	} else {
		// Parse and validate comma-separated list of regions
		regions = strings.Split(opts.regions, ",")
		if err := aws.ValidateRegions(accountSessions[accounts[0].ID], regions); err != nil {
			return fmt.Errorf("invalid regions: %w", err)
		}
	}

	// Log scan start with configuration and start timing
	var scannerNames []string
	for _, s := range scanners {
		scannerNames = append(scannerNames, s.Label())
	}

	// Convert accounts to the format expected by the logger
	var accountInfo []logging.Account
	for _, acc := range accounts {
		accountInfo = append(accountInfo, logging.Account{
			ID:   acc.ID,
			Name: acc.Name,
		})
	}

	startTime := time.Now()
	logging.ScanStart(scannerNames, accountInfo, regions)

	// Initialize results map
	accountResults := make(map[string]*scanResult)
	for _, account := range accounts {
		accountResults[account.ID] = &scanResult{
			AccountID:   account.ID,
			AccountName: account.Name,
			Results:     make(map[string]aws.ScanResults),
		}
	}

	// Create tasks for each scanner+region+account combination
	var tasks []worker.Task
	var resultsMutex sync.Mutex

	for _, scanner := range scanners {
		// For IAM scanners, we only need to scan us-east-1 since IAM is global
		scanRegions := regions
		if isIAMScanner(scanner) {
			scanRegions = []string{"us-east-1"}
		}

		for _, region := range scanRegions {
			for _, account := range accounts {
				scanner := scanner // Create new variable for closure
				region := region
				account := account

				tasks = append(tasks, worker.Task(func(ctx context.Context) error {
					// For IAM scanners, always log region as "global"
					logRegion := region
					if isIAMScanner(scanner) {
						logRegion = "global"
					}
					logging.ScannerStart(scanner.Label(), account.ID, account.Name, logRegion)

					// Get the account's base session and create regional session
					scanSession := accountSessions[account.ID]
					regionSession, err := aws.GetSessionInRegion(scanSession, region)
					if err != nil {
						logging.ScannerError(scanner.Label(), account.ID, account.Name, logRegion, err)
						return fmt.Errorf("failed to create regional session for account %s: %w", account.ID, err)
					}

					results, err := scanner.Scan(aws.ScanOptions{
						Region:     region,
						DaysUnused: opts.daysUnused,
						Session:    regionSession,
					})
					if err != nil {
						logging.ScannerError(scanner.Label(), account.ID, account.Name, logRegion, err)
						return err
					}

					// Add account and region info to each result
					for i := range results {
						if results[i].Details == nil {
							results[i].Details = make(map[string]interface{})
						}
						results[i].Details["account_id"] = account.ID
						// For IAM scanners, set region as "global", otherwise use actual region
						if isIAMScanner(scanner) {
							results[i].Details["region"] = "global"
						} else {
							results[i].Details["region"] = region
						}
					}

					// Safely append results
					resultsMutex.Lock()
					if accountResults[account.ID].Results[scanner.Label()] == nil {
						accountResults[account.ID].Results[scanner.Label()] = results
					} else {
						accountResults[account.ID].Results[scanner.Label()] = append(accountResults[account.ID].Results[scanner.Label()], results...)
					}
					resultsMutex.Unlock()

					// Log completion with results
					resultInterfaces := make([]interface{}, len(results))
					for i, r := range results {
						resultInterfaces[i] = r
					}
					logging.ScannerComplete(scanner.Label(), account.ID, account.Name, logRegion, resultInterfaces)

					return nil
				}))
			}
		}
	}

	// Create and run worker pool
	pool := worker.NewPool(config.Config.MaxWorkers)
	pool.ExecuteTasks(tasks)

	// Calculate total scans for metrics
	totalScans := 0
	for _, scanner := range scanners {
		if isIAMScanner(scanner) {
			totalScans += len(accounts) // One scan per account for IAM
		} else {
			totalScans += len(accounts) * len(regions) // One scan per account per region for others
		}
	}

	// Output results
	switch opts.output {
	case "filesystem":
		switch opts.outputFormat {
		case "json":
			// Use writer for JSON filesystem output
			writer := output.NewWriter(output.Config{
				Type:      output.FileSystem,
				OutputDir: "output",
			})

			for accountID, result := range accountResults {
				if err := writer.Write(accountID, result); err != nil {
					return fmt.Errorf("error writing results for account %s: %v", accountID, err)
				}
			}
		case "html":
			// Create reports directory if it doesn't exist
			if err := os.MkdirAll("reports", 0755); err != nil {
				return fmt.Errorf("error creating reports directory: %v", err)
			}

			// Collect all results
			var allResults []aws.ScanResult
			for _, accountResult := range accountResults {
				for _, scannerResults := range accountResult.Results {
					allResults = append(allResults, scannerResults...)
				}
			}

			// Calculate scan metrics
			duration := time.Since(startTime).Seconds()
			metrics := html.ScanMetrics{
				TotalScans:        totalScans,
				TotalRunTime:      duration,
				AvgScansPerSecond: float64(totalScans) / duration,
			}

			outputPath := "reports/scan_report.html"
			if err := html.WriteHTML(allResults, outputPath, metrics); err != nil {
				return fmt.Errorf("error writing HTML output: %v", err)
			}
			fmt.Printf("HTML report written to %s\n", outputPath)
		}
	case "s3":
		// TODO: Implement S3 output
		return fmt.Errorf("S3 output not yet implemented")
	}

	logging.ScanComplete(len(accountResults))
	return nil
}
