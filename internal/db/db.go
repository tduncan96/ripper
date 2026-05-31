package db

import (
	"database/sql"
	"fmt"
	_ "embed"
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
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}

	db.Exec("PRAGMA journal_mode=WAL;")
	db.Exec("PRAGMA foreign_keys=ON;")

	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}

	return db, nil
}

func Close(db *sql.DB) {
	db.Close()
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
		return 0, nil
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, nil
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
	err := RipRecordDB.QueryRow("SELECT * FROM Runs WHERE RunID = ?").Scan(
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