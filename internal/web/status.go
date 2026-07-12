package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"ripper/internal/prflt"
	"sort"
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
	ExitCode       int     `json:"exit_code"`
}

var (
	statusRoot    *os.Root
	statusTmpPath string
	statusGlob    string
)

func OpenStatusFile() error {
	statusTmpPath = prflt.MasterConfig.RipConfig.StatusTmp
	statusGlob = filepath.Base(statusTmpPath)
	dir := filepath.Dir(statusTmpPath)

	var err error
	statusRoot, err = os.OpenRoot(dir)
	if err != nil {
		return err
	}

	return nil
}

func getCurrentStatuses() ([]Status, error) {
	matches, err := fs.Glob(statusRoot.FS(), statusGlob)
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	statuses := make([]Status, 0, len(matches))
	for _, name := range matches {
		data, err := fs.ReadFile(statusRoot.FS(), name)
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
	sec := max(s.ElapsedSeconds, 0)
	h := sec / 3600
	m := (sec % 3600) / 60
	ss := sec % 60
	return fmt.Sprintf("%d:%02d:%02d", h, m, ss)
}
