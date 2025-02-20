package aws

import (
	"fmt"
	"strings"
)

// ScanOptions contains configuration for the scan operation
type ScanOptions struct {
	Region     string // AWS region to scan
	DaysUnused int    // Number of days a resource must be unused to be reported
}

// Scanner represents a resource scanner that can scan AWS resources
type Scanner interface {
	// ArgumentName returns the command-line argument name for the scanner (e.g., "ebs-volumes")
	ArgumentName() string

	// Label returns a human-readable label for the scanner (e.g., "EBS Volumes")
	Label() string

	// Scan performs the actual scanning operation
	// If region is empty, uses the default region from the session
	Scan(opts ScanOptions) (ScanResults, error)
}

// Registry maintains a central registry of all available scanners
type Registry struct {
	scanners map[string]Scanner
}

// NewRegistry creates a new scanner registry
func NewRegistry() *Registry {
	return &Registry{
		scanners: make(map[string]Scanner),
	}
}

// RegisterScanner adds a new scanner to the registry
func (r *Registry) RegisterScanner(s Scanner) error {
	name := s.ArgumentName()
	if _, exists := r.scanners[name]; exists {
		return fmt.Errorf("scanner with name '%s' already registered", name)
	}
	r.scanners[name] = s
	return nil
}

// GetScanner retrieves a scanner by its name
func (r *Registry) GetScanner(name string) (Scanner, error) {
	// First try exact match
	if scanner, ok := r.scanners[name]; ok {
		return scanner, nil
	}

	// Try case-insensitive match
	nameLower := strings.ToLower(name)
	for key, scanner := range r.scanners {
		if strings.ToLower(key) == nameLower {
			return scanner, nil
		}
	}

	return nil, fmt.Errorf("scanner '%s' not found", name)
}

// ListScanners returns a sorted list of all registered scanner names
func (r *Registry) ListScanners() []string {
	var names []string
	for name := range r.scanners {
		names = append(names, name)
	}
	return names
}

// DefaultRegistry is the default scanner registry instance
var DefaultRegistry = NewRegistry()
