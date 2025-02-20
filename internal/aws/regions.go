package aws

import (
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// GetAvailableRegions returns a list of regions that are enabled for the account
func GetAvailableRegions(sess *session.Session) ([]string, error) {
	// Start with us-east-1 to get the region list
	svc := ec2.New(sess, aws.NewConfig().WithRegion("us-east-1"))
	
	input := &ec2.DescribeRegionsInput{
		AllRegions: aws.Bool(false), // Only get enabled regions
	}
	
	result, err := svc.DescribeRegions(input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe regions: %w", err)
	}

	var regions []string
	for _, region := range result.Regions {
		regions = append(regions, aws.StringValue(region.RegionName))
	}
	
	return regions, nil
}

// ValidateRegions checks if the provided regions are valid and enabled for the account
func ValidateRegions(sess *session.Session, requestedRegions []string) error {
	availableRegions, err := GetAvailableRegions(sess)
	if err != nil {
		return err
	}

	// Create a map for O(1) lookup
	regionMap := make(map[string]bool)
	for _, region := range availableRegions {
		regionMap[region] = true
	}

	// Check each requested region
	for _, region := range requestedRegions {
		if !regionMap[region] {
			return fmt.Errorf("region '%s' is not available in this account. Available regions: %s", 
				region, strings.Join(availableRegions, ", "))
		}
	}

	return nil
}
