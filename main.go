package main

import (
	"os"
	"fmt"

	"ripper/cmd"
	"ripper/internal/prflt"
	"ripper/internal/db"
)

func main() {
	prflt.Init()
	
	database, err := db.Init(prflt.MasterConfig.RipConfig.RipDBDir)
	if err != nil {
		fmt.Print("Database initialization error. Exiting ...")
		os.Exit(1)
	}
	defer db.Close(database)

	db.RipRecordDB = database

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
