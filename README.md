# CloudSift: AWS Resource Scanner & Cost Optimizer

CloudSift is a powerful Go-based utility designed to scan AWS resources across multiple AWS Organizations, Accounts, and Regions. It helps identify unused resources and their associated costs, providing valuable insights to optimize your AWS environment and reduce cloud spending.

## Key Features

- **Multi-Account & Region Scanning**: Seamlessly scan multiple AWS organizations and regions to identify unused resources
- **Real-Time Cost Analysis**: 
  - Live cost estimation using AWS Pricing API
  - Smart caching system to reduce API calls and improve performance
  - Detailed cost breakdowns for all resource types
  - Hourly, daily, monthly, and yearly cost projections
  - Resource lifetime cost calculations
  - Support for all AWS regions and pricing tiers
- **Intelligent Rate Limiting**:
  - Adaptive rate limiting with exponential backoff
  - Automatic request throttling to prevent API limits
  - Smart failure detection and recovery
  - Configurable limits and retry policies
- **High-Performance Worker Pool**:
  - I/O optimized worker allocation (default: CPU cores × 8)
  - Dynamic task distribution
  - Real-time performance metrics
  - Graceful shutdown handling
- **Resource Discovery**: Comprehensive scanning of various AWS services:
  - EC2 Instances
    - CPU and memory utilization analysis
    - Attached EBS volume tracking
    - Instance state monitoring
  - EBS Volumes & Snapshots
    - Unused volume detection
    - Orphaned snapshot identification
    - Cost optimization recommendations
  - Elastic IPs
    - Unattached IP detection
    - Usage patterns analysis
  - Load Balancers (ELB)
    - Classic and Application LB support
    - Traffic pattern analysis
    - Idle LB detection
  - RDS Instances
    - Database utilization metrics
    - Idle instance detection
  - IAM Users & Roles
    - Last access tracking
    - Unused credential detection
    - Service role analysis
  - Security Groups
    - Unused group detection
    - Rule analysis
  - VPCs
    - Resource utilization
    - Default VPC identification
  - DynamoDB Tables
    - Table usage metrics
    - Provisioned vs actual capacity
  - OpenSearch Domains
    - Cluster utilization
    - Resource optimization
- **HTML Report Generation**: 
  - Interactive, modern UI
  - Resource filtering and sorting
  - Cost breakdown charts
  - Detailed resource metadata
  - Action recommendations
- **Concurrent Scanning**: 
  - Efficient parallel scanning
  - Configurable worker pools
  - Real-time progress tracking
  - Resource-aware execution
- **Flexible Output**:
  - JSON output for programmatic processing
  - HTML reports for visualization
  - Text-based logging with multiple verbosity levels
  - Optional S3 output storage
- **Advanced Features**:
  - Custom scan intervals
  - Resource age analysis
  - Cost projection
  - Cross-account role assumption
  - Regional endpoint optimization

## Example Report

