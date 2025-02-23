#!/usr/bin/env python3

import json
import random
import datetime
import uuid
import os

def generate_resource_name(resource_type):
    """Generate a descriptive name for a resource."""
    if resource_type == "EC2 Instance":
        return f"app-server"
    elif resource_type == "RDS Instance":
        engines = ["mysql", "postgres", "aurora-postgresql"]
        engine = random.choice(engines)
        return f"{engine}-db"
    elif resource_type == "DynamoDB Table":
        return f"user-data"
    elif resource_type == "EBS Volume":
        return f"data-volume"
    elif resource_type == "EBS Snapshot":
        return f"backup"
    elif resource_type == "Elastic IP":
        return f"static-ip"
    elif resource_type == "ELB":
        return f"load-balancer"
    elif resource_type == "IAM Role":
        return f"service-role"
    elif resource_type == "IAM User":
        return f"system-user"
    elif resource_type == "OpenSearch Domain":
        return f"search-cluster"
    return f"resource"

def generate_resource_reasons(resource_type, days_ago=180):
    """Generate resource-specific reasons for being flagged as unused."""
    reasons = []
    
    # Common metrics
    cpu_util = round(random.uniform(1, 5), 2)
    memory_util = round(random.uniform(1, 8), 2)
    network_util = round(random.uniform(0.1, 2), 2)
    
    if resource_type == "EC2 Instance":
        reasons.extend([
            f"Low CPU utilization (average {cpu_util}%) in the last {days_ago} days",
            f"Low memory utilization (average {memory_util}%)",
            f"Minimal network traffic ({network_util} KB/s average)"
        ])
    elif resource_type == "RDS Instance":
        connections = random.randint(0, 5)
        storage_used = round(random.uniform(5, 20), 1)
        reasons.extend([
            f"Low connection count (average {connections} active connections/day)",
            f"Low storage utilization ({storage_used}% used)",
            f"Minimal query activity in the last {days_ago} days"
        ])
    elif resource_type == "DynamoDB Table":
        read_capacity = round(random.uniform(0.1, 2), 2)
        write_capacity = round(random.uniform(0.1, 1), 2)
        reasons.extend([
            f"Low read capacity utilization ({read_capacity}% of provisioned)",
            f"Low write capacity utilization ({write_capacity}% of provisioned)",
            "No table updates in past month"
        ])
    elif resource_type == "EBS Volume":
        iops = random.randint(1, 10)
        reasons.extend([
            f"Low I/O activity (average {iops} IOPS)",
            "No snapshot created",
            "Not attached to any active instance"
        ])
    elif resource_type == "EBS Snapshot":
        age = random.randint(180, 365)
        reasons.extend([
            f"Snapshot is {age} days old",
            "Source volume no longer exists",
            "Multiple redundant snapshots exist"
        ])
    elif resource_type == "Elastic IP":
        reasons.extend([
            "Not associated with any running instance",
            f"No network traffic in {days_ago} days",
            "No DNS records pointing to this IP"
        ])
    elif resource_type == "ELB":
        requests = random.randint(1, 10)
        reasons.extend([
            f"Low request count (average {requests} requests/minute)",
            "No healthy backend instances",
            f"Minimal network traffic ({network_util} KB/s average)"
        ])
    elif resource_type == "IAM Role":
        reasons.extend([
            f"No API calls in the last {days_ago} days",
            "No attached policies",
            "No services using this role"
        ])
    elif resource_type == "IAM User":
        reasons.extend([
            f"No console or API activity in {days_ago} days",
            "No attached policies",
            "User has never logged in"
        ])
    elif resource_type == "OpenSearch Domain":
        search_requests = random.randint(1, 5)
        index_size = round(random.uniform(0.1, 2), 2)
        reasons.extend([
            f"Low search traffic (average {search_requests} requests/minute)",
            f"Small index size ({index_size} GB)",
            "No index updates in past month"
        ])
    
    # Randomly select 2-3 reasons
    return random.sample(reasons, random.randint(2, 3))

