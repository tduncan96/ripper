package main

import (
	"os"

	"ripper/cmd"
	"ripper/internal/prflt"
)

func main() {
	prflt.Init()

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
