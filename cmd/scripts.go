package cmd

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"

	"ripper/internal/preflight"

	"github.com/spf13/cobra"
)

var ripCommand = &cobra.Command{
	Use:   "rip <drv> <movie|show> <season| >",
	Short: "Start media rip on specified drive.",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		ripGate := preflight.MasterGate.RipConfig
		if ripGate != nil {
			return ripGate
		}

		script := filepath.Join(preflight.MasterConfig.SystemConfig.ScriptDir, "media-ripper.sh")
		drv := args[0]
		media := args[1]
		season := ""
		if len(args) == 3 {
			season = args[2]
		}

		c := exec.Command(script, drv, media, season)

		var stderr bytes.Buffer
		c.Stderr = &stderr
		if err := c.Run(); err != nil {
			return fmt.Errorf("rip failed during initialization: %w: %s", err, stderr.String())
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(ripCommand)
}

// need to rewrite this and the bash script to also take directories as arguments so imports from config are honored
