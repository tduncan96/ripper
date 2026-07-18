package db

import (
	_ "embed"

	"ripper/internal/prflt"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string
var DB *sqlx.DB

func Init() (*sqlx.DB, error) {
	path := prflt.MasterConfig.RipConfig.RipDbPath
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=user_version(1)"
	cnxn, err := sqlx.Connect("sqlite", dsn)
	if err != nil {
		return nil, err
	}

	if _, err := cnxn.Exec(schema); err != nil {
		return nil, err
	}

	DB = cnxn
	return DB, nil
}

func Close(db *sqlx.DB) error {
	if err := db.Close(); err != nil {
		return err
	}
	return nil
}
