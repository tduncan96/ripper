package cmd

import (
	"ripper/internal/web"

	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start ripper server",
	Run: func(cmd *cobra.Command, args []string) {
		web.Serve()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}
