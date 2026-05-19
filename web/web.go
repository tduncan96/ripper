package web

import (
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"encoding/json"
	"embed"

	"github.com/joho/godotenv"
)

type Status struct {
	Start string `json:"start"`
	StartEpoch int `json:"start_epoch"`
	RunPID int `json:"run_pid"`
	MKVPID int `json:"mkv_pid"`
	Phase string `json:"phase"`
	RawTitle string `json:"raw_title"`
	Title string `json:"title"`
	Destination string `json:"destination"`
	CurrentRipMB int `json:"cur_rip_mb"`
	CurrentMvMB int `json:"cur_mv_mb"`
	TotalRipMB int `json:"total_rip_mb"`
	TotalMvMB int `json:"total_mv_mb"`
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

	data, err := json.Marshal(status)
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

//go:embed status_page.html
var templateFS embed.FS
var statusTmpl = template.Must(template.ParseFS(templateFS, "status_page.html"))

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

func (s *Status) RipPercent() int {
	if s.TotalRipMB == 0 {
		return 0
	}
	return s.CurrentRipMB * 100 / s.TotalRipMB
}

func (s *Status) MovePercent() int {
	if s.TotalMvMB == 0 {
		return 0
	}
	return s.CurrentMvMB * 100 / s.TotalMvMB
}