def generate_resource_details(resource_type):
    """Generate realistic details for a resource."""
    details = {}
    
    if resource_type == "EC2 Instance":
        instance_types = ["t3.micro", "t3.small", "t3.medium", "t3.large", "r5.xlarge"]
        details["InstanceType"] = random.choice(instance_types)
        details["State"] = random.choice(["running", "stopped"])
        details["LaunchTime"] = (datetime.datetime.now() - datetime.timedelta(days=random.randint(30, 365))).isoformat()
        details["Tags"] = [
            {"Key": "Name", "Value": f"{generate_resource_name(resource_type)}-01"},
            {"Key": "Environment", "Value": random.choice(["dev", "stg", "prod"]).lower()},
            {"Key": "Team", "Value": random.choice(["platform", "backend", "frontend"])}
        ]
        details["SecurityGroups"] = [
            {"GroupId": f"sg-{uuid.uuid4().hex[:8]}", "GroupName": "default"},
            {"GroupId": f"sg-{uuid.uuid4().hex[:8]}", "GroupName": "app-server"}
        ]
        details["NetworkInterfaces"] = [
            {
                "NetworkInterfaceId": f"eni-{uuid.uuid4().hex[:8]}",
                "PrivateIpAddress": f"10.0.{random.randint(1, 255)}.{random.randint(1, 255)}",
                "PublicIpAddress": f"54.{random.randint(1, 255)}.{random.randint(1, 255)}.{random.randint(1, 255)}"
            }
        ]
        
    elif resource_type == "RDS Instance":
        instance_classes = ["db.t3.micro", "db.t3.small", "db.t3.medium", "db.r5.large"]
        details["DBInstanceClass"] = random.choice(instance_classes)
        details["Engine"] = random.choice(["mysql", "postgres", "aurora"])
        details["EngineVersion"] = "8.0.28"
        details["DBInstanceIdentifier"] = f"{resource_type.lower().replace(' ', '-')}-{random.randint(10000000, 99999999):08x}"
        details["DBInstanceStatus"] = "available"
        details["MasterUsername"] = "admin"
        details["Endpoint"] = {
            "Address": f"db-{uuid.uuid4().hex[:8]}.cluster-xyz.region.rds.amazonaws.com",
            "Port": 5432 if "postgres" in details["Engine"] else 3306
        }
        details["AllocatedStorage"] = random.randint(50, 1000)
        details["PreferredBackupWindow"] = "03:00-04:00"
        details["BackupRetentionPeriod"] = random.choice([7, 14, 30, 35])
        details["VpcSecurityGroups"] = [
            {
                "VpcSecurityGroupId": f"sg-{uuid.uuid4().hex[:8]}",
                "Status": "active"
            }
        ]
        details["MultiAZ"] = random.choice([True, False])
        details["PubliclyAccessible"] = False
        details["StorageEncrypted"] = True
        details["PerformanceInsights"] = {
            "Enabled": True,
            "RetentionPeriod": 7
        }
        
    elif resource_type == "EBS Volume":
        details["Size"] = random.randint(8, 100)  # Most volumes are under 100GB
        details["VolumeType"] = random.choice(["gp2", "gp3", "io1"])
        details["State"] = "available"
        details["VolumeId"] = f"{resource_type.lower().replace(' ', '-')}-{random.randint(10000000, 99999999):08x}"
        details["Encrypted"] = True
        details["Iops"] = random.randint(100, 3000)
        details["MultiAttach"] = False
        
    elif resource_type == "EBS Snapshot":
        details["VolumeSize"] = random.randint(8, 100)  # Match volume sizes
        details["State"] = "completed"
        details["StartTime"] = (datetime.datetime.now() - datetime.timedelta(days=random.randint(30, 365))).isoformat()
        details["VolumeId"] = f"{resource_type.lower().replace(' ', '-')}-{random.randint(10000000, 99999999):08x}"
        
    elif resource_type == "DynamoDB Table":
        details["ProvisionedThroughput"] = {
            "ReadCapacityUnits": random.randint(1, 3),  
            "WriteCapacityUnits": random.randint(1, 2)  
        }
        details["TableStatus"] = "ACTIVE"
        details["TableName"] = f"{generate_resource_name(resource_type)}-01"
        details["CreationDateTime"] = (datetime.datetime.now() - datetime.timedelta(days=random.randint(30, 365))).isoformat()
        details["TableSizeBytes"] = random.randint(1000, 1000000)
        details["ItemCount"] = random.randint(0, 1000)
        details["StreamEnabled"] = random.choice([True, False])
        
    elif resource_type == "IAM User":
        details["CreateDate"] = (datetime.datetime.now() - datetime.timedelta(days=random.randint(30, 365))).isoformat()
        details["PasswordLastUsed"] = (datetime.datetime.now() - datetime.timedelta(days=random.randint(30, 365))).isoformat()
        details["Groups"] = random.randint(0, 3)
        
    elif resource_type == "IAM Role":
        details["CreateDate"] = (datetime.datetime.now() - datetime.timedelta(days=random.randint(30, 365))).isoformat()
        details["LastUsedDate"] = (datetime.datetime.now() - datetime.timedelta(days=random.randint(30, 365))).isoformat()
        details["AttachedPolicies"] = random.randint(1, 5)
        
    elif resource_type == "Elastic IP":
        details["PublicIp"] = f"54.{random.randint(0, 255)}.{random.randint(0, 255)}.{random.randint(0, 255)}"
        details["AllocationId"] = f"eipalloc-{random.randint(10000000, 99999999):08x}"
        details["Domain"] = "vpc"
        
    elif resource_type == "OpenSearch Domain":
        instance_types = ["t3.small.search", "t3.medium.search", "r5.large.search"]
        details["InstanceType"] = random.choice(instance_types)
        details["EngineVersion"] = "OpenSearch_1.0"
        details["ClusterConfig"] = {
            "InstanceCount": random.randint(1, 3),
            "DedicatedMasterEnabled": random.choice([True, False])
        }
        
    return details

