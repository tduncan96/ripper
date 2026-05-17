package web

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"encoding/json"

	"github.com/joho/godotenv"
)

type Status struct {
	Start string `json:"start"`
	StartEpoch int `json:"start_epoch"`
	RunPID int `json:"run_pid"`
	MKVPID int `json:"mkv_pid"`
	Phase string `json:"phase"`
	Title string `json:"title"`
	Destination string `json:"destination"`
	CurrentRipMB int `json:"cur_rip_mb"`
	CurrentMvMB int `json:"cur_mv_mb"`
	TotalMB int `json:"total_mb"`
	ElapsedSeconds int `json:"elapsed_seconds"`
	Updated string `json:"updated"`
	UpdatedEpoch float64 `json:"updated_epoch"`
}

var (
	statusRoot *os.Root
	statusFile string
)

func OpenStatusFile() {
	vars, err := godotenv.Read("/home/saturn-svc/.config/ripper/env.sh")
	if err != nil{
		log.Fatal(err)
	}
	statusTmpPath, ok := vars["STATUS_TMP"]
	if !ok {
		log.Fatal("STATUS_TMP not set in config")
	}

	dir := filepath.Dir(statusTmpPath)
	statusFile = filepath.Base(statusTmpPath)

	statusRoot, err = os.OpenRoot(dir)
	if err != nil {
        log.Fatalf("opening status root %q: %v", dir, err)
	}
	defer statusRoot.Close()
}

func getStatus() (*Status, error) {
	file, err := statusRoot.Open(statusFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}

	return &s, nil
}

func JsonHandler (w http.ResponseWriter, r *http.Request) {
	status, err := getStatus()
	if err != nil {
		log.Printf("gatherStatus failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json, err := json.Marshal(status)
	if err != nil {
		log.Printf("json serialization failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	fmt.Fprint(w, string(json))
}

var statusTmpl = template.Must(template.ParseFiles("status_page.html"))

func StatusHandler (w http.ResponseWriter, r *http.Request) {
	status, err := getStatus()
	if err != nil {
		log.Printf("gatherStatus failed: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

    w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := statusTmpl.Execute(w, status); err != nil {
		log.Printf("template execution error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}