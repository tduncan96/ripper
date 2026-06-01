package main

import (
	"fmt"
	"os"

	"ripper/cmd"
	"ripper/internal/db"
	"ripper/internal/prflt"
)

func main() {
	err := prflt.Init()
	if err != nil {
		fmt.Fprintf(os.Stderr, "preflight initialization error: %s.\n Exiting ...", err)
		os.Exit(1)
	}

	database, err := db.Init(prflt.MasterConfig.RipConfig.RipDBDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "database initialization error: %s.\n Exiting ...", err)
		os.Exit(1)
	}
	defer db.Close(database)

	db.RipRecordDB = database

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