def calculate_scan_metrics(total_resources):
    """Calculate realistic scan metrics based on completed scans in 1 minute."""
    # Fixed total run time of 1 minute (60 seconds)
    total_run_time_seconds = 60
    
    # Calculate number of workers
    max_workers = 10
    peak_workers = random.randint(7, max_workers)
    worker_utilization = (peak_workers / max_workers) * 100

    # Calculate tasks per second (between 5-8 tasks/sec)
    tasks_per_second = random.uniform(5.0, 8.0)
    total_scans = int(tasks_per_second * total_run_time_seconds)
    
    # Calculate completed and failed scans
    failed_scans = random.randint(1, 3)  # 1-3 failed scans
    completed_scans = total_scans - failed_scans
    
    # Calculate average execution time in milliseconds
    avg_execution_time_ms = (total_run_time_seconds * 1000) / total_scans

    return {
        "CompletedAt": (datetime.datetime.now() + datetime.timedelta(days=365)).isoformat(),
        "TotalResources": total_resources,
        "ResourcesByType": {},
        "TotalCost": random.uniform(5000, 15000),
        "TotalScans": total_scans,
        "CompletedScans": completed_scans,
        "FailedScans": failed_scans,
        "TasksPerSecond": tasks_per_second,
        "AvgExecutionTimeMs": round(avg_execution_time_ms, 2),
        "WorkerUtilization": worker_utilization,
        "PeakWorkers": peak_workers,
        "MaxWorkers": max_workers,
        "TotalRunTime": "1m0s"  # Fixed 1 minute run time
    }

