package main

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"testing/quick"
)

// TestPreservation_LegitimateAppSymlinksDeleted verifies that uninstalling an app
// correctly deletes symlinks pointing inside the app's own opt folder.
//
// Property: for all symlink targets where absTarget == appOptFolder ||
// strings.HasPrefix(absTarget, appOptFolder + string(filepath.Separator)),
// the symlink is removed after UninstallApp.
//
// This test MUST PASS on unfixed code, these cases already work correctly.
func TestPreservation_LegitimateAppSymlinksDeleted(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))

		// Generate a random app name (lowercase alpha, 3-10 chars)
		nameLen := r.Intn(8) + 3
		nameBytes := make([]byte, nameLen)
		for i := range nameBytes {
			nameBytes[i] = byte('a' + r.Intn(26))
		}
		appName := string(nameBytes)

		// Generate a random sub-path within the app's opt folder (1-3 segments)
		numSegments := r.Intn(3) + 1
		segments := make([]string, numSegments)
		for i := range segments {
			segLen := r.Intn(8) + 3
			segBytes := make([]byte, segLen)
			for j := range segBytes {
				segBytes[j] = byte('a' + r.Intn(26))
			}
			segments[i] = string(segBytes)
		}

		// Set up temp directory structure
		tempDir := t.TempDir()
		optDir := filepath.Join(tempDir, "opt")
		binDir := filepath.Join(tempDir, "bin")

		if err := os.MkdirAll(optDir, 0755); err != nil {
			t.Logf("MkdirAll optDir failed: %v", err)
			return false
		}
		if err := os.MkdirAll(binDir, 0755); err != nil {
			t.Logf("MkdirAll binDir failed: %v", err)
			return false
		}

		// Create the app's opt folder and a binary inside it at the random sub-path
		appOptFolder := filepath.Join(optDir, appName)
		binaryPath := filepath.Join(appOptFolder, filepath.Join(segments...))
		binaryDir := filepath.Dir(binaryPath)

		if err := os.MkdirAll(binaryDir, 0755); err != nil {
			t.Logf("MkdirAll binaryDir failed: %v", err)
			return false
		}
		if err := os.WriteFile(binaryPath, []byte("#!/bin/sh\necho hello"), 0755); err != nil {
			t.Logf("WriteFile binary failed: %v", err)
			return false
		}

		// Create a symlink in binDir pointing to the binary inside the app's own opt folder
		symlinkName := segments[len(segments)-1] // use last segment as symlink name
		symlinkPath := filepath.Join(binDir, symlinkName)
		if err := os.Symlink(binaryPath, symlinkPath); err != nil {
			t.Logf("Symlink creation failed: %v", err)
			return false
		}

		// Set up registry with the app registered
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
		if err := SaveRegistry(reg); err != nil {
			t.Logf("SaveRegistry failed: %v", err)
			return false
		}

		// Configure with AutoConfirm so no prompt is shown
		config := &Config{
			OptDir:      optDir,
			BinDir:      binDir,
			AutoConfirm: true,
		}

		// Run UninstallApp
		err := UninstallApp(appName, config, reg)
		if err != nil {
			t.Logf("UninstallApp failed: %v", err)
			return false
		}

		// Assert that the symlink is deleted (it pointed inside the app's own folder)
		if _, err := os.Lstat(symlinkPath); err == nil {
			t.Logf("FAILURE: symlink %q still exists after uninstalling %q (target was inside app's own folder: %q)",
				symlinkPath, appName, binaryPath)
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Preservation property failed: legitimate app symlinks were not deleted: %v", err)
	}
}
