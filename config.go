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
	AppsDir     string `json:"apps_dir"`
	IconsDir    string `json:"icons_dir"`
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
	val := os.Getenv("XDG_CONFIG_HOME")
	if val != "" {
		return val
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config")
}

// GetXdgDataHome returns the base directory for data files
func GetXdgDataHome() string {
	val := os.Getenv("XDG_DATA_HOME")
	if val != "" {
		return val
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share")
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

	// If config file doesn't exist, create it with default values
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		binDir := "~/.local/bin"
		if val := os.Getenv("XDG_BIN_HOME"); val != "" {
			binDir = val
		}

		appsDir := "~/.local/share/applications"
		iconsDir := "~/.local/share/icons"
		if val := os.Getenv("XDG_DATA_HOME"); val != "" {
			appsDir = filepath.Join(val, "applications")
			iconsDir = filepath.Join(val, "icons")
		}

		// Create default config with tilde-prefixed or XDG-environment standard paths
		cfg := &Config{
			OptDir:      "~/.local/opt",
			BinDir:      binDir,
			AppsDir:     appsDir,
			IconsDir:    iconsDir,
			GithubToken: "",
			AutoConfirm: false,
			DefaultGUI:  nil,
		}

		data, err := json.MarshalIndent(cfg, "", "  ")
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
