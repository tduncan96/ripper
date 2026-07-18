package makemkv

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	trackRe     = regexp.MustCompile(`^TINFO:([0-9]+),([0-9]+),0,\"(.+)\"`)
	discRe      = regexp.MustCompile(`^CINFO:32,0,\"(.+)\"`)
	discTitleRe = regexp.MustCompile(`(?i)[._ -]+(SEASON|S|DISC|D|WW|BOOK|VOLUME)[._ -]*[0-9]+([._ -]*(DISC|D)[._ -]*[0-9]+)?[._ -]*$`)
	spaceRe     = regexp.MustCompile(`[_\s]+`)
	sourceRe    = regexp.MustCompile(`([0-9]+)\.(?:mpls|m2ts)`)
	wordRe      = regexp.MustCompile(`(^| )([a-z])`)
)

type Track struct {
	ID        int    `json:"id"`         // X
	Title     string `json:"title"`      // TINFO:X,2
	Duration  string `json:"duration"`   // TINFO:X,9
	SizeHuman string `json:"size_human"` // TINFO:X,10
	Bytes     uint64 `json:"bytes"`      // TINFO:X,11
	Source    string `json:"source"`     // BR -> TINFO:X,16 | DVD -> TINFO:X,24
	Segments  string `json:"segments"`   // TINFO:X,26
	FileName  string `json:"file_name"`  // TINFO:X,27
	TreeName  string `json:"tree_name"`  // TINFO:X,30
	Selected  bool   `json:"selected"`
	Order     int    `json:"order"`
	Status    bool   `json:"status"`
}

type Disc struct {
	Raw        string         `json:"raw_title"`
	Title      string         `json:"title"`
	Device     string         `json:"device"`
	Media      string         `json:"media"`
	Season     int            `json:"season"`
	Tracks     map[int]*Track `json:"tracks"`
	TotalBytes uint64         `json:"total_bytes"`
	Staging    string         `json:"staging"`
	Perm       string         `json:"perm"`
	CherryP    bool           `json:"cherry_picked"`
	Status     bool           `json:"status"`
	Log        []byte         `json:"log"`
	Info       []byte         `json:"info"`
}

func ParseInfo(b []byte) (disc Disc, err error) {
	disc.Info = b
	
	var lines []string
	scanner := bufio.NewScanner(bytes.NewReader(b))
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err = scanner.Err(); err != nil {
		return disc, err
	}

	var errs []error
	var title string

	titleInd := slices.IndexFunc(lines, func(l string) bool { return discRe.MatchString(l) })
	if titleInd == -1 {
		errs = append(errs, fmt.Errorf("no title line from MakeMKV Info"))
		title = "ERROR"
		disc.Raw = title
	} else {
		titleLine := discRe.FindStringSubmatch(lines[titleInd])
		if titleLine == nil {
			errs = append(errs, fmt.Errorf("invalid title from MakeMKV Info"))
			title = "ERROR"
			disc.Raw = title
		} else {
			disc.Raw = titleLine[1]
			title = discTitleRe.ReplaceAllString(titleLine[1], "")
			title = spaceRe.ReplaceAllString(title, " ")
			title = strings.TrimSpace(title)
			title = strings.ToLower(title)
			title = wordRe.ReplaceAllStringFunc(title, strings.ToUpper)
		}
	}

	disc.Title = title
	disc.Tracks = make(map[int]*Track)

	lines = slices.DeleteFunc(lines, func(l string) bool { return !trackRe.MatchString(l) })
	for _, line := range lines {
		matches := trackRe.FindStringSubmatch(line) //TINFO:(matches[1]),(matches[2]),0,(matches[3])

		id, err := strconv.Atoi(matches[1])
		if err != nil {
			errs = append(errs, err)
			continue
		}

		track, ok := disc.Tracks[id]
		if !ok {
			disc.Tracks[id] = &Track{ID: id}
			track = disc.Tracks[id]
		}

		field, err := strconv.Atoi(matches[2])
		if err != nil {
			errs = append(errs, err)
			continue
		}

		switch field {
		case 2:
			track.Title = matches[3]
		case 9:
			track.Duration = matches[3]
		case 10:
			track.SizeHuman = matches[3]
		case 11:
			b, err := strconv.Atoi(matches[3])
			if err != nil {
				errs = append(errs, err)
				continue
			}
			track.Bytes = uint64(b) // #nosec G115 -- File size cannot be negative.
		case 16:
			track.Source = matches[3]
			if m := sourceRe.FindStringSubmatch(matches[3]); m != nil {
				order, err := strconv.Atoi(m[1])
				if err != nil {
					errs = append(errs, err)
				}
				track.Order = order
			}
		case 24:
			if track.Source == "" {
				track.Source = matches[3]
				order, err := strconv.Atoi(matches[3])
				if err != nil {
					errs = append(errs, err)
					continue
				}
				track.Order = order
			}
		case 26:
			track.Segments = matches[3]
		case 27:
			track.FileName = matches[3]
		case 30:
			track.TreeName = matches[3]
		}
	}

	ordered := slices.SortedStableFunc(maps.Values(disc.Tracks), func(a, b *Track) int { return cmp.Compare(a.Order, b.Order) })
	for i, track := range ordered {
		track.Order = i
	}

	for _, track := range disc.Tracks {
		disc.TotalBytes += track.Bytes
	}

	return disc, errors.Join(errs...)
}
