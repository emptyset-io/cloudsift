package utils

// GetDynamoDBCost calculates the estimated monthly cost for a DynamoDB table
// This is a simplified calculation and may not reflect actual costs perfectly
func GetDynamoDBCost(tableSizeBytes int64, readCapacity, writeCapacity int64, region string) (float64, error) {
	// Convert table size to GB for pricing calculations
	tableSizeGB := float64(tableSizeBytes) / (1024 * 1024 * 1024)

	// Base storage cost per GB per month (approximate)
	// TODO: Get actual pricing from AWS Price List API
	storageCostPerGBMonth := 0.25

	// Calculate storage cost
	storageCost := tableSizeGB * storageCostPerGBMonth

	// Calculate provisioned capacity cost
	// TODO: Get actual pricing from AWS Price List API
	readCostPerUnit := 0.00013  // per hour
	writeCostPerUnit := 0.00065 // per hour

	readCapacityCost := float64(readCapacity) * readCostPerUnit * 24 * 30  // monthly
	writeCapacityCost := float64(writeCapacity) * writeCostPerUnit * 24 * 30 // monthly

	totalCost := storageCost + readCapacityCost + writeCapacityCost

	return totalCost, nil
}
