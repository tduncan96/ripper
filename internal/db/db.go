package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"

	_ "modernc.org/sqlite"
)

type Record struct {
	RunID       int
	StartTime   string
	EndTime     string
	ExitCode    int
	Device      string
	Title       string
	RawTitle    string
	Destination string
	TotalRipMB  int
	TotalMvMB   int
	RipLog      string
}

//go:embed schema.sql
var schema string
var RipRecordDB *sql.DB

func Init(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}

	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}

	return db, nil
}

func Close(db *sql.DB) {
	err := db.Close()
	if err != nil {
		fmt.Printf("error closing db: %s", err)
		os.Exit(1)
	}
}

func (r *Record) CreateRecord() (int64, error) {
	result, err := RipRecordDB.Exec(
		`INSERT INTO Runs (StartTime, EndTime, ExitCode, Device, Title, RawTitle, Destination, TotalRipMB, TotalMvMb, RipLog)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.StartTime,
		r.EndTime,
		r.ExitCode,
		r.Device,
		r.Title,
		r.RawTitle,
		r.Destination,
		r.TotalRipMB,
		r.TotalMvMB,
		r.RipLog,
	)
	if err != nil {
		return 0, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}

	r.RunID = int(id)
	return id, nil
}

func GetAllRecords() ([]Record, error) {
	var records []Record
	rows, err := RipRecordDB.Query("SELECT * FROM Runs")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var r Record
		err := rows.Scan(
			&r.RunID,
			&r.StartTime,
			&r.EndTime,
			&r.ExitCode,
			&r.Device,
			&r.Title,
			&r.RawTitle,
			&r.Destination,
			&r.TotalRipMB,
			&r.TotalMvMB,
			&r.RipLog,
		)
		if err != nil {
			fmt.Printf("Error collecting record from DB: %s", err)
			continue
		}
		records = append(records, r)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

func GetRecord(id int64) (Record, error) {
	var record Record
	err := RipRecordDB.QueryRow("SELECT * FROM Runs WHERE RunID = ?", id).Scan(
		&record.RunID,
		&record.StartTime,
		&record.EndTime,
		&record.ExitCode,
		&record.Device,
		&record.Title,
		&record.RawTitle,
		&record.Destination,
		&record.TotalRipMB,
		&record.TotalMvMB,
		&record.RipLog,
	)
	if err != nil {
		return Record{}, err
	}

	return record, nil
}
