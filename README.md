# CloudSift

<div align="center">

**AWS Resource Scanner & Cost Optimizer**

*Scan AWS resources across multiple Organizations, Accounts, and Regions to identify unused resources and optimize costs.*

[View Demo](https://emptyset-io.github.io/cloudsift/examples/output/sample_report.html) •
[Documentation](#documentation) •
[Installation](#installation) •
[Usage](#usage)

</div>

## 📋 Table of Contents

- [Overview](#overview)
- [Features](#features)
  - [Resource Discovery](#resource-discovery)
  - [Cost Analysis](#cost-analysis)
  - [Performance & Scalability](#performance--scalability)
  - [Output & Reporting](#output--reporting)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
  - [Basic Usage](#basic-usage)
- [Documentation](#documentation)
  - [Cost Estimation System](#cost-estimation-system)
  - [Rate Limiting](#rate-limiting)
  - [Worker Pool Architecture](#worker-pool-architecture)
- [Example Report](#example-report)
- [AWS Permissions](#aws-permissions)
- [Contributing](#contributing)
- [License](#license)
- [Contact](#contact)

## Overview

CloudSift is a powerful Go-based utility that helps organizations optimize their AWS infrastructure and reduce cloud spending. By scanning multiple AWS Organizations, Accounts, and Regions, it provides comprehensive insights into resource utilization and costs.

## Features

### Resource Discovery

CloudSift performs comprehensive scanning across various AWS services:

#### Compute & Storage
- **EC2 Instances**
  - CPU and memory utilization analysis
  - Attached EBS volume tracking
  - Instance state monitoring
- **EBS Volumes & Snapshots**
  - Unused volume detection
  - Orphaned snapshot identification
  - Cost optimization recommendations
- **RDS Instances**
  - Database utilization metrics
  - Idle instance detection

#### Networking
- **Elastic IPs**
  - Unattached IP detection
  - Usage patterns analysis
- **Load Balancers (ELB)**
  - Classic and Application LB support
  - Traffic pattern analysis
  - Idle LB detection
- **VPCs**
  - Resource utilization
  - Default VPC identification
- **Security Groups**
  - Unused group detection
  - Rule analysis

#### Identity & Database
- **IAM Users & Roles**
  - Last access tracking
  - Unused credential detection
  - Service role analysis
- **DynamoDB Tables**
  - Table usage metrics
  - Provisioned vs actual capacity
- **OpenSearch Domains**
  - Cluster utilization
  - Resource optimization

### Cost Analysis

CloudSift includes a sophisticated real-time cost analysis system:

- **Live Cost Estimation**
  - AWS Pricing API integration
  - Smart caching system
  - Detailed cost breakdowns
  - Multiple time period projections
  - Resource lifetime calculations
  - Support for all AWS regions and pricing tiers

### Performance & Scalability

- **Intelligent Rate Limiting**
  - Adaptive rate limiting with exponential backoff
  - Automatic request throttling
  - Smart failure detection and recovery
  - Configurable limits and retry policies

- **High-Performance Worker Pool**
  - I/O optimized worker allocation
  - Dynamic task distribution
  - Real-time performance metrics
  - Graceful shutdown handling

### Output & Reporting

- **HTML Reports**
  - Interactive, modern UI
  - Resource filtering and sorting
  - Cost breakdown charts
  - Detailed resource metadata
  - Action recommendations

- **Flexible Output Options**
  - JSON for programmatic processing
  - Text-based logging with multiple verbosity levels
  - Optional S3 output storage

## Getting Started

### Prerequisites

1. **AWS Credentials**:  
   Configure your AWS credentials using `aws configure` or set up the `~/.aws/credentials` file.

2. **Required Roles** (for multi-account scanning):  
   - **Organization Role**: IAM role for querying organization-level resources.  
   - **Scanner Role**: IAM role in each account with read-only access to AWS resources.  

   **Note**: If supplying just an AWS profile, the scanner will operate in single-account mode, scanning only that account. In this case, the organization and scanner roles are optional.

### Installation

```bash
# Clone the repository
git clone https://github.com/emptyset-io/cloudsift.git
cd cloudsift

# Build the binary
make build

# Or build for all platforms
make build-all
```

### Basic Usage

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

## Usage

CloudSift provides a comprehensive command-line interface with several commands for managing and inspecting AWS resources.

### Global Flags

These flags are available for all commands:

```bash
--log-format string          # Log output format (text or json) (default "text")
--log-level string          # Set logging level (DEBUG, INFO, WARN, ERROR) (default "INFO")
--max-workers int           # Maximum number of concurrent workers (default 12)
--organization-role string  # Role name to assume for organization-wide operations
--profile, -p string       # AWS profile to use (supports SSO profiles) (default "default")
--scanner-role string      # Role name to assume for scanning operations
```

### Scan Command

The `scan` command is used to scan AWS resources for potential cost savings. It supports scanning multiple resource types across multiple regions and accounts.

```bash
cloudsift scan [flags]

# Flags:
--bucket string          # S3 bucket name (required when --output=s3)
--bucket-region string   # S3 bucket region (required when --output=s3)
--days-unused int        # Number of days a resource must be unused to be reported (default 90)
--output string          # Output type (filesystem, s3) (default "filesystem")
--output-format, -o string # Output format (json, html) (default "html")
--regions string         # Comma-separated list of regions to scan (default: all available regions)
--scanners string        # Comma-separated list of scanners to run (default: all available scanners)
```

#### Scan Examples

```bash
# Scan all resources in all regions of current account
cloudsift scan

# Scan EBS volumes in us-west-2 of current account
cloudsift scan --scanners ebs-volumes --regions us-west-2

# Scan multiple resource types in multiple regions of all organization accounts
cloudsift scan --scanners ebs-volumes,ebs-snapshots \
               --regions us-west-2,us-east-1 \
               --organization-role OrganizationAccessRole \
               --scanner-role SecurityAuditRole

# Output HTML report to S3
cloudsift scan --output s3 --output-format html \
               --bucket my-bucket --bucket-region us-west-2

# Output JSON results to S3
cloudsift scan --output s3 --output-format json \
               --bucket my-bucket --bucket-region us-west-2
```

### List Command

The `list` command provides information about various AWS resources and configurations.

#### List Accounts
```bash
cloudsift list accounts [flags]

# Examples:
# List current account
cloudsift list accounts

# List all accounts in organization
cloudsift list accounts --organization-role OrganizationAccessRole
```

#### List Profiles
```bash
cloudsift list profiles

# Lists all available AWS credential profiles from the AWS credentials 
# and config files
```

#### List Scanners
```bash
cloudsift list scanners

# Lists all available resource scanners that can be used to scan 
# AWS resources
```

### Version Command

```bash
cloudsift version

# Displays version information including:
# - Version number
# - Git commit hash
# - Build time
# - Go version
```

### Advanced Usage

1. **Multi-Account Scanning**
   - Use `--organization-role` to specify the role for organization-wide operations
   - Use `--scanner-role` to specify the role for scanning individual accounts
   - When both roles are specified, all accounts in the organization will be scanned

2. **Output Options**
   - Default output is HTML format to filesystem
   - Support for JSON output format
   - Optional S3 output with bucket configuration
   - Configurable unused resource threshold (default 90 days)

3. **Performance Tuning**
   - Adjust `--max-workers` for concurrent operations (default: 12)
   - Configure logging level and format for debugging
   - Region-specific scanning for focused analysis

## Documentation

### Cost Estimation System

The cost estimation system provides real-time analysis using the AWS Pricing API:

#### Features
- Live AWS Pricing with intelligent caching
- Comprehensive coverage of all AWS regions
- Detailed cost breakdowns (hourly/daily/monthly/yearly)
- Resource-specific calculations

#### Cache Management
- Location: `cache/costs.json`
- Thread-safe concurrent operations
- Automatic cache maintenance
- Graceful handling of cache misses

### Rate Limiting

CloudSift implements an intelligent rate limiting system:

#### Core Features
- Adaptive rate control (default: 5 RPS)
- Smart backoff strategy
- Comprehensive failure handling
- Detailed metrics and logging

#### Configuration
```go
type RateLimitConfig struct {
    RequestsPerSecond float64       // Default: 5.0
    MaxRetries       int           // Default: 10
    BaseDelay        time.Duration // Default: 1s
    MaxDelay         time.Duration // Default: 120s
}
```

### Worker Pool Architecture

The worker pool system is optimized for concurrent AWS API operations:

#### Features
- Dynamic scaling (CPU cores × 8)
- Comprehensive performance metrics
- Efficient task management
- Graceful shutdown handling

#### Performance Optimization
- Optimized for I/O-bound operations
- Configurable worker limits
- Built-in task prioritization

## Example Report

View a sample CloudSift report [here](https://emptyset-io.github.io/cloudsift/examples/output/sample_report.html). This demonstration showcases:
- Resource utilization metrics
- Cost analysis and potential savings
- Resource details and metadata
- Usage patterns and recommendations

*Note: The example uses generated sample data and does not reflect real AWS resources or costs.*

## AWS Permissions

### Organization Role Permissions

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

### Scanner Role Permissions

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
If you want to enable S3-based file output, the organization role must also have permissions to read and write to an S3 bucket. The following policy grants the necessary access to `<bucket_name>`:

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

## Contributing

We welcome contributions! Please feel free to submit a Pull Request.

## License

This project is licensed under the **Mozilla Public License 2.0 (MPL-2.0)**. For more details, refer to the [license page](https://www.mozilla.org/MPL/2.0/).

### Commercial License

For full proprietary use, premium features, or additional support, contact us at **[support@cloudsift.io](mailto:support@cloudsift.io)** for commercial licensing options.

## Contact

For questions and support, please email support@cloudsift.io
