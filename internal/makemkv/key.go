package makemkv

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const keyURL = "https://forum.makemkv.com/forum/viewtopic.php?t=1053"

var (
	keyRe        = regexp.MustCompile(`T-[A-Za-z0-9_+/=@-]{40,}`)
	keyExpiredRe = regexp.MustCompile(`MSG:(5052|5055),`)
)

func KeyExpired(info []byte) bool {
	return keyExpiredRe.Match(info)
}

func RefreshKey() error {
	client := &http.Client{Timeout: 25 * time.Second}

	req, err := http.NewRequest(http.MethodGet, keyURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64)")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("makemkv forum returned %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	key := keyRe.FindString(string(body))
	if key == "" {
		return fmt.Errorf("no beta key found on forum page")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	dir := filepath.Join(home, ".MakeMKV")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()

	config, err := fs.ReadFile(root.FS(), "settings.conf")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(config))
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "app_Key") {
			continue
		}
		out = append(out, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return err
	}
	out = append(out, fmt.Sprintf("app_Key = %q", key))
	content := strings.Join(out, "\n") + "\n"

	tmp, err := root.Create("settings.conf.tmp")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	return root.Rename("settings.conf.tmp", "settings.conf")
}
