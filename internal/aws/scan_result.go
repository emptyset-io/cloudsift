package aws

// ScanResult represents a single resource found during a scan
type ScanResult struct {
	ResourceType string                 `json:"resource_type"`
	ResourceName string                 `json:"resource_name"`
	ResourceID   string                 `json:"resource_id"`
	Reason       string                 `json:"reason"`
	Tags         map[string]string      `json:"tags"`
	Details      map[string]interface{} `json:"details"`
	Cost         map[string]interface{} `json:"cost"`
}

// ScanResults is a slice of ScanResult
type ScanResults []ScanResult
