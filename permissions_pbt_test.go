package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"testing/quick"
)

// TestBugCondition_ConfigFilePermissions verifies that LoadConfig() creates
// new config files with 0600 permissions (owner-only).
//
// EXPECTED: This test FAILS on unfixed code because LoadConfig() uses 0644.
func TestBugCondition_ConfigFilePermissions(t *testing.T) {
	f := func(token string) bool {
		tempDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tempDir)

		// Call LoadConfig which should create a new config file
		_, err := LoadConfig()
		if err != nil {
			t.Logf("LoadConfig error: %v", err)
			return false
		}

		configPath := filepath.Join(tempDir, "plop", "config.json")
		info, err := os.Stat(configPath)
		if err != nil {
			t.Logf("Stat error: %v", err)
			return false
		}

		perm := info.Mode().Perm()
		if perm != 0600 {
			t.Logf("COUNTEREXAMPLE: LoadConfig() creates config.json with %04o permissions instead of 0600", perm)
			return false
		}
		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 5}); err != nil {
		t.Errorf("Bug confirmed: %v", err)
	}
}

// TestBugCondition_RegistryFilePermissions verifies that SaveRegistry() writes
// the registry file with 0600 permissions (owner-only).
//
// EXPECTED: This test FAILS on unfixed code because SaveRegistry() uses 0644.
func TestBugCondition_RegistryFilePermissions(t *testing.T) {
	f := func(appName string) bool {
		if appName == "" {
			appName = "testapp"
		}

		tempDir := t.TempDir()
		registryPathOverride = filepath.Join(tempDir, "registry.json")
		defer func() { registryPathOverride = "" }()

		reg := &Registry{
			Apps: map[string]AppMetadata{
				appName: {
					Name:    appName,
					Version: "1.0.0",
				},
			},
		}

		err := SaveRegistry(reg)
		if err != nil {
			t.Logf("SaveRegistry error: %v", err)
			return false
		}

		info, err := os.Stat(registryPathOverride)
		if err != nil {
			t.Logf("Stat error: %v", err)
			return false
		}

		perm := info.Mode().Perm()
		if perm != 0600 {
			t.Logf("COUNTEREXAMPLE: SaveRegistry() creates registry.json with %04o permissions instead of 0600", perm)
			return false
		}
		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 5}); err != nil {
		t.Errorf("Bug confirmed: %v", err)
	}
}

// TestBugCondition_ExistingPermissiveFileRemediation verifies that loading an
// existing config file with 0644 permissions tightens it to 0600.
//
// EXPECTED: This test FAILS on unfixed code because LoadConfig() does not
// remediate existing permissive files.
func TestBugCondition_ExistingPermissiveFileRemediation(t *testing.T) {
	f := func(token string) bool {
		tempDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", tempDir)

		// Create a config file with permissive 0644 permissions (simulating old version)
		configDir := filepath.Join(tempDir, "plop")
		err := os.MkdirAll(configDir, 0755)
		if err != nil {
			t.Logf("MkdirAll error: %v", err)
			return false
		}

		configPath := filepath.Join(configDir, "config.json")
		cfg := &Config{
			OptDir:      "~/.local/opt",
			BinDir:      "~/.local/bin",
			GithubToken: token,
		}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			t.Logf("MarshalIndent error: %v", err)
			return false
		}

		// Write with permissive 0644 permissions (simulating old plop version)
		err = os.WriteFile(configPath, data, 0644)
		if err != nil {
			t.Logf("WriteFile error: %v", err)
			return false
		}

		// Load the config - this should remediate permissions to 0600
		_, err = LoadConfig()
		if err != nil {
			t.Logf("LoadConfig error: %v", err)
			return false
		}

		info, err := os.Stat(configPath)
		if err != nil {
			t.Logf("Stat error: %v", err)
			return false
		}

		perm := info.Mode().Perm()
		if perm != 0600 {
			t.Logf("COUNTEREXAMPLE: LoadConfig() leaves existing config.json with %04o permissions instead of remediating to 0600", perm)
			return false
		}
		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 5}); err != nil {
		t.Errorf("Bug confirmed: %v", err)
	}
}
