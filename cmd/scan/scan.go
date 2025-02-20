package scan

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"cloudsift/internal/aws"
	"cloudsift/internal/config"
	"cloudsift/internal/logging"
	"cloudsift/internal/output"
	"cloudsift/internal/worker"

	"github.com/spf13/cobra"
)

type scanOptions struct {
	regions    string
	scanners   string
	format     string
	output     string
	bucket     string
	outputDir  string
	daysUnused int // Number of days a resource must be unused to be reported
}

// NewScanCmd creates the scan command
func NewScanCmd() *cobra.Command {
	opts := &scanOptions{}

	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Scan AWS resources",
		Long: `Scan AWS resources using the available scanners.
If --scanner-role is provided, scan will operate across all accounts in the organization.
Otherwise, it will only scan the current account.

Resources will be marked as unused if they have been in an off/unattached state
for longer than the specified --days-unused threshold (default 30 days).

If no scanners are specified via --scanners, all available scanners will be used.`,
		Example: `  # Scan all resources in current account using all scanners
  cloudsift scan

  # Scan across all accounts in organization
  cloudsift scan --scanner-role MyRole

  # Scan specific resources in specific regions
  cloudsift scan --regions us-east-1,us-west-2 --scanners ebs-volumes,ebs-snapshots

  # Output results to a specific directory (default is ./output)
  cloudsift scan --output filesystem --output-dir /path/to/dir

  # Output results to S3
  cloudsift scan --output s3 --bucket my-bucket

  # Set custom threshold for unused resources (default 30 days)
  cloudsift scan --days-unused 60`,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Validate output flags
			if opts.output != "" {
				switch output.Type(opts.output) {
				case output.FileSystem:
					// No additional validation needed
				case output.S3:
					if opts.bucket == "" {
						return fmt.Errorf("--bucket is required when --output=s3")
					}
				default:
					return fmt.Errorf("invalid output type: %s, must be 'filesystem' or 's3'", opts.output)
				}
			}
			// Validate days threshold
			if opts.daysUnused < 0 {
				return fmt.Errorf("--days-unused must be a positive number")
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(cmd, opts)
		},
	}

	// Add flags
	cmd.Flags().StringVar(&opts.regions, "regions", "", "Comma-separated list of regions to scan")
	cmd.Flags().StringVar(&opts.scanners, "scanners", "", "Comma-separated list of scanners to use")
	cmd.Flags().StringVar(&opts.format, "format", "text", "Output format (text or json)")
	cmd.Flags().StringVar(&opts.output, "output", "filesystem", "Output destination (filesystem or s3)")
	cmd.Flags().StringVar(&opts.outputDir, "output-dir", "", "Directory to write output files to (only used with --output=filesystem)")
	cmd.Flags().StringVar(&opts.bucket, "bucket", "", "S3 bucket name for output (required when --output=s3)")
	cmd.Flags().IntVar(&opts.daysUnused, "days-unused", 30, "Number of days a resource must be unused to be reported")

	return cmd
}

type scanResult struct {
	AccountID   string                     `json:"account_id"`
	AccountName string                     `json:"account_name"`
	Results     map[string]aws.ScanResults `json:"results"` // Map of scanner label to results
}