def calculate_resource_costs(resource_type, details):
    """Calculate realistic costs for AWS resources."""
    costs = {
        "hourly": 0.0,
        "daily": 0.0,
        "monthly": 0.0,
        "yearly": 0.0,
        "lifetime": 0.0
    }
    
    if resource_type == "EC2 Instance":
        # EC2 pricing based on instance type
        instance_type = details.get("InstanceType", "t3.micro")
        hourly_rates = {
            "t3.micro": 0.0104,
            "t3.small": 0.0208,
            "t3.medium": 0.0416,
            "t3.large": 0.0832,
            "r5.xlarge": 0.252,
            "r5.2xlarge": 0.504,
            "r5.4xlarge": 1.008
        }
        costs["hourly"] = hourly_rates.get(instance_type, 0.0416) / 2  # Assume average usage
        costs["daily"] = costs["hourly"] * 24
        costs["monthly"] = costs["daily"] * 30.44
        
    elif resource_type == "RDS Instance":
        # RDS pricing (slightly higher than EC2)
        instance_class = details.get("DBInstanceClass", "db.t3.medium")
        hourly_rates = {
            "db.t3.micro": 0.017,
            "db.t3.small": 0.034,
            "db.t3.medium": 0.068,
            "db.r5.large": 0.29,
            "db.r5.xlarge": 0.58
        }
        costs["hourly"] = hourly_rates.get(instance_class, 0.068) / 3  # Assume smaller instances
        costs["daily"] = costs["hourly"] * 24
        costs["monthly"] = costs["daily"] * 30.44
        
    elif resource_type == "EBS Volume":
        # EBS pricing ($0.08 per GB-month for gp3)
        size_gb = float(details.get("Size", 20))  # Default to smaller size
        volume_type = details.get("VolumeType", "gp3")
        # gp3 is $0.08/GB-month, gp2 is $0.10/GB-month
        price_per_gb = 0.10 if volume_type == "gp2" else 0.08
        costs["monthly"] = size_gb * price_per_gb  # Monthly cost
        costs["daily"] = costs["monthly"] / 30.44  # Daily cost (avg days per month)
        costs["hourly"] = costs["daily"] / 24  # Hourly cost
        
    elif resource_type == "EBS Snapshot":
        # EBS Snapshot pricing ($0.05 per GB-month)
        size_gb = float(details.get("VolumeSize", 20))  # Default to smaller size
        costs["monthly"] = size_gb * 0.05  # Monthly cost
        costs["daily"] = costs["monthly"] / 30.44  # Daily cost
        costs["hourly"] = costs["daily"] / 24  # Hourly cost
        
    elif resource_type == "Elastic IP":
        # Elastic IP pricing (charged when not attached to running instance)
        costs["hourly"] = 0.005  # $0.005 per hour when not attached
        costs["daily"] = costs["hourly"] * 24
        costs["monthly"] = costs["daily"] * 30.44
        
    elif resource_type == "DynamoDB Table":
        # DynamoDB pricing (based on provisioned capacity)
        read_capacity = int(details.get("ProvisionedThroughput", {}).get("ReadCapacityUnits", 5))
        write_capacity = int(details.get("ProvisionedThroughput", {}).get("WriteCapacityUnits", 5))
        costs["hourly"] = (read_capacity * 0.00013) + (write_capacity * 0.00065)
        costs["daily"] = costs["hourly"] * 24
        costs["monthly"] = costs["daily"] * 30.44
        
    elif resource_type == "ELB":
        # ELB pricing
        costs["hourly"] = 0.0225  # $0.0225 per hour per ALB
        costs["daily"] = costs["hourly"] * 24
        costs["monthly"] = costs["daily"] * 30.44
        
    elif resource_type == "OpenSearch Domain":
        # OpenSearch pricing (based on instance type)
        instance_type = details.get("InstanceType", "t3.small.search")
        hourly_rates = {
            "t3.small.search": 0.036,
            "t3.medium.search": 0.073,
            "r5.large.search": 0.186
        }
        costs["hourly"] = hourly_rates.get(instance_type, 0.036) / 2  # Assume smaller instances
        costs["daily"] = costs["hourly"] * 24
        costs["monthly"] = costs["daily"] * 30.44

    # Calculate yearly and lifetime costs
    costs["yearly"] = costs["monthly"] * 12
    costs["lifetime"] = costs["yearly"] * random.randint(-1, 3)  # assume 3 year lifetime

    return costs

