package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// AppMetadata stores detailed registration fields for an installed tool
type AppMetadata struct {
	Name             string   `json:"name"`
	Version          string   `json:"version"`
	InstalledAt      string   `json:"installed_at"`
	ArchiveSource    string   `json:"archive_source"`
	UpdateURL        string   `json:"update_url"`
	BinaryPath       string   `json:"binary_path"`
	SymlinkPath      string   `json:"symlink_path"`
	DesktopPath      string   `json:"desktop_path"`
	IconPath         string   `json:"icon_path"`
	InstallScript    string   `json:"install_script,omitempty"`
	SelectedBinaries []string `json:"selected_binaries,omitempty"`
}

// Registry maps tool names to their individual metadata entries
type Registry struct {
	Apps map[string]AppMetadata `json:"apps"`
}

var registryPathOverride string

// GetRegistryPath returns the absolute path to the registry file
func GetRegistryPath() (string, error) {
	if registryPathOverride != "" {
		return registryPathOverride, nil
	}

	// Check standard XDG config location first for backward compatibility
	configHome := GetXdgConfigHome()
	if configHome != "" {
		legacyPath := filepath.Join(configHome, "plop", "registry.json")
		if _, err := os.Stat(legacyPath); err == nil {
			return legacyPath, nil
		}
	}

	// Default to XDG data home (or ~/.local/share/plop/registry.json)
	dataHome := GetXdgDataHome()
	if dataHome == "" {
		return "", fmt.Errorf("could not determine home or XDG_DATA_HOME directory")
	}
	return filepath.Join(dataHome, "plop", "registry.json"), nil
}

// LoadRegistry loads the application registration mapping
func LoadRegistry() (*Registry, error) {
	registryPath, err := GetRegistryPath()
	if err != nil {
		return nil, err
	}

	// Make sure config directory exists
	err = os.MkdirAll(filepath.Dir(registryPath), 0755)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(registryPath); os.IsNotExist(err) {
		// Return a fresh registry
		return &Registry{Apps: make(map[string]AppMetadata)}, nil
	}

	data, err := os.ReadFile(registryPath)
	if err != nil {
		return nil, err
	}

	// In case file is empty
	if len(data) == 0 {
		return &Registry{Apps: make(map[string]AppMetadata)}, nil
	}

	var reg Registry
	err = json.Unmarshal(data, &reg)
	if err != nil {
		return nil, err
	}

	if reg.Apps == nil {
		reg.Apps = make(map[string]AppMetadata)
	}

	return &reg, nil
}

// SaveRegistry saves the application registration mapping
func SaveRegistry(reg *Registry) error {
	registryPath, err := GetRegistryPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(registryPath, data, 0644)
}
