package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config represents the plop user configuration
type Config struct {
	OptDir      string `json:"opt_dir"`
	BinDir      string `json:"bin_dir"`
	AppsDir     string `json:"apps_dir,omitempty"`
	IconsDir    string `json:"icons_dir,omitempty"`
	GithubToken string `json:"github_token"`
	AutoConfirm bool   `json:"auto_confirm"`
	DefaultGUI  *bool  `json:"default_gui"`
}

// ExpandTilde replaces the leading ~ in a path with the user's home directory
func ExpandTilde(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		// Avoid double separator if path is just "~" or starts with "~/"
		if path == "~" {
			return home
		}
		if strings.HasPrefix(path, "~/") {
			return filepath.Join(home, path[2:])
		}
		return filepath.Join(home, path[1:])
	}
	return path
}

// GetXdgConfigHome returns the base directory for configurations
func GetXdgConfigHome() string {
	dir := PlatformConfigDir()
	if dir == "" {
		return ""
	}
	return filepath.Dir(dir)
}

// GetXdgDataHome returns the base directory for data files
func GetXdgDataHome() string {
	dir := PlatformDataDir()
	if dir == "" {
		return ""
	}
	return filepath.Dir(dir)
}

// GetConfigPath returns the absolute path to the configuration file
func GetConfigPath() (string, error) {
	configHome := GetXdgConfigHome()
	if configHome == "" {
		return "", fmt.Errorf("could not determine home or XDG_CONFIG_HOME directory")
	}
	return filepath.Join(configHome, "plop", "config.json"), nil
}

// LoadConfig loads the user configuration, creating default file if missing
func LoadConfig() (*Config, error) {
	configPath, err := GetConfigPath()
	if err != nil {
		return nil, err
	}

	// Make sure config directory exists
	err = os.MkdirAll(filepath.Dir(configPath), 0755)
	if err != nil {
		return nil, err
	}

	// If config file doesn't exist, create it with platform-appropriate defaults
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		cfg := PlatformDefaultConfig()

		// On macOS, exclude apps_dir and icons_dir from the JSON file
		// (desktop integration is not applicable). The omitempty tag handles
		// this when we marshal a copy with empty values.
		var data []byte
		if IsDarwin() {
			jsonCfg := *cfg
			jsonCfg.AppsDir = ""
			jsonCfg.IconsDir = ""
			data, err = json.MarshalIndent(&jsonCfg, "", "  ")
		} else {
			data, err = json.MarshalIndent(cfg, "", "  ")
		}
		if err != nil {
			return nil, err
		}

		err = os.WriteFile(configPath, data, 0644)
		if err != nil {
			return nil, err
		}
		return cfg, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = json.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	return &cfg, nil
}
