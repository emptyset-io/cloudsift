package scanners

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	awslib "cloudsift/internal/aws"
	"cloudsift/internal/aws/utils"
	"cloudsift/internal/config"
	"cloudsift/internal/logging"
	"cloudsift/internal/worker"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// AMIScanner scans for unused AMIs
type AMIScanner struct{}

func init() {
	awslib.DefaultRegistry.RegisterScanner(&AMIScanner{})
}

// Name implements Scanner interface
func (s *AMIScanner) Name() string {
	return "amis"
}

// ArgumentName implements Scanner interface
func (s *AMIScanner) ArgumentName() string {
	return "amis"
}

// Label implements Scanner interface
func (s *AMIScanner) Label() string {
	return "AMIs"
}

// amiTask represents a single AMI to analyze
type amiTask struct {
	ami         *ec2.Image
	ec2Client   *ec2.EC2
	accountID   string
	region      string
	scanner     *AMIScanner
	opts        awslib.ScanOptions
	now         time.Time
	rateLimiter *awslib.RateLimiter
}

// processAMI analyzes a single AMI and returns a scan result if it's unused
func (t *amiTask) processAMI(ctx context.Context) (*awslib.ScanResult, error) {
	amiID := aws.StringValue(t.ami.ImageId)
	amiName := aws.StringValue(t.ami.Name)

	logging.Debug("Analyzing AMI", map[string]interface{}{
		"ami_id":   amiID,
		"ami_name": amiName,
	})

	// Check if AMI is in use by any EC2 instances
	if err := t.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait error: %w", err)
	}

	instances, err := t.ec2Client.DescribeInstancesWithContext(ctx, &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("image-id"),
				Values: []*string{t.ami.ImageId},
			},
		},
	})
	if err != nil {
		if strings.Contains(err.Error(), "Throttling:") {
			t.rateLimiter.OnFailure()
		}
		return nil, fmt.Errorf("failed to describe instances: %w", err)
	}
	t.rateLimiter.OnSuccess()

	// Count running instances using this AMI
	var runningInstances int
	for _, reservation := range instances.Reservations {
		for _, instance := range reservation.Instances {
			if aws.StringValue(instance.State.Name) == "running" {
				runningInstances++
			}
		}
	}

	// Skip if AMI is in use
	if runningInstances > 0 {
		return nil, nil
	}

	// Calculate age of AMI
	creationDate, err := time.Parse(time.RFC3339, aws.StringValue(t.ami.CreationDate))
	if err != nil {
		return nil, fmt.Errorf("failed to parse AMI creation date: %w", err)
	}

	age := t.now.Sub(creationDate)
	ageInDays := int(age.Hours() / 24)

	// Skip if AMI is not old enough
	if ageInDays < t.opts.DaysUnused {
		return nil, nil
	}

	// Get snapshot details for cost calculation
	var totalSnapshotSize int64
	var snapshotDetails []map[string]interface{}

	for _, blockDevice := range t.ami.BlockDeviceMappings {
		if blockDevice.Ebs != nil && blockDevice.Ebs.SnapshotId != nil {
			if err := t.rateLimiter.Wait(ctx); err != nil {
				return nil, fmt.Errorf("rate limit wait error: %w", err)
			}

			snapshot, err := t.ec2Client.DescribeSnapshotsWithContext(ctx, &ec2.DescribeSnapshotsInput{
				SnapshotIds: []*string{blockDevice.Ebs.SnapshotId},
			})
			if err != nil {
				if strings.Contains(err.Error(), "Throttling:") {
					t.rateLimiter.OnFailure()
				}
				return nil, fmt.Errorf("failed to describe snapshot: %w", err)
			}
			t.rateLimiter.OnSuccess()

			if len(snapshot.Snapshots) > 0 {
				snapshotSize := aws.Int64Value(snapshot.Snapshots[0].VolumeSize)
				totalSnapshotSize += snapshotSize

				snapshotDetails = append(snapshotDetails, map[string]interface{}{
					"snapshot_id":   aws.StringValue(blockDevice.Ebs.SnapshotId),
					"device_name":   aws.StringValue(blockDevice.DeviceName),
					"volume_size":   snapshotSize,
					"volume_type":   aws.StringValue(blockDevice.Ebs.VolumeType),
					"creation_date": aws.TimeValue(snapshot.Snapshots[0].StartTime),
				})
			}
		}
	}

	// Calculate costs using the cost estimator
	costEstimator := awslib.DefaultCostEstimator
	var costs *awslib.CostBreakdown
	if costEstimator != nil {
		costStart := time.Now()
		hoursRunning := t.now.Sub(creationDate).Hours()

		costs, err = costEstimator.CalculateCost(awslib.ResourceCostConfig{
			ResourceType:  "EBSSnapshots",
			ResourceSize: totalSnapshotSize,
			Region:       t.opts.Region,
			CreationTime: creationDate,
		})
		if err != nil {
			logging.Error("Failed to calculate costs", err, map[string]interface{}{
				"account_id":    t.accountID,
				"region":        t.opts.Region,
				"resource_name": amiName,
				"resource_id":   amiID,
				"duration_ms":   time.Since(costStart).Milliseconds(),
			})
		} else {
			logging.Debug("Cost calculation completed", map[string]interface{}{
				"resource_id": amiID,
				"duration_ms": time.Since(costStart).Milliseconds(),
			})

			// Calculate lifetime cost
			lifetime := costs.HourlyRate * hoursRunning
			costs.Lifetime = &lifetime
		}
	}

	// Convert AWS tags to map
	tags := make(map[string]string)
	for _, tag := range t.ami.Tags {
		tags[aws.StringValue(tag.Key)] = aws.StringValue(tag.Value)
	}

	details := map[string]interface{}{
		"ami": map[string]interface{}{
			"id":            amiID,
			"name":          amiName,
			"creation_date": creationDate,
			"description":   aws.StringValue(t.ami.Description),
			"platform":      aws.StringValue(t.ami.Platform),
			"architecture":  aws.StringValue(t.ami.Architecture),
			"state":         aws.StringValue(t.ami.State),
			"root_device":   aws.StringValue(t.ami.RootDeviceName),
			"age_days":      ageInDays,
		},
		"snapshots": snapshotDetails,
		"total_snapshot_size_gb": totalSnapshotSize,
	}

	// Get resource name from tags or use AMI name/ID
	resourceName := amiName
	if resourceName == "" {
		resourceName = amiID
	}
	if name, ok := tags["Name"]; ok {
		resourceName = name
	}

	reason := fmt.Sprintf("AMI has not been used by any instances for %d days and has %.2f GB in associated snapshots", 
		ageInDays, float64(totalSnapshotSize))

	return &awslib.ScanResult{
		ResourceType: t.scanner.Label(),
		ResourceName: resourceName,
		ResourceID:   amiID,
		AccountID:    t.accountID,
		Reason:       reason,
		Tags:         tags,
		Details:      details,
		Cost:         map[string]interface{}{"total": costs},
	}, nil
}

