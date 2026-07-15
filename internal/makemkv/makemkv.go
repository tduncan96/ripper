package makemkv

import (
	"os/exec"
	"strconv"
)

func Info(dev string) ([]byte, error) {
	return exec.Command("makemkvcon", "-r", "info", "dev:"+dev).CombinedOutput() // #nosec G204 -- Input validated prior to injection
}

func Make(track int, dev string, dir string) ([]byte, error) {
	return exec.Command("makemkvcon", "mkv", "dev:"+dev, strconv.Itoa(track), dir).CombinedOutput() // #nosec G204 -- Input validated prior to injection
}
