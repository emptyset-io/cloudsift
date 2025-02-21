package scan

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"cloudsift/internal/aws"
	"cloudsift/internal/config"
	"cloudsift/internal/logging"
	"cloudsift/internal/output"
	"cloudsift/internal/output/html"
	"cloudsift/internal/worker"

	"github.com/spf13/cobra"
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

	// Get list of accounts to scan
	accounts, err := aws.ListAccounts(opts.organizationRole)
	if err != nil {
		return fmt.Errorf("failed to list accounts: %w", err)
	}

	// Get list of regions to scan
	regions, err := getRegionsToScan(opts)
	if err != nil {
		return fmt.Errorf("failed to get regions: %w", err)
	}

	// Initialize results map
	accountResults := make(map[string]*scanResult)
	for _, account := range accounts {
		accountResults[account.ID] = &scanResult{
			AccountID:   account.ID,
			AccountName: account.Name,
			Results:     make(map[string]aws.ScanResults),
		}
	}

	// Create tasks for each scanner+region combination
	var tasks []worker.Task
	var resultsMutex sync.Mutex

	for _, scanner := range scanners {
		// For IAM scanners, only scan in us-east-1 since IAM is a global service
		scanRegions := regions
		if scanner.Label() == "IAM Roles" || scanner.Label() == "IAM Users" {
			scanRegions = []string{"us-east-1"}
		}

		for _, region := range scanRegions {
			for _, account := range accounts {
				scanner := scanner // Create new variable for closure
				region := region
				account := account

				tasks = append(tasks, worker.Task(func(ctx context.Context) error {
					logging.ScannerStart(scanner.Label(), account.ID, account.Name, region)

					results, err := scanner.Scan(aws.ScanOptions{
						Region:     region,
						DaysUnused: opts.daysUnused,
						Role:       opts.scannerRole,
					})
					if err != nil {
						logging.ScannerError(scanner.Label(), account.ID, account.Name, region, err)
						return err
					}

					// Add account and region info to each result
					for i := range results {
						if results[i].Details == nil {
							results[i].Details = make(map[string]interface{})
						}
						results[i].Details["account_id"] = account.ID
						results[i].Details["account_name"] = account.Name
						if scanner.Label() == "IAM Roles" || scanner.Label() == "IAM Users" {
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
					logging.ScannerComplete(scanner.Label(), account.ID, account.Name, region, resultInterfaces)

					return nil
				}))
			}
		}
	}

	// Create and run worker pool
	pool := worker.NewPool(config.Config.MaxWorkers)
	pool.ExecuteTasks(tasks)

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

			// Convert map of scan results to slice
			var allResults []aws.ScanResult
			for _, accountResult := range accountResults {
				for _, scannerResults := range accountResult.Results {
					for _, result := range scannerResults {
						// Add account and region to details
						if result.Details == nil {
							result.Details = make(map[string]interface{})
						}
						result.Details["account_id"] = accountResult.AccountID
						result.Details["account_name"] = accountResult.AccountName
						allResults = append(allResults, result)
					}
				}
			}

			// Calculate scan metrics
			startTime := time.Now()
			totalScans := len(scanners) * len(regions) * len(accounts)
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
		case "s3":
			// TODO: Implement S3 output
			return fmt.Errorf("S3 output not yet implemented")
		}
	case "s3":
		// TODO: Implement S3 output
		return fmt.Errorf("S3 output not yet implemented")
	}

	logging.ScanComplete(len(accountResults))
	return nil
}

func getRegionsToScan(opts *scanOptions) ([]string, error) {
	// Create base session for validation
	sess, err := aws.GetSession(opts.organizationRole)
	if err != nil {
		return nil, fmt.Errorf("failed to create AWS session: %w", err)
	}

	var regions []string
	if opts.regions == "" {
		// If no regions specified, get all available regions
		regions, err = aws.GetAvailableRegions(sess)
		if err != nil {
			return nil, fmt.Errorf("failed to get available regions: %w", err)
		}
	} else {
		// Parse and validate comma-separated list of regions
		regions = strings.Split(opts.regions, ",")
		if err := aws.ValidateRegions(sess, regions); err != nil {
			return nil, fmt.Errorf("invalid regions: %w", err)
		}
	}
	return regions, nil
}