// calculateAgeString formats the time difference between now and a given time
func (s *AMIScanner) calculateAgeString(now time.Time, t *time.Time) string {
	if t == nil {
		return "unknown"
	}
	days := int(now.Sub(*t).Hours() / 24)
	if days < 30 {
		return fmt.Sprintf("%d days", days)
	}
	if days < 365 {
		return fmt.Sprintf("%d months", days/30)
	}
	years := days / 365
	remainingMonths := (days % 365) / 30
	if remainingMonths > 0 {
		return fmt.Sprintf("%dy%dm", years, remainingMonths)
	}
	return fmt.Sprintf("%dy", years)
}

// Scan implements Scanner interface
func (s *AMIScanner) Scan(opts awslib.ScanOptions) (awslib.ScanResults, error) {
	// Get regional session
	sess, err := awslib.GetSessionInRegion(opts.Session, opts.Region)
	if err != nil {
		logging.Error("Failed to create regional session", err, map[string]interface{}{
			"region": opts.Region,
		})
		return nil, fmt.Errorf("failed to create regional session: %w", err)
	}

	// Get current account ID
	accountID, err := utils.GetAccountID(sess)
	if err != nil {
		logging.Error("Failed to get caller identity", err, nil)
		return nil, fmt.Errorf("failed to get caller identity: %w", err)
	}

	// Create EC2 client
	ec2Client := ec2.New(sess)

	// Create rate limiter specific to this account/region
	rateLimiterKey := fmt.Sprintf("%s-%s-ami", accountID, opts.Region)
	rateConfig := &config.RateLimitConfig{
		RequestsPerSecond: 35.0,                   // EC2 API has higher rate limits
		MaxRetries:        10,                     // Keep retrying on throttling
		BaseDelay:         200 * time.Millisecond, // Start with higher base delay
		MaxDelay:          120 * time.Second,      // Keep 2 minute max delay
	}
	rateLimiter := awslib.GetGlobalRegistry().GetRateLimiter(rateLimiterKey, rateConfig)

	// Get the shared worker pool
	pool := worker.GetSharedPool()

	// Channel for errors
	errorChan := make(chan error, 1)

	// WaitGroup to track all submitted tasks
	var wg sync.WaitGroup

	// Process AMIs in chunks to avoid memory issues
	var results awslib.ScanResults
	var resultsMutex sync.Mutex

	ctx := context.Background()

	// Describe AMIs owned by this account
	input := &ec2.DescribeImagesInput{
		Owners: []*string{aws.String("self")},
	}

	if err := rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limit wait error: %w", err)
	}

	images, err := ec2Client.DescribeImagesWithContext(ctx, input)
	if err != nil {
		if strings.Contains(err.Error(), "Throttling:") {
			rateLimiter.OnFailure()
		}
		logging.Error("Failed to describe AMIs", err, nil)
		return nil, fmt.Errorf("failed to describe AMIs: %w", err)
	}
	rateLimiter.OnSuccess()

	// Process each AMI
	for _, ami := range images.Images {
		wg.Add(1)
		task := &amiTask{
			ami:         ami,
			ec2Client:   ec2Client,
			accountID:   accountID,
			region:      opts.Region,
			scanner:     s,
			opts:        opts,
			now:         time.Now(),
			rateLimiter: rateLimiter,
		}

		// Submit task to worker pool
		pool.Submit(func(ctx context.Context) error {
			defer wg.Done()

			result, err := task.processAMI(ctx)
			if err != nil {
				select {
				case errorChan <- err:
				default:
					logging.Error("Failed to process AMI", err, map[string]interface{}{
						"ami_id":   aws.StringValue(task.ami.ImageId),
						"account":  accountID,
						"region":   opts.Region,
					})
				}
				return err
			}

			if result != nil {
				resultsMutex.Lock()
				results = append(results, *result)
				resultsMutex.Unlock()
			}

			return nil
		})
	}

	// Wait for all tasks to complete
	wg.Wait()

	// Check for any errors
	select {
	case err := <-errorChan:
		return nil, fmt.Errorf("error processing AMIs: %w", err)
	default:
	}

	return results, nil
}
