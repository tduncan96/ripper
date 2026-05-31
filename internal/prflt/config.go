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
		Staging string
		Permanent    string
		StatusTmp  string
		LogTmp     string
		NtfyURL    string
	}
	LibrarianConfig struct {
		BookURL     string
		BookPageID  string
		BookTokenID string
		BookKey     string
		JellyURL string
		JellyKey    string
	}
}

var MasterConfig Config
var configFilePath string = "/etc/ripper/env"
var scriptFilePath string = "/usr/local/libexec"

func (c Config) validate() (ripError, librError error) {
	var ripMissing []string
	if c.RipConfig.Staging == "" {
		ripMissing = append(ripMissing, "STAGING")
	}
	if c.RipConfig.Permanent == "" {
		ripMissing = append(ripMissing, "PERMANENT")
	}
	if c.RipConfig.StatusTmp == "" {
		ripMissing = append(ripMissing, "STATUS_TMP")
	}
	if c.RipConfig.LogTmp == "" {
		ripMissing = append(ripMissing, "LOG_TMP")
	}
	if c.RipConfig.NtfyURL == "" {
		ripMissing = append(ripMissing, "NTFY_URL")
	}

	if len(ripMissing) > 0 {
		ripError = fmt.Errorf("Missing required configs from rip.env: %s", strings.Join(ripMissing, ", "))
	} else {
		ripError = nil
	}

	var librMissing []string
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
	if c.LibrarianConfig.JellyURL == "" {
		librMissing = append(librMissing, "JELLYFIN_URL")
	}	
	if c.LibrarianConfig.JellyKey == "" {
		librMissing = append(librMissing, "JELLYFIN_API_KEY")
	}

	if len(librMissing) > 0 {
		librError = fmt.Errorf("Missing required configs from libr.env: %s", strings.Join(librMissing, ", "))
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
	defer root.Close()

	configDir, err := root.Open(".")
	if err != nil {
		return nil, nil, err
	}
	defer configDir.Close()

	configList, err := configDir.ReadDir(-1)
	if err != nil {
		return nil, nil, err
	}

	for _, file := range configList {
		vars, err := godotenv.Read(configFilePath + file.Name())
		if err != nil {
			return nil, nil, err
		}
		if file.Name() == "rip.env" {
			MasterConfig.RipConfig.Staging = vars["STAGING"]
			MasterConfig.RipConfig.Permanent = vars["PERMANENT"]
			MasterConfig.RipConfig.StatusTmp = vars["STATUS_TMP"]
			MasterConfig.RipConfig.LogTmp = vars["LOG_TMP"]
			MasterConfig.RipConfig.NtfyURL = vars["NTFY_URL"]
		} else if file.Name() == "libr.env" {
			MasterConfig.LibrarianConfig.BookURL = vars["BOOKSTACK_URL"]
			MasterConfig.LibrarianConfig.BookPageID = vars["BOOKSTACK_PAGE_ID"]
			MasterConfig.LibrarianConfig.BookTokenID = vars["BOOKSTACK_TOKEN_ID"]
			MasterConfig.LibrarianConfig.BookKey = vars["BOOKSTACK_API_KEY"]
			MasterConfig.LibrarianConfig.JellyURL = vars["JELLYFIN_URL"]
			MasterConfig.LibrarianConfig.JellyKey = vars["JELLYFIN_API_KEY"]
		}
	}

	ripErr, librErr = MasterConfig.validate()
	return ripErr, librErr, nil
}
