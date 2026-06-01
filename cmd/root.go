package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "ripper",
	Short: "Media ripper and monitor wrapper",
	Run: func(cmd *cobra.Command, args []string) {
		err := cmd.Help()
		if err != nil {
			fmt.Printf("error in command root: %s", err)
			os.Exit(1)
		}
	},
}

func Execute() error {
	return rootCmd.Execute()
}
