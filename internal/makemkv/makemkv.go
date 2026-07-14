package makemkv

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"ripper/internal/prflt"
	"slices"
	"strconv"
)

var (
	epRe     = regexp.MustCompile(`S[0-9]+E\K[0-9]+`)
	mkvMagic = []byte{0x1a, 0x45, 0xdf, 0xa3}
)

func Info(dev string) ([]byte, error) {
	return exec.Command("makemkvcon", "-r", "info", "dev:"+dev).CombinedOutput() // #nosec G204 -- Input validated prior to injection
}

func Make(i int, dev string, dir string) ([]byte, error) {
	return exec.Command("makemkvcon", "mkv", "dev:"+dev, strconv.Itoa(i), dir).CombinedOutput() // #nosec G204 -- Input validated prior to injection
}

func (d *Disc) setDests() (err error) {
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

func verify(root *os.Root, fileName string) error {
	file, err := root.Open(fileName)
	if err != nil {
		return err
	}
	defer func() {
		if cErr := file.Close(); err != nil {
			err = cErr
		}
	}()

	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return err
	}
	buf = buf[:n]

	if len(buf) < 5 {
		return fmt.Errorf("file length too short: %d", len(buf))
	}

	if !bytes.HasPrefix(buf, mkvMagic) {
		return fmt.Errorf("file has incorrect file signature in header; expected %v, got %v", mkvMagic, buf[:4])
	}

	info, err := os.Stat(filepath.Join(root.Name(), fileName))
	if err != nil {
		return err
	}
	if info.Size() < 1<<25 {
		return fmt.Errorf("file too small: %d", info.Size())
	}

	return nil
}

func promote(staging, perm *os.Root, name string) (err error) {
	in, err := staging.Open(name)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := perm.Create(name)
	if err != nil {
		return err
	}
	defer func() {
		if cErr := out.Close(); cErr != nil {
			err = cErr
		}
	}()

	srcHash := sha256.New()
	tee := io.TeeReader(in, srcHash)

	buf := make([]byte, 1<<20)
	if _, err := io.CopyBuffer(out, tee, buf); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}

	check, err := perm.Open(name)
	if err != nil {
		return err
	}
	defer check.Close()

	dstHash := sha256.New()
	if _, err := io.Copy(dstHash, check); err != nil {
		return err
	}

	if !bytes.Equal(srcHash.Sum(nil), dstHash.Sum(nil)) {
		return fmt.Errorf("checksum mismatch while promoting %s", name)
	}

	if err := staging.Remove(name); err != nil {
		return fmt.Errorf("failed clean up while promoting %s: %w", name, err)
	}

	return nil
}

func (d *Disc) Rip() (out []byte, err error) {
	var errs []error

	if err := d.setDests(); err != nil {
		return out, err
	}

	staging, err := os.OpenRoot(d.Staging)
	if err != nil {
		return out, err
	}
	defer func() {
		if cErr := staging.Close(); cErr != nil {
			errs = append(errs, fmt.Errorf("error closing root: %w", cErr))
		}
	}()

	perm, err := os.OpenRoot(d.Perm)
	if err != nil {
		return out, err
	}
	defer func() {
		if cErr := perm.Close(); cErr != nil {
			errs = append(errs, cErr)
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
				if track.Bytes < anchor*130/100 && track.Bytes > anchor*70/100 {
					d.SelTracks = append(d.SelTracks, track.ID)
				}
			}
		}
	}

	tracks := slices.Clone(d.Tracks)
	tracks = slices.DeleteFunc(tracks, func(t *Track) bool { return !slices.Contains(d.SelTracks, t.ID) })

	var offset int
	if d.Media == "Show" {
		stagingDirs, err := fs.ReadDir(staging.FS(), ".")
		if err != nil {
			return out, err
		}

		for _, dir := range stagingDirs {
			if dir.IsDir() {
				id, err := strconv.Atoi(dir.Name())
				if err != nil {
					continue
				}
				tracks = slices.DeleteFunc(tracks, func(t *Track) bool { return t.ID == id })
			}
		}

		permFiles, err := fs.ReadDir(perm.FS(), ".")
		if err != nil {
			return out, err
		}

		for _, file := range permFiles {
			if !file.IsDir() {
				continue
			}
			ep, err := strconv.Atoi(epRe.FindStringSubmatch(file.Name())[0])
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if ep > offset {
				offset = ep
			}
		}
	}

	for _, track := range tracks {
		track.Status = true
		dir := strconv.Itoa(track.ID)
		err = staging.MkdirAll(dir, 0o700)
		if err != nil {
			errs = append(errs, err)
			track.Status = false
			continue
		}

		path := filepath.Join(staging.Name(), dir)
		trackOut, err := Make(track.ID, d.Device, path)
		divString := fmt.Sprintf("\n======== Track %v || Order %v ========\n", track.ID, track.Order)
		out = append(out, []byte(divString)...)
		out = append(out, trackOut...)
		if err != nil {
			errs = append(errs, err)
			track.Status = false
			continue
		}

		if err := verify(staging, filepath.Join(dir, track.FileName)); err != nil {
			errs = append(errs, err)
			track.Status = false
			continue
		}
		verify := fmt.Sprintf("file %v verified; renaming", track.FileName)
		out = append(out, []byte(verify)...)

		newName := fmt.Sprintf("%v S%dE%d", d.Title, d.Season, offset+track.Order)
		if err := staging.Rename(filepath.Join(dir, track.FileName), newName); err != nil {
			errs = append(errs, err)
			track.Status = false
			continue
		}
		track.FileName = newName
		rename := fmt.Sprintf("file %v renamed to %v", track.FileName, newName)
		out = append(out, []byte(rename)...)

		if err := promote(staging, perm, track.FileName); err != nil {
			errs = append(errs, err)
			track.Status = false
		}
	}

	d.Status = true
	for _, track := range tracks {
		if !track.Status {
			d.Status = false
		}
	}

	if d.Status {
		if rErr := staging.RemoveAll("."); rErr != nil {
			errs = append(errs, rErr)
		}
	}

	return out, errors.Join(errs...)
}
