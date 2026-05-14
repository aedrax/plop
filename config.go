package main

import (
	"fmt"
	"os"
	"path/filepath"
)

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

// GetConfigPath returns the absolute path to the configuration file
func GetConfigPath() (string, error) {
	configHome := GetXdgConfigHome()
	if configHome == "" {
		return "", fmt.Errorf("could not determine home or XDG_CONFIG_HOME directory")
	}
	return filepath.Join(configHome, "plop", "config.json"), nil
}
