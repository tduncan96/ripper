package cmd

import (
	"fmt"
	"text/tabwriter"

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

		err := web.OpenStatusFile()
		if err != nil {
			return err
		}

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

var listRecordsCmd = &cobra.Command{
	Use:   "records",
	Short: "List all rip records from the database.",
	RunE: func(cmd *cobra.Command, args []string) error {
		records, err := db.GetAllRecords()
		if err != nil {
			return err
		}

		w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ID\tSTART\tTITLE\tDEVICE\tEXIT\tRIP MB")
		for _, r := range records {
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%d\t%d\n",
				r.RunID, r.StartTime, r.Title, r.Device, r.ExitCode, r.TotalRipMB)
		}
		return w.Flush()
	},
}

func init() {
	rootCmd.AddCommand(createDbRecordCmd)
	rootCmd.AddCommand(listRecordsCmd)
}
