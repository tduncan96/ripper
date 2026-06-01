package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"

	"ripper/internal/prflt"

	"github.com/spf13/cobra"
)

var ripCommand = &cobra.Command{
	Use:   "rip <drv> <movie|show> <season| >",
	Short: "Start media rip on specified drive.",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		ripGate := prflt.MasterGate.RipConfig
		if ripGate != nil {
			return ripGate
		}

		script := filepath.Join(prflt.MasterConfig.SystemConfig.ScriptDir, "media-ripper.sh")
		drvNum := args[0]
		if _, err := strconv.Atoi(drvNum); err != nil {
			return fmt.Errorf("drive must be a number, got %q", drvNum)
		}
		media := args[1]
		if media != "movie" && media != "show" {
			return fmt.Errorf("media type must be 'movie' or 'show', got %q", media)
		}
		season := ""
		if len(args) == 3 {
			season = args[2]
			if _, err := strconv.Atoi(season); err != nil {
				return fmt.Errorf("season must be a number, got %q", season)
			}
		}

		ripConfig := prflt.MasterConfig.RipConfig

		staging := ripConfig.Staging
		permanent := ripConfig.Permanent
		statusTmpPath := ripConfig.StatusTmp
		logTmpPath := ripConfig.LogTmp
		ntfyURL := ripConfig.NtfyURL

		c := exec.Command( //nosec G204 -- script dir is a trusted constant; numeric/enum args validated; exec uses no shell
			script,        // 0
			permanent,     // 1
			staging,       // 2
			statusTmpPath, // 3
			logTmpPath,    // 4
			ntfyURL,       // 5
			drvNum,        // 6
			media,         // 7
			season,        // 8
		)

		if err := c.Start(); err != nil {
			return fmt.Errorf("rip failed during initialization: %w", err)
		}
		return nil
	},
}

var librarianCommand = &cobra.Command{
	Use:   "catalog",
	Short: "Pulls full catalog of movies, shows, and music from Jellyfin and dumps it into Bookstack.",
	RunE: func(cmd *cobra.Command, args []string) error {
		librGate := prflt.MasterGate.LibrConfig
		if librGate != nil {
			return librGate
		}

		script := filepath.Join(prflt.MasterConfig.SystemConfig.ScriptDir, "media-librarian.sh")

		librConfig := prflt.MasterConfig.LibrarianConfig

		c := exec.Command( //nosec G204 -- script dir is a trusted constant; numeric/enum args validated; exec uses no shell
			script,                                 // 0
			librConfig.JellyURL,                    // 1
			librConfig.JellyKey,                    // 2
			librConfig.BookURL,                     // 3
			librConfig.BookPageID,                  // 4
			librConfig.BookTokenID,                 // 5
			librConfig.BookKey,                     // 6
			prflt.MasterConfig.RipConfig.Permanent, // 7
		)

		if err := c.Run(); err != nil {
			return fmt.Errorf("media cataloging failed during initialization: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(ripCommand)
	rootCmd.AddCommand(librarianCommand)
}
