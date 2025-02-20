package output

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

const (
	barWidth    = 40
	refreshRate = 100 * time.Millisecond
)

// ProgressBar represents a progress bar for tracking file uploads
type ProgressBar struct {
	total     int64
	current   int64
	mu        sync.Mutex
	done      chan struct{}
	lastPrint time.Time
}

// NewProgressBar creates a new progress bar
func NewProgressBar(total int64) *ProgressBar {
	return &ProgressBar{
		total:     total,
		done:      make(chan struct{}),
		lastPrint: time.Now(),
	}
}

// Update updates the current progress
func (p *ProgressBar) Update(n int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = n

	// Only refresh if enough time has passed since last print
	if time.Since(p.lastPrint) >= refreshRate {
		p.print()
		p.lastPrint = time.Now()
	}
}

// print prints the current progress bar
func (p *ProgressBar) print() {
	percent := float64(p.current) / float64(p.total)
	filled := int(percent * float64(barWidth))
	
	// Create the bar
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	
	// Calculate transfer speed
	speed := float64(p.current) / time.Since(p.lastPrint).Seconds()
	speedStr := formatBytes(int64(speed)) + "/s"
	
	// Format progress
	progress := fmt.Sprintf("%s/%s", formatBytes(p.current), formatBytes(p.total))
	
	// Print the bar with colors
	fmt.Printf("\r%s [%s] %3.0f%% %s %s",
		color.BlueString("Uploading"),
		color.GreenString(bar),
		percent*100,
		color.YellowString(progress),
		color.CyanString(speedStr))
}

// Done marks the progress bar as complete
func (p *ProgressBar) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = p.total
	p.print()
	fmt.Println() // Move to next line
	close(p.done)
}

// formatBytes formats bytes into human readable format
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
