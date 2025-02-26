package init

import (
	"github.com/spf13/cobra"
)

// NewInitCmd creates the init command
func NewInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize CloudSift configuration files",
		Long: `Initialize CloudSift configuration files.

This command helps you create default configuration files for CloudSift.
You can create either a config.yaml file or a .env file with default settings.`,
	}

	cmd.AddCommand(NewConfigCmd())
	cmd.AddCommand(NewEnvCmd())

	return cmd
}
