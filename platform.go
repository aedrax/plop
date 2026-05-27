package main

import (
	"os"
	"path/filepath"
	"runtime"
)

// IsDarwin returns true when running on macOS
func IsDarwin() bool {
	return runtime.GOOS == "darwin"
}

// PlatformConfigDir returns the base config directory for plop.
// On macOS without XDG_CONFIG_HOME: ~/Library/Application Support/plop
// On Linux without XDG_CONFIG_HOME: ~/.config/plop
// When XDG_CONFIG_HOME is set to a non-empty value on either platform,
// it takes precedence and returns $XDG_CONFIG_HOME/plop.
func PlatformConfigDir() string {
	if val := os.Getenv("XDG_CONFIG_HOME"); val != "" {
		return filepath.Join(val, "plop")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "plop")
	}
	return filepath.Join(home, ".config", "plop")
}

// PlatformDataDir returns the base data directory for plop.
// On macOS without XDG_DATA_HOME: ~/Library/Application Support/plop
// On Linux without XDG_DATA_HOME: ~/.local/share/plop
// When XDG_DATA_HOME is set to a non-empty value on either platform,
// it takes precedence and returns $XDG_DATA_HOME/plop.
func PlatformDataDir() string {
	if val := os.Getenv("XDG_DATA_HOME"); val != "" {
		return filepath.Join(val, "plop")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "plop")
	}
	return filepath.Join(home, ".local", "share", "plop")
}

// PlatformDefaultConfig returns a Config with platform-appropriate defaults.
// Respects XDG_BIN_HOME and XDG_DATA_HOME overrides on both platforms.
func PlatformDefaultConfig() *Config {
	binDir := "~/.local/bin"
	if val := os.Getenv("XDG_BIN_HOME"); val != "" {
		binDir = val
	}

	if runtime.GOOS == "darwin" {
		dataDir := PlatformDataDir()
		return &Config{
			OptDir:      "~/Applications/plop",
			BinDir:      binDir,
			AppsDir:     filepath.Join(dataDir, "applications"),
			IconsDir:    filepath.Join(dataDir, "icons"),
			GithubToken: "",
			AutoConfirm: false,
			DefaultGUI:  nil,
		}
	}

	// Linux defaults
	appsDir := "~/.local/share/applications"
	iconsDir := "~/.local/share/icons"
	if val := os.Getenv("XDG_DATA_HOME"); val != "" {
		appsDir = filepath.Join(val, "applications")
		iconsDir = filepath.Join(val, "icons")
	}

	return &Config{
		OptDir:      "~/.local/opt",
		BinDir:      binDir,
		AppsDir:     appsDir,
		IconsDir:    iconsDir,
		GithubToken: "",
		AutoConfirm: false,
		DefaultGUI:  nil,
	}
}
