package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"time"

	"ripper/internal/prflt"
)

type Status struct {
	Start          string  `json:"start"`
	StartEpoch     int     `json:"start_epoch"`
	RunPID         int     `json:"run_pid"`
	MKVPID         int     `json:"mkv_pid"`
	Phase          string  `json:"phase"`
	RawTitle       string  `json:"raw_title"`
	Title          string  `json:"title"`
	Dest           string  `json:"dest"`
	FullDest       string  `json:"full_dest"`
	CurrentRipMB   int     `json:"cur_rip_mb"`
	CurrentMvMB    int     `json:"cur_mv_mb"`
	TotalRipMB     int     `json:"total_rip_mb"`
	TotalMvMB      int     `json:"total_mv_mb"`
	ElapsedSeconds int     `json:"elapsed_seconds"`
	SelTracks      string  `json:"sel_tracks"`
	Updated        string  `json:"updated"`
	UpdatedEpoch   float64 `json:"updated_epoch"`
	Drive          string  `json:"drive"`
	Device         string  `json:"device"`
	RipLog         string  `json:"rip_log"`
}

var (
	statusRoot    *os.Root
	statusTmpPath string
	statusFile    string
	statusGlob    string
)

func OpenStatusFile() {
	statusTmpPath = prflt.MasterConfig.RipConfig.StatusTmp
	statusGlob = filepath.Base(statusTmpPath)
	dir := filepath.Dir(statusTmpPath)

	var err error
	statusRoot, err = os.OpenRoot(dir)
	if err != nil {
		log.Fatalf("opening status root %q: %v", dir, err)
	}
}

func getStatuses() ([]Status, error) {
	fsys := statusRoot.FS()

	matches, err := fs.Glob(fsys, statusGlob)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	statuses := make([]Status, 0, len(matches))
	for _, name := range matches {
		data, err := fs.ReadFile(fsys, name)
		if err != nil {
			log.Printf("skip %s: %v", name, err)
			continue
		}
		var s Status
		if err := json.Unmarshal(data, &s); err != nil {
			log.Printf("skip %s: bad json: %v", name, err)
			continue
		}
		statuses = append(statuses, s)
	}
	return statuses, nil
}

func JsonHandler(w http.ResponseWriter, r *http.Request) {
	statuses, err := getStatuses()
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

func LogHandler(w http.ResponseWriter, r *http.Request) {
	statuses, err := getStatuses()
	if err != nil {
		log.Printf("getStatuses failed: %v", err)
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

//go:embed status_page.gohtml
var templateFS embed.FS
var statusTmpl = template.Must(template.ParseFS(templateFS, "status_page.gohtml"))

func StatusHandler(w http.ResponseWriter, r *http.Request) {
	statuses, err := getStatuses()
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

func clampFill(pct int) int {
	if pct > 100 {
		return 100
	}
	if pct < 0 {
		return 0
	}
	return pct
}

func overflow(pct int) int {
	if pct > 100 {
		return pct - 100
	}
	return 0
}

func (s *Status) RipFill() int {
	return clampFill(s.RipPercent())
}

func (s *Status) MoveFill() int {
	return clampFill(s.MovePercent())
}

func (s *Status) RipOverflow() int {
	return overflow(s.RipPercent())
}

func (s *Status) MoveOverflow() int {
	return overflow(s.MovePercent())
}

func (s *Status) ElapsedHMS() string {
	sec := s.ElapsedSeconds
	if sec < 0 {
		sec = 0
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	ss := sec % 60
	return fmt.Sprintf("%d:%02d:%02d", h, m, ss)
}

func Serve() {
	OpenStatusFile()

	http.HandleFunc("GET /{$}", StatusHandler)
	http.HandleFunc("GET /json", JsonHandler)
	http.HandleFunc("GET /logs/{drv}", LogHandler)

	srv := &http.Server{
		Addr:         ":9511",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Fatal(srv.ListenAndServe())
}