func getScanners(scannerList string) ([]aws.Scanner, error) {
	registry := aws.DefaultRegistry
	var scanners []aws.Scanner

	// If no scanners specified, use all available
	if scannerList == "" {
		for _, name := range registry.ListScanners() {
			scanner, err := registry.GetScanner(name)
			if err != nil {
				return nil, fmt.Errorf("failed to get scanner '%s': %w", name, err)
			}
			scanners = append(scanners, scanner)
		}
		return scanners, nil
	}

	// Get specified scanners
	requestedScanners := strings.Split(scannerList, ",")
	for _, name := range requestedScanners {
		scanner, err := registry.GetScanner(name)
		if err != nil {
			return nil, fmt.Errorf("invalid scanner '%s': %w", name, err)
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
	accounts, err := aws.ListAccounts()
	if err != nil {
		return fmt.Errorf("failed to list accounts: %w", err)
	}

	// Create base session for validation
	sess, err := aws.GetSession()
	if err != nil {
		return fmt.Errorf("failed to create AWS session: %w", err)
	}

	// Get and validate regions
	var regions []string
	if opts.regions != "" {
		regions = strings.Split(opts.regions, ",")
		if err := aws.ValidateRegions(sess, regions); err != nil {
			return err
		}
	} else {
		regions, err = aws.GetAvailableRegions(sess)
		if err != nil {
			return fmt.Errorf("failed to get available regions: %w", err)
		}
	}

	// Log scan start with configuration
	var scannerNames []string
	for _, s := range scanners {
		scannerNames = append(scannerNames, s.Name())
	}

	// Convert accounts to the format expected by the logger
	var accountInfo []logging.Account
	for _, acc := range accounts {
		accountInfo = append(accountInfo, logging.Account{
			ID:   acc.ID,
			Name: acc.Name,
		})
	}

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

	// Create tasks for each scanner+region combination
	var tasks []worker.Task
	var resultsMutex sync.Mutex

	for _, scanner := range scanners {
		for _, region := range regions {
			for _, account := range accounts {
				scanner := scanner // Create new variable for closure
				region := region
				account := account

				tasks = append(tasks, func(ctx context.Context) error {
					logging.ScannerStart(scanner.Name(), account.ID, account.Name, region)

					results, err := scanner.Scan(aws.ScanOptions{
						Region:     region,
						DaysUnused: opts.daysUnused,
					})
					if err != nil {
						logging.ScannerError(scanner.Name(), account.ID, account.Name, region, err)
						return err
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
					logging.ScannerComplete(scanner.Name(), account.ID, account.Name, region, resultInterfaces)

					return nil
				})
			}
		}
	}

	// Create and run worker pool
	pool := worker.NewPool(config.Config.MaxWorkers)
	pool.ExecuteTasks(tasks)

	// Create output writer
	writer := output.NewWriter(output.Config{
		Type:      output.Type(opts.output),
		S3Bucket:  opts.bucket,
		OutputDir: opts.outputDir,
	})

	// Write results for each account
	for _, result := range accountResults {
		if err := writer.Write(result.AccountID, result); err != nil {
			return fmt.Errorf("failed to write results for account %s: %w", result.AccountID, err)
		}
	}

	logging.ScanComplete(len(accountResults))
	return nil
}

type scanContext struct {
	AccountID   string          `json:"account_id"`
	AccountName string          `json:"account_name"`
	Region      string          `json:"region"`
	Results     aws.ScanResults `json:"results"`
}

func runScanNew(cmd *cobra.Command, opts *scanOptions) error {
	// Get and validate scanners
	scanners, err := getScanners(opts.scanners)
	if err != nil {
		return err
	}

	if len(scanners) == 0 {
		return fmt.Errorf("no scanners available")
	}

	// Get list of accounts to scan
	accounts, err := aws.ListAccounts()
	if err != nil {
		return fmt.Errorf("failed to list accounts: %w", err)
	}

	// Create base session for validation
	sess, err := aws.GetSession()
	if err != nil {
		return fmt.Errorf("failed to create AWS session: %w", err)
	}

	// Get and validate regions
	var regions []string
	if opts.regions != "" {
		regions = strings.Split(opts.regions, ",")
		if err := aws.ValidateRegions(sess, regions); err != nil {
			return err
		}
	} else {
		regions, err = aws.GetAvailableRegions(sess)
		if err != nil {
			return fmt.Errorf("failed to get available regions: %w", err)
		}
	}

	// Log scan start with configuration
	var scannerNames []string
	for _, s := range scanners {
		scannerNames = append(scannerNames, s.Name())
	}

	// Convert accounts to the format expected by the logger
	var accountInfo []logging.Account
	for _, acc := range accounts {
		// For now, we just use the account ID as both ID and Name
		// TODO: Add account alias/name resolution
		accountInfo = append(accountInfo, logging.Account{
			ID:   acc.ID,
			Name: acc.Name,
		})
	}

	logging.ScanStart(scannerNames, accountInfo, regions)

	// Create tasks for each scanner+region combination
	var tasks []worker.Task
	var resultsMutex sync.Mutex
	scanResults := make(map[string]map[string]aws.ScanResults) // accountID -> region -> results

	// Initialize results for each account and region
	for _, account := range accountInfo {
		scanResults[account.ID] = make(map[string]aws.ScanResults)
		for _, region := range regions {
			scanResults[account.ID][region] = aws.ScanResults{}
		}
	}

	for _, scanner := range scanners {
		for _, region := range regions {
			for _, account := range accountInfo {
				scanner := scanner // Create new variable for closure
				region := region
				account := account

				tasks = append(tasks, func(ctx context.Context) error {
					logging.ScannerStart(scanner.Name(), account.ID, account.Name, region)

					results, err := scanner.Scan(aws.ScanOptions{
						Region:     region,
						DaysUnused: opts.daysUnused,
					})
					if err != nil {
						logging.ScannerError(scanner.Name(), account.ID, account.Name, region, err)
						return err
					}

					// Safely append results
					resultsMutex.Lock()
					scanResults[account.ID][region] = append(scanResults[account.ID][region], results...)
					resultsMutex.Unlock()

					// Log completion with results
					resultInterfaces := make([]interface{}, len(results))
					for i, r := range results {
						resultInterfaces[i] = r
					}
					logging.ScannerComplete(scanner.Name(), account.ID, account.Name, region, resultInterfaces)

					return nil
				})
			}
		}
	}

	// Create and run worker pool
	pool := worker.NewPool(config.Config.MaxWorkers)
	pool.ExecuteTasks(tasks)

	// Create output writer
	writer := output.NewWriter(output.Config{
		Type:      output.Type(opts.output),
		S3Bucket:  opts.bucket,
		OutputDir: opts.outputDir,
	})

	// Write results for each account and region
	for accountID, regionResults := range scanResults {
		for region, results := range regionResults {
			if len(results) == 0 {
				continue // Skip empty results
			}

			// Create scan context
			ctx := &scanContext{
				AccountID:   accountID,
				AccountName: accountID, // TODO: Add account alias/name resolution
				Region:      region,
				Results:     results,
			}

			// Write results
			if err := writer.Write(accountID, ctx); err != nil {
				return fmt.Errorf("failed to write results for account %s in region %s: %w", accountID, region, err)
			}
		}
	}

	logging.ScanComplete(len(accountInfo))
	return nil
}
