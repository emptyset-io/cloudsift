package output

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	awsutil "cloudsift/internal/aws"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/schollz/progressbar/v3"
)

const (
	defaultMaxRetries        = 3
	defaultRetryDelay        = 2 * time.Second
	defaultPartSize          = 5 * 1024 * 1024 // 5MB
	defaultConcurrentUploads = 5
)

// RetryConfig holds retry configuration
type RetryConfig struct {
	MaxRetries int
	RetryDelay time.Duration
}

// UploadConfig holds upload configuration
type UploadConfig struct {
	PartSize        int64
	ConcurrentParts int
}

// Type represents the output type
type Type string

const (
	// FileSystem represents local filesystem output
	FileSystem Type = "filesystem"
	// S3 represents S3 bucket output
	S3 Type = "s3"
)

// Config holds output configuration
type Config struct {
	Type             Type
	S3Bucket         string
	S3Region         string
	OutputDir        string
	Retry            *RetryConfig
	Upload           *UploadConfig
	Region           string
	OrganizationRole string // Role to assume for S3 operations
}

// Writer handles writing scan results to different destinations
type Writer struct {
	config Config
}

// NewWriter creates a new output writer with default settings
func NewWriter(config Config) *Writer {
	// Set default retry config if not provided
	if config.Retry == nil {
		config.Retry = &RetryConfig{
			MaxRetries: defaultMaxRetries,
			RetryDelay: defaultRetryDelay,
		}
	}

	// Set default upload config if not provided
	if config.Upload == nil {
		config.Upload = &UploadConfig{
			PartSize:        defaultPartSize,
			ConcurrentParts: defaultConcurrentUploads,
		}
	}

	if config.Type == FileSystem && config.OutputDir == "" {
		config.OutputDir = "output"
	}
	return &Writer{config: config}
}

// getAccountID extracts just the numeric account ID from a potentially compound ID
func (w *Writer) getAccountID(accountID string) string {
	// Split by "-" and take the first part, trimming any whitespace
	parts := strings.Split(accountID, "-")
	return strings.TrimSpace(parts[0])
}

// getFilePath returns the file path in the format:
// filesystem: output/YYYY/MM/DD/<accountId>/HH-MM-SS-0700.json.gz
// s3: YYYY/MM/DD/<accountId>/HH-MM-SS-0700.json.gz
func (w *Writer) getFilePath(accountID string, t time.Time) string {
	// Extract just the numeric account ID
	accountID = w.getAccountID(accountID)

	// Format the filename with account ID and timestamp
	fileName := t.Format("15-04-05-0700") + ".json.gz"

	// Format the date path as YYYY/MM/DD
	datePath := t.Format("2006/01/02")

	// Construct the path
	if w.config.Type == FileSystem {
		// In filesystem, create the directory structure with account ID as a folder
		return filepath.Join(w.config.OutputDir, datePath, accountID, fileName)
	}
	// For S3, use the same structure without the base directory
	return filepath.Join(datePath, accountID, fileName)
}

// compressData compresses the input data using gzip
func (w *Writer) compressData(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)

	if _, err := gz.Write(data); err != nil {
		return nil, fmt.Errorf("failed to write to gzip writer: %w", err)
	}

	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	return buf.Bytes(), nil
}

// Write writes the scan results to the configured destination
func (w *Writer) Write(accountID string, results interface{}) error {
	// Convert results to JSON
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal results: %w", err)
	}

	// Compress the data
	compressedData, err := w.compressData(data)
	if err != nil {
		return fmt.Errorf("failed to compress data: %w", err)
	}

	now := time.Now()
	path := w.getFilePath(accountID, now)

	switch w.config.Type {
	case FileSystem:
		return w.writeToFileSystem(path, compressedData)
	case S3:
		return w.writeToS3WithRetry(path, compressedData)
	default:
		return fmt.Errorf("unsupported output type: %s", w.config.Type)
	}
}

// writeToFileSystem writes compressed data to the local filesystem
func (w *Writer) writeToFileSystem(path string, data []byte) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write file %s: %w", path, err)
	}

	return nil
}

