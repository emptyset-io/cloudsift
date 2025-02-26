<!-- PROJECT LOGO -->
<br />
<div align="center">
  <a href="https://github.com/othneildrew/Best-README-Template">
    <img src="https://cloudsift.io/_astro/logo.DKmiXdWf_Z25aT8Y.svg" alt="Lencelot the Cloudsift Logo" height="100">
  </a>

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
  - [Multi-Account Setup](#multi-account-setup)
    - [Manual Setup](#manual-setup)
    - [Automated CloudFormation Setup](#automated-cloudformation-setup)
  - [Installation](#installation)
  - [Basic Usage](#basic-usage)
- [Documentation](#documentation)
  - [Cost Estimation System](#cost-estimation-system)
  - [Rate Limiting](#rate-limiting)
  - [Worker Pool Architecture](#worker-pool-architecture)
  - [Configuration](#configuration)
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

### Multi-Account Setup

CloudSift can operate in either single-account or multi-account mode:
- **Single-Account Mode**: Only requires AWS credentials with appropriate permissions
- **Multi-Account Mode**: Requires organization roles and an S3 bucket for storing results

Choose one of the following setup methods for multi-account scanning:

#### Manual Setup

If you prefer to set up the infrastructure manually or need customization, create:
- An Organization Role with permissions to list AWS Organization resources
- A Scanner Role that can be assumed in member accounts
- An S3 bucket with server-side encryption for storing scan results

See [AWS Permissions](#aws-permissions) for detailed IAM policy requirements.

#### Automated CloudFormation Setup

For convenience, we provide a CloudFormation template that automatically sets up all required infrastructure. This is entirely optional and only needed for multi-account scanning.

⚠️ **Important**: Deploy this template in your AWS Organization's management account.

[![Launch Stack](https://s3.amazonaws.com/cloudformation-examples/cloudformation-launch-stack.png)](https://console.aws.amazon.com/cloudformation/home?#/stacks/new?templateURL=https://cloudsift-development-public.s3.us-west-2.amazonaws.com/deployment-v1/aws/cloudsift-cloudformation-organization.json&stackName=cloudsift)

The template creates:
- Organization Role: For querying organization-level resources
- Scanner Role: For reading resources in member accounts
- S3 Bucket: For storing scan results with server-side encryption
   
After deployment, note these outputs for use with CloudSift:
- `OrganizationRole`: Use with `--organization-role`
- `ScannerRole`: Use with `--scanner-role`
- `BucketName`: Use with `--bucket`
- `BucketRegion`: Use with `--bucket-region`

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

### Configuration

CloudSift can be configured using either a YAML configuration file or environment variables. To get started quickly, use the `init` command to create default configuration files:

```bash
# Create a default config.yaml in the current directory
cloudsift init config

# Create a default .env file in the current directory
cloudsift init env

# Create config files in custom locations
cloudsift init config --output /path/to/config.yaml
cloudsift init env --output /path/to/.env

# Force overwrite existing files
cloudsift init config --force
cloudsift init env --force
```

The generated files will contain all available configuration options with helpful comments explaining each setting.

For more details on configuration options, see the [Configuration](#configuration) section.

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

### Basic Usage

```bash
# Scan all resources in all regions
cloudsift scan

# Scan specific resources in specific regions
cloudsift scan --regions us-west-2,us-east-1 --scanners ebs-volumes,ec2-instances

# Output results to S3
cloudsift scan --output s3 --bucket my-bucket --bucket-region us-west-2

# Use a specific AWS profile and roles
cloudsift scan --profile prod --organization-role OrgRole --scanner-role ScanRole

# Ignore specific resources (case-insensitive)
cloudsift scan \
  --ignore-resource-ids i-1234567890abcdef0,vol-0987654321fedcba \
  --ignore-resource-names prod-server,backup-volume \
  --ignore-tags Environment=production,KeepAlive=true
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
--ignore-resource-ids string    # Comma-separated list of resource IDs to ignore (case-insensitive)
--ignore-resource-names string  # Comma-separated list of resource names to ignore (case-insensitive)
--ignore-tags string           # Comma-separated list of tags to ignore in KEY=VALUE format (case-insensitive)
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
               --scanner-role ScannerAccessRole

# Output HTML report to S3
cloudsift scan --output s3 --output-format html \
               --bucket my-bucket --bucket-region us-west-2

# Output JSON results to S3
cloudsift scan --output s3 --output-format json \
               --bucket my-bucket --bucket-region us-west-2

# Ignore specific resources (case-insensitive matching)
cloudsift scan --ignore-resource-ids i-1234567890abcdef0,vol-0987654321fedcba \
               --ignore-resource-names prod-server,backup-volume \
               --ignore-tags "Environment=production,KeepAlive=true"
```

### Configuration

CloudSift can be configured through environment variables or a configuration file. Environment variables take precedence over configuration file settings.

#### Environment Variables

All configuration options can be set via environment variables with the `CLOUDSIFT_` prefix:

| Environment Variable | Description | Default |
|---------------------|-------------|---------|
| `CLOUDSIFT_AWS_PROFILE` | AWS profile to use | `default` |
| `CLOUDSIFT_AWS_ORGANIZATION_ROLE` | Role for organization access | `""` |
| `CLOUDSIFT_AWS_SCANNER_ROLE` | Role for scanning accounts | `""` |
| `CLOUDSIFT_APP_LOG_FORMAT` | Log format (text/json) | `text` |
| `CLOUDSIFT_APP_LOG_LEVEL` | Log level (DEBUG/INFO/WARN/ERROR) | `INFO` |
| `CLOUDSIFT_SCAN_REGIONS` | Comma-separated list of regions | `""` (all regions) |
| `CLOUDSIFT_SCAN_SCANNERS` | Comma-separated list of scanners | `""` (all scanners) |
| `CLOUDSIFT_SCAN_OUTPUT` | Output type (filesystem/s3) | `filesystem` |
| `CLOUDSIFT_SCAN_OUTPUT_FORMAT` | Output format (json/html) | `html` |
| `CLOUDSIFT_SCAN_BUCKET` | S3 bucket for output | `""` |
| `CLOUDSIFT_SCAN_BUCKET_REGION` | S3 bucket region | `""` |
| `CLOUDSIFT_SCAN_DAYS_UNUSED` | Days threshold for unused resources | `90` |
| `CLOUDSIFT_SCAN_IGNORE_RESOURCE_IDS` | Resource IDs to ignore (case-insensitive) | `""` |
| `CLOUDSIFT_SCAN_IGNORE_RESOURCE_NAMES` | Resource names to ignore (case-insensitive) | `""` |
| `CLOUDSIFT_SCAN_IGNORE_TAGS` | Tags to ignore in KEY=VALUE format (case-insensitive) | `""` |

#### Configuration File

The `config.yaml` file can be placed in the following locations (in order of precedence):
1. Current directory (`./config.yaml`)
2. User's home directory (`$HOME/.cloudsift/config.yaml`)
3. System-wide directory (`/etc/cloudsift/config.yaml`)

Example configuration file:

```yaml
aws:
  profile: prod
  organization_role: OrganizationAccessRole
  scanner_role: SecurityAuditRole

app:
  log_format: text
  log_level: INFO
  max_workers: 8

scan:
  regions:
    - us-west-2
    - us-east-1
  scanners:
    - ebs-volumes
    - ec2-instances
  output: filesystem
  output_format: html
  bucket: ""
  bucket_region: ""
  days_unused: 90
  
  # Ignore list configuration (all case-insensitive)
  ignore:
    resource_ids:
      - i-1234567890abcdef0     # Will match "I-1234567890ABCDEF0"
      - vol-0987654321fedcba    # Will match "VOL-0987654321FEDCBA"
    
    resource_names:
      - prod-server-01          # Will match "PROD-SERVER-01"
      - backup-volume-02        # Will match "Backup-Volume-02"
    
    tags:                       # Both keys and values are case-insensitive
      Environment: production   # Will match "ENVIRONMENT: PRODUCTION"
      KeepAlive: "true"        # Will match "keepalive: TRUE"
      Project: critical        # Will match "PROJECT: CRITICAL"
    
    regions:
      - us-west-1             # Regions to ignore
      - eu-west-1
```

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
