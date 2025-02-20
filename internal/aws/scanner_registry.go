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
	// Name returns the human-readable name of the scanner
	Name() string

	// ArgumentName returns the command-line argument name for the scanner
	ArgumentName() string

	// Label returns a unique label identifying the scanner
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
	argName := s.ArgumentName()
	if _, exists := r.scanners[argName]; exists {
		return fmt.Errorf("scanner with argument name '%s' already registered", argName)
	}
	r.scanners[argName] = s
	return nil
}

// GetScanner retrieves a scanner by its identifier (argument name, label, or name)
func (r *Registry) GetScanner(identifier string) (Scanner, error) {
	// First try by argument name (exact match)
	if scanner, ok := r.scanners[identifier]; ok {
		return scanner, nil
	}

	// Try case-insensitive match on argument name
	identifier = strings.ToLower(identifier)
	for _, scanner := range r.scanners {
		if strings.ToLower(scanner.ArgumentName()) == identifier ||
			strings.ToLower(scanner.Label()) == identifier ||
			strings.ToLower(scanner.Name()) == identifier {
			return scanner, nil
		}
	}

	return nil, fmt.Errorf("no scanner found for identifier '%s'", identifier)
}

// ListScanners returns a sorted list of all registered scanner argument names
func (r *Registry) ListScanners() []string {
	var names []string
	for name := range r.scanners {
		names = append(names, name)
	}
	return names
}

// DefaultRegistry is the default scanner registry instance
var DefaultRegistry = NewRegistry()