// writeToS3WithRetry writes data to an S3 bucket with retry logic
func (w *Writer) writeToS3WithRetry(path string, data []byte) error {
	if w.config.S3Bucket == "" {
		return fmt.Errorf("S3 bucket not specified")
	}

	var lastErr error
	for attempt := 0; attempt < w.config.Retry.MaxRetries; attempt++ {
		if attempt > 0 {
			fmt.Printf("Retrying S3 upload (attempt %d/%d) after error: %v\n",
				attempt+1, w.config.Retry.MaxRetries, lastErr)
			time.Sleep(w.config.Retry.RetryDelay)
		}

		if err := w.writeToS3(path, data); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return fmt.Errorf("failed to upload to S3 after %d attempts: %w",
		w.config.Retry.MaxRetries, lastErr)
}

// getRoleARN returns the full ARN for a role. If the input is already an ARN, returns it as is.
func getRoleARN(sess *session.Session, roleName string) (string, error) {
	// If it's already an ARN, return it
	if strings.HasPrefix(roleName, "arn:aws:iam::") {
		return roleName, nil
	}

	// Get the account ID using STS
	stsClient := sts.New(sess)
	result, err := stsClient.GetCallerIdentity(&sts.GetCallerIdentityInput{})
	if err != nil {
		return "", fmt.Errorf("failed to get account ID: %w", err)
	}

	// Construct the role ARN
	return fmt.Sprintf("arn:aws:iam::%s:role/%s", *result.Account, roleName), nil
}

// writeToS3 writes data to an S3 bucket with progress tracking
func (w *Writer) writeToS3(path string, data []byte) error {
	// Create base session
	sess, err := awsutil.GetSession("", w.config.S3Region)
	if err != nil {
		return fmt.Errorf("failed to create AWS session: %w", err)
	}

	// If organization role is specified, assume it
	if w.config.OrganizationRole != "" {
		// Get full role ARN
		roleARN, err := getRoleARN(sess, w.config.OrganizationRole)
		if err != nil {
			return fmt.Errorf("failed to get role ARN: %w", err)
		}

		// Create STS client
		stsClient := sts.New(sess)

		// Create assume role input
		roleSessionName := fmt.Sprintf("cloudsift-upload-%d", time.Now().Unix())
		input := &sts.AssumeRoleInput{
			RoleArn:         aws.String(roleARN),
			RoleSessionName: aws.String(roleSessionName),
		}

		// Assume the role
		result, err := stsClient.AssumeRole(input)
		if err != nil {
			return fmt.Errorf("failed to assume role: %w", err)
		}

		// Create new session with temporary credentials
		sess, err = session.NewSession(&aws.Config{
			Region: aws.String(w.config.S3Region),
			Credentials: credentials.NewStaticCredentials(
				*result.Credentials.AccessKeyId,
				*result.Credentials.SecretAccessKey,
				*result.Credentials.SessionToken,
			),
		})
		if err != nil {
			return fmt.Errorf("failed to create session with assumed role: %w", err)
		}
	}

	// Create uploader
	uploader := s3manager.NewUploader(sess, func(u *s3manager.Uploader) {
		u.PartSize = w.config.Upload.PartSize
		u.Concurrency = w.config.Upload.ConcurrentParts
	})

	// Create progress reader
	reader := &progressReader{
		reader: bytes.NewReader(data),
		size:   int64(len(data)),
		bar: progressbar.NewOptions64(
			int64(len(data)),
			progressbar.OptionSetDescription("Uploading to S3..."),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetWidth(15),
			progressbar.OptionThrottle(65*time.Millisecond),
			progressbar.OptionShowCount(),
			progressbar.OptionOnCompletion(func() {
				fmt.Println()
			}),
		),
	}

	// Upload the file with server-side encryption
	_, err = uploader.Upload(&s3manager.UploadInput{
		Bucket:               aws.String(w.config.S3Bucket),
		Key:                  aws.String(path),
		Body:                 reader,
		ServerSideEncryption: aws.String("aws:kms"),
	})

	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// progressReader wraps an io.Reader to track progress
type progressReader struct {
	reader io.Reader
	size   int64
	read   int64
	bar    *progressbar.ProgressBar
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	if err := r.bar.Add(n); err != nil {
		fmt.Fprintf(os.Stderr, "Error updating progress bar: %v\n", err)
	}
	return n, err
}
