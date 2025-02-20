package list

import (
	"fmt"

	"cloudsift/internal/aws"
	"github.com/spf13/cobra"
)

// NewScannersCmd creates and returns the scanners command
func NewScannersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scanners",
		Short: "List available resource scanners",
		Long: `List all available resource scanners that can be used to scan AWS resources.
Each scanner is specialized for a specific type of resource.`,
		Example: `  # List all available resource scanners
  cloudsift list scanners`,
		RunE: func(cmd *cobra.Command, args []string) error {
			scannerList := aws.DefaultRegistry.ListScanners()
			if len(scannerList) == 0 {
				fmt.Println("No scanners registered")
				return nil
			}

			fmt.Println("Available scanners:")
			for _, name := range scannerList {
				scanner, err := aws.DefaultRegistry.GetScanner(name)
				if err != nil {
					continue
				}
				fmt.Printf("  - %s\n", scanner.Name())
			}
			return nil
		},
	}

	return cmd
}
