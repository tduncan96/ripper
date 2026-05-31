package cmd

import (
	"fmt"

	"ripper/internal/db"
	"ripper/internal/web"

	"github.com/spf13/cobra"
)

var createDbRecordCmd = &cobra.Command{
	Use:   "record <drive tag>",
	Short: "Record the current run stats into the DB for a given drive.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		drv := args[0]
		status, err := web.GetStatus(drv)
		if err != nil {
			return err
		}

		record := db.Record{
			StartTime:   status.Start,
			EndTime:     status.Updated,
			ExitCode:    status.ExitCode,
			Device:      status.Device,
			Title:       status.Title,
			RawTitle:    status.RawTitle,
			Destination: status.FullDest,
			TotalRipMB:  status.TotalRipMB,
			TotalMvMB:   status.TotalMvMB,
			RipLog:      status.RipLog,
		}

		id, err := record.CreateRecord()
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Created run record. ID: %d\n", id)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(createDbRecordCmd)
}
