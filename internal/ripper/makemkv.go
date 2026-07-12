package ripper

import (
	"bufio"
	"io"
	"regexp"
	"slices"
	"strconv"
)

var trackRe = regexp.MustCompile(`^TINFO:([0-9]+),([0-9]+),0,\"(.+)\"`)

type Track struct {
	ID        int    `json:"id"`         // X
	Title     string `json:"title"`      // TINFO:X,2
	Duration  string `json:"duration"`   // TINFO:X,9
	SizeHuman string `json:"size_human"` // TINFO:X,10
	Bytes     int64  `json:"bytes"`      // TINFO:X,11
	Source    string `json:"source"`     // TINFO:X,16
	Segments  string `json:"segments"`   // TINFO:X,26
	FileName  string `json:"file_name"`  // TINFO:X,27
	TreeName  string `json:"tree_name"`  // TINFO:X,30

}

type Plan struct {
	Tracks     []*Track `json:"tracks"`
	SelTracks  []int    `json:"sel_tracks"`
	TotalBytes int64    `json:"total_bytes"`
}

func ParseInfo(r io.Reader, sel []int) (plan Plan, err error) {
	var lines []string
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err = scanner.Err(); err != nil {
		return plan, err
	}
	lines = slices.DeleteFunc(lines, func(l string) bool { return trackRe.MatchString(l) })

	for _, line := range lines {
		matches := trackRe.FindStringSubmatch(line)

		var id int
		var field int
		var value = matches[2]
		id, err = strconv.Atoi(matches[0])
		if err != nil {
			return plan, err
		}

		trackInd := slices.IndexFunc(plan.Tracks, func(t *Track) bool { return t.ID == id })

		if trackInd == -1 {
			plan.Tracks = append(plan.Tracks, &Track{ID: id})
		}
		track := plan.Tracks[trackInd]

		field, err = strconv.Atoi(matches[1])
		if err != nil {
			return plan, err
		}

		switch field {
		case 2:
			track.Title = value
		case 9:
			track.Duration = value
		case 10:
			track.SizeHuman = value
		case 11:
			b, err := strconv.Atoi(value)
			if err != nil {
				return plan, err
			}
			track.Bytes = int64(b)
		case 16:
			track.Source = value
		case 26:
			track.Segments = value
		case 27:
			track.FileName = value
		case 30:
			track.TreeName = value
		}
	}

	plan.SelTracks = sel
	plan.Tracks = slices.DeleteFunc(plan.Tracks, func(t *Track) bool { return slices.Contains(plan.SelTracks, t.ID) })

	for _, track := range plan.Tracks {
		plan.TotalBytes += track.Bytes
	}

	return plan, err
}
