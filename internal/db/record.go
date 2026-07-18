package db

import (
	"errors"
)

type Record struct {
	RunID       int64
	StartTime   string
	EndTime     string
	ExitCode    int64
	Device      string
	Title       string
	RawTitle    string
	Destination string
	TotalRipMB  int64
	TotalMvMB   int64
	RipLog      string
}

func (r *Record) CreateRecord() (err error) {
	result, err := DB.NamedExec(
		`INSERT INTO Runs (StartTime, EndTime, ExitCode, Device, Title, RawTitle, Destination, TotalRipMB, TotalMvMb, RipLog)
		VALUES (:RunID, :StartTime, :EndTime, :ExitCode, :Device, :Title, :RawTitle, :Destination, :TotalRipMB, :TotalMvMB, :RipLog)`,
		r,
	)
	if err != nil {
		return err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	r.RunID = id
	return nil
}

func GetRecord(id int64) (rec Record, err error) {
	err = DB.Get(&rec, "Select * From Jobs Where RunId = ?", id)
	return rec, err
}

func GetRecords(ids []int64) (recs []Record, err error) {
	var errs []error
	for _, id := range ids {
		r, err := GetRecord(id)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		recs = append(recs, r)
	}
	return recs, errors.Join(errs...)
}

func GetAllRecords() (records []Record, err error) {
	err = DB.Get(&records, "Select * From Jobs")
	return records, err
}
