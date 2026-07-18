package system

import (
	"fmt"
	"os/exec"
	"regexp"
)

var devRe  = regexp.MustCompile(`(?m)^ID_CDROM_MEDIA=1$`)

func DriveState(dev string) error {
	out, err := exec.Command("udevadm", "info", "--query=property", "--name="+dev).CombinedOutput() // #nosec G204 -- dev is a validated drive tag, not user input
	if err != nil {
		return err
	}

	if !devRe.Match(out) {
		return fmt.Errorf("cdrom not found in udevadm query")
	}

	return nil
}


func Eject(dev string) error {
	_, err := exec.Command("eject", dev).CombinedOutput()
	return err
}