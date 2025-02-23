# CloudSift: AWS Resource Scanner & Cost Optimizer

CloudSift is a powerful Go-based utility designed to scan AWS resources across multiple AWS Organizations, Accounts, and Regions. It helps identify unused resources and their associated costs, providing valuable insights to optimize your AWS environment and reduce cloud spending.

## Key Features

- **Multi-Account & Region Scanning**: Seamlessly scan multiple AWS organizations and regions to identify unused resources
- **Cost Analysis**: Get detailed cost breakdowns for unused resources to understand potential savings
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
  - Tag-based filtering
  - Cross-account role assumption
  - Regional endpoint optimization

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

# List AWS accounts
cloudsift list accounts

# Run a scan with all scanners
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
