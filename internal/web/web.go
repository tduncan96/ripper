package web

import (
	"embed"
	"encoding/json"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"ripper/internal/db"
)

func GetStatus(drv string) (s Status, err error) {
	file := strings.ReplaceAll(statusGlob, "*", drv)
	data, err := fs.ReadFile(statusRoot.FS(), file)
	if err != nil {
		return Status{}, err
	}

	if err := json.Unmarshal(data, &s); err != nil {
		return Status{}, err
	}

	return s, nil
}

func JsonHandler(w http.ResponseWriter, r *http.Request) {
	statuses, err := getCurrentStatuses()
	if err != nil {
		log.Printf("getStatuses failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data, err := json.Marshal(statuses)
	if err != nil {
		log.Printf("json serialization failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write(data); err != nil {
		log.Printf("write failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func currentLogHandler(w http.ResponseWriter, r *http.Request) {
	statuses, err := getCurrentStatuses()
	if err != nil {
		log.Printf("Failed to get current statuses: %v", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	i := slices.IndexFunc(statuses, func(d Status) bool { return d.Drive == r.PathValue("drv") })
	if i == -1 {
		http.Error(w, "unknown drive", http.StatusNotFound)
		return
	}
	s := statuses[i]

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if _, err := w.Write([]byte(s.RipLog)); err != nil {
		log.Printf("write failed: %v", err)
	}
}

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	statuses, err := getCurrentStatuses()
	if err != nil {
		log.Printf("getStatuses failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := statusTmpl.Execute(w, statuses); err != nil {
		log.Printf("template execution error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func recordsListHandler(w http.ResponseWriter, r *http.Request) {
	records, err := db.GetAllRecords()
	if err != nil {
		log.Printf("GetAllRecords failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := recordsTmpl.Execute(w, records); err != nil {
		log.Printf("template execution error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func recordHandler(w http.ResponseWriter, r *http.Request) {
	runId, err := strconv.Atoi(r.PathValue("run_id"))
	if err != nil {
		log.Printf("Failed to get record: %s", err)
	}
	record, err := db.GetRecord(int64(runId))
	if err != nil {
		log.Printf("Failed to get record: %s", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := recordTmpl.Execute(w, record); err != nil {
		log.Printf("template execution error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

//go:embed templates/* static/*
var assetFS embed.FS
var statusTmpl = template.Must(template.ParseFS(assetFS, "templates/status_page.gohtml"))
var recordsTmpl = template.Must(template.ParseFS(assetFS, "templates/records_page.gohtml"))
var recordTmpl = template.Must(template.ParseFS(assetFS, "templates/record.gohtml"))

func Serve() {
	err := OpenStatusFile()
	if err != nil {
		log.Fatalf("error opening status root: %v", err)
	}

	staticFS, err := fs.Sub(assetFS, "static")
	if err != nil {
		log.Fatalf("error with embeded static dir: %v", err)
		return
	}
	http.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))

	http.HandleFunc("GET /{$}", StatusHandler)
	http.HandleFunc("GET /json", JsonHandler)

	http.HandleFunc("GET /records", recordsListHandler)
	http.HandleFunc("GET /records/{run_id}", recordHandler)

	http.HandleFunc("GET /logs/current/{drv}", currentLogHandler)

	srv := &http.Server{
		Addr:         ":9511",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())
}
