package prflt

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	SystemConfig struct {
		ConfigDir string
		ScriptDir string
	}
	RipConfig struct {
		Permanent string
		Staging   string
		StatusTmp string
		LogTmp    string
		NtfyURL   string
		RipDbPath string
	}
	LibrarianConfig struct {
		JellyURL    string
		JellyKey    string
		BookURL     string
		BookPageID  string
		BookTokenID string
		BookKey     string
	}
}

var MasterConfig Config

const configFilePath = "/etc/ripper/env"
const scriptFilePath = "/usr/local/libexec"

func (c *Config) validate() (ripError, librError error) {
	var ripMissing []string
	if c.RipConfig.Permanent == "" {
		ripMissing = append(ripMissing, "PERMANENT")
	}
	if c.RipConfig.Staging == "" {
		c.RipConfig.Staging = c.RipConfig.Permanent
		if c.RipConfig.Staging == "" {
			ripMissing = append(ripMissing, "STAGING")
		}
	}
	if c.RipConfig.StatusTmp == "" {
		c.RipConfig.StatusTmp = "/tmp/*.rip-status.json"
	}
	if c.RipConfig.LogTmp == "" {
		c.RipConfig.LogTmp = "/tmp/*.rip.log"
	}
	if c.RipConfig.NtfyURL == "" {
		ripMissing = append(ripMissing, "NTFY_URL")
	}
	if c.RipConfig.RipDbPath == "" {
		c.RipConfig.RipDbPath = "/opt/ripper/ripper.db"
	}

	if len(ripMissing) > 0 {
		ripError = fmt.Errorf("missing required configs from rip.env: %s", strings.Join(ripMissing, ", "))
	} else {
		ripError = nil
	}

	var librMissing []string
	if c.LibrarianConfig.JellyURL == "" {
		librMissing = append(librMissing, "JELLYFIN_URL")
	}
	if c.LibrarianConfig.JellyKey == "" {
		librMissing = append(librMissing, "JELLYFIN_API_KEY")
	}
	if c.LibrarianConfig.BookURL == "" {
		librMissing = append(librMissing, "BOOKSTACK_URL")
	}
	if c.LibrarianConfig.BookKey == "" {
		librMissing = append(librMissing, "BOOKSTACK_API_KEY")
	}
	if c.LibrarianConfig.BookPageID == "" {
		librMissing = append(librMissing, "BOOKSTACK_PAGE_ID")
	}
	if c.LibrarianConfig.BookTokenID == "" {
		librMissing = append(librMissing, "BOOKSTACK_TOKEN_ID")
	}

	if len(librMissing) > 0 {
		librError = fmt.Errorf("missing required configs from libr.env: %s", strings.Join(librMissing, ", "))
	} else {
		librError = nil
	}

	return ripError, librError
}

func ReadConfigFiles() (ripErr, librErr, error error) {
	MasterConfig.SystemConfig.ConfigDir = configFilePath
	MasterConfig.SystemConfig.ScriptDir = scriptFilePath

	root, err := os.OpenRoot(configFilePath)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err := root.Close(); err != nil {
			fmt.Println("Error closing root:", err.Error())
			return
		}
	}()

	configDir, err := root.Open(".")
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err := configDir.Close(); err != nil {
			fmt.Println("Error closing config directory:", err.Error())
			return
		}
	}()

	configList, err := configDir.ReadDir(0)
	if err != nil {
		return nil, nil, err
	}

	for _, file := range configList {
		f, err := root.Open(file.Name())
		if err != nil {
			return nil, nil, err
		}

		vars, err := godotenv.Parse(f)
		if err != nil {
			return nil, nil, err
		}

		err = f.Close()
		if err != nil {
			return nil, nil, err
		}

		switch file.Name() {
		case "rip.env":
			MasterConfig.RipConfig.Permanent = vars["PERMANENT"]
			MasterConfig.RipConfig.Staging = vars["STAGING"]
			MasterConfig.RipConfig.StatusTmp = vars["STATUS_TMP"]
			MasterConfig.RipConfig.LogTmp = vars["LOG_TMP"]
			MasterConfig.RipConfig.NtfyURL = vars["NTFY_URL"]
			MasterConfig.RipConfig.RipDbPath = vars["RIP_DB_PATH"]
		case "libr.env":
			MasterConfig.LibrarianConfig.JellyURL = vars["JELLYFIN_URL"]
			MasterConfig.LibrarianConfig.JellyKey = vars["JELLYFIN_API_KEY"]
			MasterConfig.LibrarianConfig.BookURL = vars["BOOKSTACK_URL"]
			MasterConfig.LibrarianConfig.BookPageID = vars["BOOKSTACK_PAGE_ID"]
			MasterConfig.LibrarianConfig.BookTokenID = vars["BOOKSTACK_TOKEN_ID"]
			MasterConfig.LibrarianConfig.BookKey = vars["BOOKSTACK_API_KEY"]
		}
	}

	ripErr, librErr = MasterConfig.validate()
	return ripErr, librErr, nil
}