def generate_sample_data():
    """Generate sample AWS resource usage data with descriptive names."""
    # Initialize the data structure
    data = {
        "ScanMetrics": {
            "CompletedAt": (datetime.datetime.now() + datetime.timedelta(days=365)).isoformat(),
            "TotalResources": 0,  # Will be updated after generating all resources
            "ResourcesByType": {},
        },
        "AccountsAndRegions": {
            "123456789012": generate_random_regions(),
            "234567890123": generate_random_regions(),
            "345678901234": generate_random_regions()
        },
        "AccountNames": {
            "123456789012": "Production",
            "234567890123": "Staging",
            "345678901234": "Development"
        },
        "UnusedResources": []
    }

    # Define resource types
    resource_types = [
        "EC2 Instance",
        "RDS Instance",
        "ELB",
        "EBS Volume",
        "EBS Snapshot",
        "DynamoDB Table",
        "IAM User",
        "IAM Role",
        "Elastic IP",
        "OpenSearch Domain"
    ]
    
    # Generate resources for each type
    for resource_type in resource_types:
        # Generate between 5 and 15 resources of this type
        num_resources = random.randint(5, 15)
        
        for i in range(num_resources):
            account_id = random.choice(list(data["AccountsAndRegions"].keys()))
            region = random.choice(data["AccountsAndRegions"][account_id])
            details = generate_resource_details(resource_type)
            
            resource = {
                "id": f"{resource_type.lower().replace(' ', '-')}-{random.randint(10000000, 99999999):08x}",
                "name": f"{generate_resource_name(resource_type)}-{str(i+1).zfill(2)}",
                "type": resource_type,
                "account_id": account_id,
                "account_name": data["AccountNames"][account_id],
                "region": region,
                "last_used": (datetime.datetime.now() - datetime.timedelta(days=random.randint(30, 180))).isoformat(),
                "estimated_monthly_savings": round(random.uniform(100, 1000), 2),
                "reasons": generate_resource_reasons(resource_type),
                "details": details,
                "costs": calculate_resource_costs(resource_type, details)
            }
            
            data["UnusedResources"].append(resource)
            
            # Update resource count by type
            if resource_type not in data["ScanMetrics"]["ResourcesByType"]:
                data["ScanMetrics"]["ResourcesByType"][resource_type] = 0
            data["ScanMetrics"]["ResourcesByType"][resource_type] += 1
    
    # Calculate total resources
    data["ScanMetrics"]["TotalResources"] = len(data["UnusedResources"])
    
    # Calculate scan metrics
    scan_metrics = calculate_scan_metrics(data["ScanMetrics"]["TotalResources"])
    data["ScanMetrics"].update(scan_metrics)
    
    # Calculate cost breakdown by resource type
    cost_breakdown = {}
    total_costs = {
        "hourly": 0.0,
        "daily": 0.0,
        "monthly": 0.0,
        "yearly": 0.0,
        "lifetime": 0.0
    }
    
    # Group costs by resource type
    for resource in data["UnusedResources"]:
        resource_type = resource["type"]
        if resource_type not in cost_breakdown:
            cost_breakdown[resource_type] = {
                "type": resource_type,
                "hourly": 0.0,
                "daily": 0.0,
                "monthly": 0.0,
                "yearly": 0.0,
                "lifetime": 0.0
            }
        
        # Add resource costs to type totals
        for period in ["hourly", "daily", "monthly", "yearly", "lifetime"]:
            cost = resource["costs"][period]
            cost_breakdown[resource_type][period] += cost
            total_costs[period] += cost
    
    # Convert cost breakdown to list
    data["CostBreakdown"] = list(cost_breakdown.values())
    data["TotalCosts"] = total_costs
    
    return data

def generate_random_regions():
    regions = ["us-east-1", "us-east-2", "us-west-1", "us-west-2", "eu-central-1", "eu-west-1", "ap-southeast-1"]
    return random.sample(regions, random.randint(3, 7))

def main():
    """Generate sample data and write to JSON file."""
    data = generate_sample_data()
    output_file = "examples/sample_scan_data.json"
    
    with open(output_file, "w") as f:
        json.dump(data, f, indent=2)
    print(f"Sample data written to {output_file}")

if __name__ == "__main__":
    main()
