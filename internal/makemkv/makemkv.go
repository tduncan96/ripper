package makemkv

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"ripper/internal/prflt"
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
	Bytes     int64  `json:"bytes"`      // TINFO:X,11
	Source    string `json:"source"`     // BR -> TINFO:X,16 | DVD -> TINFO:X,24
	Segments  string `json:"segments"`   // TINFO:X,26
	FileName  string `json:"file_name"`  // TINFO:X,27
	TreeName  string `json:"tree_name"`  // TINFO:X,30
	Order     int    `json:"order"`
	Status    bool   `json:"status"`
}

type Disc struct {
	Title      string   `json:"title"`
	Device     string   `json:"device"`
	Media      string   `json:"media"`
	Season     int      `json:"season"`
	Tracks     []*Track `json:"tracks"`
	SelTracks  []int    `json:"sel_tracks"`
	TotalBytes int64    `json:"total_bytes"`
	Staging    string   `json:"staging"`
	Perm       string   `json:"perm"`
	Status     bool     `json:"status"`
}

func Info(dev string) ([]byte, error) {
	return exec.Command("makemkvcon", "-r", "info", "dev:"+dev).CombinedOutput() // #nosec G204 -- Input validated prior to injection
}

func ParseInfo(b []byte) (disc Disc, err error) {
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
	} else {
		titleLine := discRe.FindStringSubmatch(lines[titleInd])
		if titleLine == nil {
			errs = append(errs, fmt.Errorf("invalid title from MakeMKV Info"))
			title = "ERROR"
		} else {
			title = discTitleRe.ReplaceAllString(titleLine[1], "")
			title = spaceRe.ReplaceAllString(title, " ")
			title = strings.TrimSpace(title)
			title = strings.ToLower(title)
			title = wordRe.ReplaceAllStringFunc(title, strings.ToUpper)
		}
	}

	disc.Title = title

	lines = slices.DeleteFunc(lines, func(l string) bool { return !trackRe.MatchString(l) })
	for _, line := range lines {
		matches := trackRe.FindStringSubmatch(line)

		id, err := strconv.Atoi(matches[1])
		if err != nil {
			errs = append(errs, err)
			continue
		}

		trackInd := slices.IndexFunc(disc.Tracks, func(t *Track) bool { return t.ID == id })

		if trackInd == -1 {
			disc.Tracks = append(disc.Tracks, &Track{ID: id})
			trackInd = len(disc.Tracks) - 1
		}
		track := disc.Tracks[trackInd]

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
			track.Bytes = int64(b)
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

	slices.SortStableFunc(disc.Tracks, func(a, b *Track) int { return cmp.Compare(a.Order, b.Order) })
	for i, track := range disc.Tracks {
		track.Order = i
	}

	for _, track := range disc.Tracks {
		disc.TotalBytes += track.Bytes
	}

	return disc, errors.Join(errs...)
}

func (d *Disc) SetDests() (err error) {
	var relPath string
	switch d.Media {
	case "Movie":
		relPath = filepath.Join(d.Media, d.Title)
	case "Show":
		relPath = filepath.Join(d.Media, d.Title, "Season "+strconv.Itoa(d.Season))
	}

	stagingRoot, err := os.OpenRoot(prflt.MasterConfig.RipConfig.Staging)
	if err != nil {
		return err
	}
	defer func() {
		if cErr := stagingRoot.Close(); err != nil {
			err = fmt.Errorf("error closing staging root dir: %w", cErr)
		}
	}()
	permRoot, err := os.OpenRoot(prflt.MasterConfig.RipConfig.Permanent)
	if err != nil {
		return err
	}
	defer func() {
		if cErr := permRoot.Close(); err != nil {
			err = fmt.Errorf("error closing permanent root dir: %w", cErr)
		}
	}()

	err = stagingRoot.MkdirAll(relPath, 0o700)
	if err != nil {
		return err
	}
	err = permRoot.MkdirAll(relPath, 0o700)
	if err != nil {
		return err
	}

	d.Staging = filepath.Join(stagingRoot.Name(), relPath)
	d.Perm = filepath.Join(permRoot.Name(), relPath)

	return err
}

func Make(i int, dev string, dir string) ([]byte, error) {
	return exec.Command("makemkvcon", "mkv", "dev:"+dev, strconv.Itoa(i), dir).CombinedOutput() // #nosec G204 -- Input validated prior to injection
}

func (d *Disc) Rip() (out []byte, err error) {
	var errs []error

	if err := d.SetDests(); err != nil {
		return out, err
	}

	root, err := os.OpenRoot(d.Staging)
	if err != nil {
		return out, err
	}
	defer func() {
		if cErr := root.Close(); cErr != nil {
			errs = append(errs, fmt.Errorf("error closing root: %w", cErr))
		}
	}()

	if d.SelTracks == nil {
		sizeSort := slices.Clone(d.Tracks)
		slices.SortStableFunc(sizeSort, func(a, b *Track) int { return cmp.Compare(a.Bytes, b.Bytes) })
		switch d.Media {
		case "Movie":
			d.SelTracks = []int{sizeSort[len(sizeSort)-1].ID}
		case "Show":
			anchor := sizeSort[len(sizeSort)-2].Bytes
			for _, track := range d.Tracks {
				if track.Bytes < anchor*130/100 || track.Bytes > anchor*70/100 {
					d.SelTracks = append(d.SelTracks, track.ID)
				}
			}
		}
	}

	tracks := slices.Clone(d.Tracks)
	tracks = slices.DeleteFunc(tracks, func(t *Track) bool { return !slices.Contains(d.SelTracks, t.ID) })
	for _, track := range tracks {
		errCount := len(errs)
		dir := strconv.Itoa(track.Order)
		err = root.Mkdir(dir, 0o700)
		if errors.Is(err, fs.ErrExist) {
			dir = fmt.Sprintf("%d_%d", track.ID, track.Order)
			err = root.Mkdir(dir, 0o700)
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}

		path := filepath.Join(root.Name(), dir)
		trackOut, err := Make(track.ID, d.Device, path)
		if err != nil {
			errs = append(errs, err)
		}

		divString := fmt.Sprintf("\n===== Track %v || Order %v =====\n", track.ID, track.Order)
		out = append(out, []byte(divString)...)
		out = append(out, trackOut...)

		if len(errs) == errCount {
			track.Status = true
		}
	}

	d.Status = true
	for _, track := range tracks {
		if !track.Status {
			d.Status = false
		}
	}

	return out, errors.Join(errs...)
}

func Mime(root os.Root) error {

	return nil
}

func (d *Disc) Promote() (err error) {
	var errs []error
	stagingRoot, err := os.OpenRoot(d.Staging)
	if err != nil {
		return err
	}
	defer func() {
		if cErr := stagingRoot.Close(); cErr != nil {
			errs = append(errs, fmt.Errorf("error closing root: %w", cErr))
		}
	}()

	return nil
}
