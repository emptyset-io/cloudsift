# CloudSift CLI

CloudSift is a powerful command-line interface tool built in Go for cloud infrastructure scanning and analysis. It provides a robust set of commands for scanning and analyzing cloud resources across different providers.

## Features

- **Multi-Cloud Support**: Built with multi-cloud architecture in mind
- **AWS Integration**: Deep integration with AWS services using the AWS SDK
- **Progress Tracking**: Real-time progress visualization for long-running operations
- **Colorized Output**: Enhanced readability with color-coded output
- **Configurable Profiles**: Support for multiple AWS profiles

## Prerequisites

- Go 1.24.0 or higher
- AWS credentials configured (for AWS-related operations)

## Installation

### From Source

1. Clone the repository:
```bash
git clone https://github.com/emptyset-io/cloudsift.git
cd cloudsift
```

2. Install Go (if not already installed):
```bash
make install-go
```

3. Install dependencies:
```bash
make deps
```

4. Build the binary:
```bash
make build
```

The binary will be available in the `bin` directory.

### Using Go Install

```bash
go install github.com/emptyset-io/cloudsift@latest
```

## Usage

### Basic Commands

```bash
# Show help and available commands
cloudsift --help

# List available scanners
cloudsift list scanners

# Run a specific scanner
cloudsift scan --provider aws --profile default
```

### Configuration

CloudSift uses the following configuration hierarchy:
1. Command-line flags
2. Environment variables
3. Configuration files
4. Default values

## Development

### Project Structure

```
cloudsift/
├── cmd/            # CLI commands and subcommands
├── internal/       # Internal packages
├── cache/         # Caching implementation
├── main.go        # Entry point
└── Makefile       # Build and development commands
```

### Available Make Commands

- `make build`: Build the binary
- `make test`: Run tests
- `make fmt`: Format code
- `make lint`: Run linters
- `make clean`: Clean build artifacts
- `make install`: Install the binary locally

### Running Tests

```bash
make test
```

### Contributing

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Acknowledgments

- Built with [Cobra](https://github.com/spf13/cobra)
- AWS SDK for Go
- Progress visualization by [progressbar](https://github.com/schollz/progressbar)
- Color support by [fatih/color](https://github.com/fatih/color)

## Support

For bugs, questions, and discussions, please use the [GitHub Issues](https://github.com/yourusername/cloudsift/issues).
