package cmd

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ripper",
	Short: "Media ripper and monitor wrapper",
	Run:   func(cmd *cobra.Command, args []string) { cmd.Help() },
}

func Execute() error {
	return rootCmd.Execute()
}
