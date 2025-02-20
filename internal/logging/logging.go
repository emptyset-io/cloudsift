package logging

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/fatih/color"
)

// Level represents a logging level
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	PROGRESS // Special level that always displays
)

func (l Level) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case PROGRESS:
		return "PROGRESS"
	default:
		return "UNKNOWN"
	}
}

// Format represents the log output format
type Format int

const (
	Text Format = iota
	JSON
)

// Logger handles structured logging
type Logger struct {
	out         io.Writer
	level       Level
	format      Format
	lastLogTime time.Time
	logMutex    sync.RWMutex
}

// LogConfig contains logger configuration
type LogConfig struct {
	Level  Level
	Format Format
}

// Account represents an AWS account
type Account struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var (
	defaultLogger = &Logger{
		out:         os.Stdout,
		level:       INFO,
		format:      Text,
		lastLogTime: time.Now(),
		logMutex:    sync.RWMutex{},
	}

	// Color definitions
	debugColor    = color.New(color.FgCyan)
	infoColor     = color.New(color.FgGreen)
	warnColor     = color.New(color.FgYellow)
	errorColor    = color.New(color.FgRed)
	progressColor = color.New(color.FgBlue, color.Bold)
)

// Configure sets up the default logger
func Configure(config LogConfig) {
	defaultLogger.level = config.Level
	defaultLogger.format = config.Format
}

type logEntry struct {
	Timestamp string      `json:"timestamp"`
	Level     string      `json:"level"`
	Message   string      `json:"message"`
	Data      interface{} `json:"data,omitempty"`
}

func (l *Logger) log(level Level, msg string, data interface{}) {
	// Always show PROGRESS level, otherwise respect level setting
	if level != PROGRESS && level < l.level {
		return
	}

	// Update last log time for non-PROGRESS logs
	if level != PROGRESS {
		l.logMutex.Lock()
		l.lastLogTime = time.Now()
		l.logMutex.Unlock()
	}

	timestamp := time.Now().Format("2006/01/02 15:04:05")

	if l.format == JSON {
		entry := logEntry{
			Timestamp: timestamp,
			Level:     level.String(),
			Message:   msg,
			Data:      data,
		}
		if err := json.NewEncoder(l.out).Encode(entry); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to encode log entry: %v\n", err)
		}
		return
	}

	// Text format
	var levelColor *color.Color
	switch level {
	case DEBUG:
		levelColor = debugColor
	case INFO:
		levelColor = infoColor
	case WARN:
		levelColor = warnColor
	case ERROR:
		levelColor = errorColor
	case PROGRESS:
		levelColor = progressColor
	default:
		levelColor = infoColor
	}

	levelStr := levelColor.Sprintf("%-5s", level.String())
	fmt.Fprintf(l.out, "%s %s: %s", timestamp, levelStr, msg)
	if data != nil {
		fmt.Fprintf(l.out, " %+v", data)
	}
	fmt.Fprintln(l.out)
}

func (l *Logger) Debug(msg string, data ...interface{}) {
	l.log(DEBUG, msg, firstOrNil(data))
}

func (l *Logger) Info(msg string, data ...interface{}) {
	l.log(INFO, msg, firstOrNil(data))
}

func (l *Logger) Warn(msg string, data ...interface{}) {
	l.log(WARN, msg, firstOrNil(data))
}

func (l *Logger) Error(msg string, err error, data ...interface{}) {
	if err != nil {
		msg = fmt.Sprintf("%s: %v", msg, err)
	}
	l.log(ERROR, msg, firstOrNil(data))
}

func (l *Logger) Progress(msg string, data interface{}) {
	l.log(PROGRESS, msg, data)
}

// firstOrNil returns the first element of data if present, nil otherwise
func firstOrNil(data []interface{}) interface{} {
	if len(data) > 0 {
		return data[0]
	}
	return nil
}

// ScanStart logs the start of a scan operation
func (l *Logger) ScanStart(scanners []string, accounts []Account, regions []string) {
	data := map[string]interface{}{
		"scanners": scanners,
		"accounts": accounts,
		"regions":  regions,
	}
	l.Info("Starting scan operation", data)
}

// ScannerStart logs the start of a specific scanner
func (l *Logger) ScannerStart(scanner, accountID, accountName, region string) {
	data := map[string]interface{}{
		"scanner":      scanner,
		"account_id":   accountID,
		"account_name": accountName,
		"region":       region,
	}
	l.Info("Starting scanner", data)
}

// ScannerComplete logs the completion of a specific scanner
func (l *Logger) ScannerComplete(scanner, accountID, accountName, region string, results []interface{}) {
	data := map[string]interface{}{
		"scanner":      scanner,
		"account_id":   accountID,
		"account_name": accountName,
		"region":       region,
		"result_count": len(results),
	}
	l.Info("Scanner completed", data)

	// Log detailed results at DEBUG level
	if l.level <= DEBUG && len(results) > 0 {
		for _, result := range results {
			l.Debug("Found resource", map[string]interface{}{
				"scanner":      scanner,
				"account_id":   accountID,
				"account_name": accountName,
				"region":       region,
				"resource":     result,
			})
		}
	}
}

// ScannerError logs a scanner error
func (l *Logger) ScannerError(scanner, accountID, accountName, region string, err error) {
	data := map[string]interface{}{
		"scanner":      scanner,
		"account_id":   accountID,
		"account_name": accountName,
		"region":       region,
	}
	l.Error("Scanner failed", err, data)
}

// ScanComplete logs the completion of a scan operation
func (l *Logger) ScanComplete(totalResults int) {
	data := map[string]interface{}{
		"total_results": totalResults,
	}
	l.Info("Scan operation complete", data)
}

// GetLastLogTime returns the time of the last non-PROGRESS log
func (l *Logger) GetLastLogTime() time.Time {
	l.logMutex.RLock()
	defer l.logMutex.RUnlock()
	return l.lastLogTime
}

// GetLastLogTime returns the time of the last non-PROGRESS log using the default logger
func GetLastLogTime() time.Time {
	return defaultLogger.GetLastLogTime()
}

// Default logger methods
func Debug(msg string, data ...interface{}) {
	defaultLogger.Debug(msg, data...)
}

func Info(msg string, data ...interface{}) {
	defaultLogger.Info(msg, data...)
}

func Warn(msg string, data ...interface{}) {
	defaultLogger.Warn(msg, data...)
}

func Error(msg string, err error, data ...interface{}) {
	defaultLogger.Error(msg, err, data...)
}

func Progress(msg string, data ...interface{}) {
	defaultLogger.Progress(msg, firstOrNil(data))
}

func ScanStart(scanners []string, accounts []Account, regions []string) {
	defaultLogger.ScanStart(scanners, accounts, regions)
}

func ScannerStart(scanner, accountID, accountName, region string) {
	defaultLogger.ScannerStart(scanner, accountID, accountName, region)
}

func ScannerComplete(scanner, accountID, accountName, region string, results []interface{}) {
	defaultLogger.ScannerComplete(scanner, accountID, accountName, region, results)
}

func ScannerError(scanner, accountID, accountName, region string, err error) {
	defaultLogger.ScannerError(scanner, accountID, accountName, region, err)
}

func ScanComplete(totalResults int) {
	defaultLogger.ScanComplete(totalResults)
}
