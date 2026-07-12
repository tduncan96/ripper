package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"

	"ripper/internal/prflt"

	"github.com/spf13/cobra"
)

var ripEject bool
var ripTrackSelect bool

var ripCmd = &cobra.Command{
	Use:   "rip <flags> <drv> <movie|show> <season| >",
	Short: "Start media rip on specified drive.",
	Args:  cobra.RangeArgs(2, 3),
	RunE: func(cmd *cobra.Command, args []string) error {
		ripGate := prflt.MasterGate.RipConfig
		if ripGate != nil {
			return ripGate
		}

		script := filepath.Join(prflt.MasterConfig.SystemConfig.ScriptDir, "media-ripper.sh")

		var optArgs []string
		if ripEject {
			optArgs = append(optArgs, "-e")
		}
		if ripTrackSelect {
			optArgs = append(optArgs, "-t")
		}

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
		posArgs := []string{
			ripConfig.Permanent, // 1
			ripConfig.Staging,   // 2
			ripConfig.StatusTmp, // 3
			ripConfig.LogTmp,    // 4
			ripConfig.NtfyURL,   // 5
			drvNum,              // 6
			media,               // 7
			season,              // 8
		}

		ripArgs := append(optArgs, posArgs...)

		c := exec.Command(script, ripArgs...) // #nosec G204 -- script dir is a trusted constant; numeric/enum args validated; exec uses no shell

		if err := c.Start(); err != nil {
			return fmt.Errorf("rip failed during initialization: %w", err)
		}

		pid := c.Process.Pid
		fmt.Fprintf(cmd.OutOrStdout(), "Process spawned. PID: %d\n", pid)
		return nil
	},
}

var libCmd = &cobra.Command{
	Use:   "lib",
	Short: "Pulls full catalog of movies, shows, and music from Jellyfin and dumps it into Bookstack.",
	RunE: func(cmd *cobra.Command, args []string) error {
		librGate := prflt.MasterGate.LibrConfig
		if librGate != nil {
			return librGate
		}

		script := filepath.Join(prflt.MasterConfig.SystemConfig.ScriptDir, "media-librarian.sh")

		librConfig := prflt.MasterConfig.LibrarianConfig
		librArgs := []string{
			librConfig.JellyURL,                    // 1
			librConfig.JellyKey,                    // 2
			librConfig.BookURL,                     // 3
			librConfig.BookPageID,                  // 4
			librConfig.BookTokenID,                 // 5
			librConfig.BookKey,                     // 6
			prflt.MasterConfig.RipConfig.Permanent, // 7
		}

		c := exec.Command(script, librArgs...) // #nosec G204 -- script dir is a trusted constant; numeric/enum args validated; exec uses no shell

		if err := c.Run(); err != nil {
			return fmt.Errorf("media cataloging failed during initialization: %w", err)
		}

		pid := c.Process.Pid
		fmt.Fprintf(cmd.OutOrStdout(), "Process spawned. PID: %d\n", pid)
		return nil
	},
}

var unlockCmd = &cobra.Command{
	Use:   "unlock <drive number>",
	Short: "Removes the lock file for the given drive.",
	RunE: func(cmd *cobra.Command, args []string) error {
		_, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("argument needs to be an integer; got %v", args[0])
		}
		drv := "sr" + args[0]
		c := exec.Command("rm -rf /var/lock/media-ripper." + drv + ".lock") // #nosec G204 -- Input validated prior to injection
		if err := c.Run(); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	ripCmd.Flags().BoolVarP(&ripEject, "eject", "e", false, "eject disc after rip")
	ripCmd.Flags().BoolVarP(&ripTrackSelect, "track-select", "t", false, "enable select of specific tracks")

	rootCmd.AddCommand(ripCmd, libCmd, unlockCmd)
}
