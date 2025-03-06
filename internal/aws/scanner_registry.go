package aws

import (
	"fmt"
	"sort"
	"sync"

	"github.com/aws/aws-sdk-go/aws/session"
)

// ScanOptions contains configuration for the scan operation
type ScanOptions struct {
	Region     string           // Region to scan
	DaysUnused int              // Number of days a resource must be unused to be reported
	Session    *session.Session // AWS session to use for scanning (already configured with necessary role chain)
	AccountID  string           // AWS Account ID for the session
}

// Scanner interface defines methods that must be implemented by resource scanners
type Scanner interface {
	ArgumentName() string // ArgumentName returns the name used in CLI arguments
	Label() string        // Label returns a human-readable label for the scanner
	Scan(opts ScanOptions) (ScanResults, error)
}

// ScannerRegistry manages available scanners
type ScannerRegistry struct {
	scanners map[string]Scanner
	mu       sync.RWMutex
}

// NewScannerRegistry creates a new scanner registry
func NewScannerRegistry() *ScannerRegistry {
	return &ScannerRegistry{
		scanners: make(map[string]Scanner),
	}
}

// RegisterScanner registers a scanner with the registry
func (r *ScannerRegistry) RegisterScanner(scanner Scanner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.scanners[scanner.ArgumentName()] = scanner
}

// GetScanner retrieves a scanner by argument name
func (r *ScannerRegistry) GetScanner(argumentName string) (Scanner, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	scanner, ok := r.scanners[argumentName]
	if !ok {
		return nil, fmt.Errorf("scanner %s not found", argumentName)
	}
	return scanner, nil
}

// ListScanners returns a sorted list of registered scanner argument names
func (r *ScannerRegistry) ListScanners() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var argumentNames []string
	for argumentName := range r.scanners {
		argumentNames = append(argumentNames, argumentName)
	}
	sort.Strings(argumentNames)
	return argumentNames
}

// DefaultRegistry is the default scanner registry
var DefaultRegistry = NewScannerRegistry()