You can view an example of a CloudSift report [here](https://emptyset-io.github.io/cloudsift/examples/output/sample_report.html). This report showcases the various features and insights that CloudSift provides, including:

- Resource utilization metrics
- Cost analysis and potential savings
- Resource details and metadata
- Usage patterns and recommendations

Note: This example uses generated sample data and does not reflect real AWS resources or costs. It is intended to demonstrate the report format and features only.

## Cost Estimation

CloudSift includes a sophisticated cost estimation system that provides real-time cost analysis for AWS resources:

### Features

- **Live AWS Pricing**: Uses the AWS Pricing API to get current pricing information for all resource types
- **Intelligent Caching**: 
  - Caches pricing data locally to minimize API calls
  - Automatically refreshes stale cache entries
  - Persists across runs to maintain performance
- **Comprehensive Coverage**:
  - Supports all AWS regions and their specific pricing
  - Handles complex pricing models (e.g., EC2 instance types, EBS volume types)
  - Accounts for regional price variations
- **Cost Breakdowns**:
  - Hourly rates
  - Daily projections
  - Monthly estimates
  - Yearly forecasts
  - Lifetime costs based on resource creation time
- **Resource-Specific Calculations**:
  - EC2: Instance type and region-specific pricing
  - EBS: Volume type, size, and IOPS
  - RDS: Instance class and storage calculations
  - ELB: Load balancer type and data processing
  - DynamoDB: Provisioned capacity and storage
  - OpenSearch: Instance count and storage size

### Cache Management

The cost estimator maintains a local cache at `cache/costs.json` to optimize performance:

- Automatically creates and manages the cache directory
- Thread-safe cache access for concurrent operations
- Graceful handling of cache misses
- Periodic cache updates to maintain accuracy

## Rate Limiting

CloudSift implements an intelligent rate limiting system to handle AWS API requests efficiently:

### Features

- **Adaptive Rate Control**:
  - Default rate: 5 requests per second
  - Configurable requests per second
  - Token bucket algorithm implementation
  - Continuous token replenishment

- **Smart Backoff Strategy**:
  - Exponential backoff with configurable parameters
  - Base delay: 1 second
  - Maximum delay: 120 seconds
  - Maximum retries: 10 attempts
  - Automatic backoff reset after 5 minutes of success

- **Failure Handling**:
  - Automatic throttling on API failures
  - Progressive backoff on consecutive failures
  - Graceful recovery mechanisms
  - Detailed failure logging and metrics

### Configuration

The rate limiter can be customized through the configuration:

```go
type RateLimitConfig struct {
    RequestsPerSecond float64       // Default: 5.0
    MaxRetries       int           // Default: 10
    BaseDelay        time.Duration // Default: 1s
    MaxDelay         time.Duration // Default: 120s
}
```

See [internal/config/ratelimit.go](https://github.com/emptyset-io/cloudsift/blob/main/internal/config/ratelimit.go) for the complete rate limit configuration.

## Worker Pool

CloudSift uses a highly optimized worker pool system for concurrent operations:

### Features

- **Dynamic Scaling**:
  - Default workers: CPU cores × 8 (optimized for I/O-bound tasks)
  - Configurable maximum worker limit
  - Automatic worker management

- **Performance Metrics**:
  - Total tasks executed
  - Completed vs failed tasks
  - Current and peak worker counts
  - Average execution time
  - Total execution duration

- **Task Management**:
  - Buffered task queue
  - Priority task handling
  - Graceful shutdown
  - Task synchronization

### Performance Optimization

The worker pool is specifically designed for I/O-bound AWS API operations:

- Default worker count is set to 8 times the number of CPU cores
- This multiplier is optimal for I/O-bound tasks where workers spend most time waiting
- Users can adjust the worker count based on their specific needs and API limits
- Monitor the metrics to find the optimal worker count for your use case

### Configuration

The worker pool size can be configured using the `--max-workers` CLI flag. The default is set to 8 times the number of CPU cores since the application is I/O bound. Adjust this value based on your API limits and system capabilities.

## Prerequisites

### AWS Setup

1. **AWS Credentials**:  
   Configure your AWS credentials using `aws configure` or set up the `~/.aws/credentials` file.

2. **Required Roles** (for multi-account scanning):  
   - **Organization Role**: IAM role for querying organization-level resources.  
   - **Scanner Role**: IAM role in each account with read-only access to AWS resources.  

   **Note**: If supplying just an AWS profile, the scanner will operate in single-account mode, scanning only that account. In this case, the organization and scanner roles are optional.


### Role Permissions

#### Organization Role Permissions

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Action": "sts:AssumeRole",
            "Effect": "Allow",
            "Resource": [
                "arn:aws:iam::<account_id>:role/<scanner_role>"
            ]
        },
        {
            "Action": [
                "organizations:ListAccounts",
                "organizations:DescribeAccount",
                "ec2:DescribeRegions"
            ],
            "Effect": "Allow",
            "Resource": "*"
        }
    ]
}
```

#### Scanner Role Permissions

The scanner role requires the AWS-managed `ReadOnlyAccess` policy and the following trust relationship:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Principal": {
                "AWS": "arn:aws:iam::<organization_account_id>:role/<organization_role>"
            },
            "Action": "sts:AssumeRole"
        }
    ]
}
```

##### Optional S3 Permissions  
If you want to enable S3-based file output, the scanner role must also have permissions to read and write to an S3 bucket. The following policy grants the necessary access to `<bucket_name>`:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "s3:PutObject",
                "s3:GetObject",
                "s3:ListBucket",
                "s3:GetBucketLocation",
                "s3:DeleteObject",
                "s3:ListMultipartUploadParts",
                "s3:ListBucketMultipartUploads"
            ],
            "Resource": "arn:aws:s3:::<bucket_name>/*"
        },
        {
            "Effect": "Allow",
            "Action": "s3:ListBucket",
            "Resource": "arn:aws:s3:::<bucket_name>"
        }
    ]
}
```

These S3 permissions are optional and only required if you intend to upload scan results to S3.

## Installation

```bash
# Clone the repository
git clone https://github.com/emptyset-io/cloudsift.git
cd cloudsift

# Build the binary
make build

# Or build for all platforms
make build-all
```

## Usage

CloudSift provides a simple command-line interface:

```bash
# List available scanners
cloudsift list scanners

# List AWS accounts in single account mode
cloudsift list accounts

# List AWS accounts in organization mode
cloudsift list accounts --organization-role <org_role>

# Run a scan in single account mode
cloudsift scan

# Run a scan in organization mode with all scanners
cloudsift scan --organization-role <org_role> --scanner-role <scanner_role>

# Run specific scanners
cloudsift scan --organization-role <org_role> --scanner-role <scanner_role> --scanners ec2,ebs,s3

# Scan specific regions
cloudsift scan --organization-role <org_role> --scanner-role <scanner_role> --regions us-west-2,us-east-1

# Configure parallel scanning
cloudsift scan --max-workers 10 --organization-role <org_role> --scanner-role <scanner_role>
```

## Contributing

We welcome contributions! Please feel free to submit a Pull Request.

## License

This project is licensed under the **Mozilla Public License 2.0 (MPL-2.0)**. For more details, refer to the [license page](https://www.mozilla.org/MPL/2.0/).

### Commercial License

For full proprietary use, premium features, or additional support, contact us at **[support@cloudsift.io](mailto:support@cloudsift.io)** for commercial licensing options.

## Contact

For questions and support, please email support@cloudsift.io
