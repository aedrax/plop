package main

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"testing/quick"
)

// TestBugCondition_PrefixCollidingSymlinksDeletedOnUninstall verifies that
// uninstalling an app whose name is a string prefix of another app incorrectly
// deletes symlinks belonging to the longer-named app.
//
// Bug condition: strings.HasPrefix(absTarget, appOptFolder) matches paths like
// /opt/myapp2/binary against /opt/myapp because it does not enforce a path
// separator boundary.
//
// EXPECTED: This test FAILS on unfixed code, failure confirms the bug exists.
func TestBugCondition_PrefixCollidingSymlinksDeletedOnUninstall(t *testing.T) {
	f := func(seed int64) bool {
		rng := rand.New(rand.NewSource(seed))

		// Generate a base app name (3-10 lowercase alpha chars)
		baseLen := 3 + rng.Intn(8)
		baseNameBytes := make([]byte, baseLen)
		for i := range baseNameBytes {
			baseNameBytes[i] = byte('a' + rng.Intn(26))
		}
		baseName := string(baseNameBytes)

		// Generate a suffix that does NOT start with filepath.Separator
		// This creates the prefix-collision condition (e.g., "myapp" vs "myapp2")
		suffixOptions := []string{
			"2",
			"-extra",
			"app",
			"123",
			"-pro",
			"x",
			"-ng",
			"plus",
		}
		suffix := suffixOptions[rng.Intn(len(suffixOptions))]
		longerName := baseName + suffix

		// Set up temp directory structure
		tempDir := t.TempDir()
		optDir := filepath.Join(tempDir, "opt")
		binDir := filepath.Join(tempDir, "bin")

		if err := os.MkdirAll(optDir, 0755); err != nil {
			t.Logf("MkdirAll optDir error: %v", err)
			return false
		}
		if err := os.MkdirAll(binDir, 0755); err != nil {
			t.Logf("MkdirAll binDir error: %v", err)
			return false
		}

		// Create opt folder for the short-named app (the one being uninstalled)
		appOptFolder := filepath.Join(optDir, baseName)
		if err := os.MkdirAll(appOptFolder, 0755); err != nil {
			t.Logf("MkdirAll appOptFolder error: %v", err)
			return false
		}

		// Create opt folder for the longer-named app (should NOT be affected)
		longerAppOptFolder := filepath.Join(optDir, longerName)
		if err := os.MkdirAll(longerAppOptFolder, 0755); err != nil {
			t.Logf("MkdirAll longerAppOptFolder error: %v", err)
			return false
		}

		// Create a binary inside the longer-named app's opt folder
		binaryPath := filepath.Join(longerAppOptFolder, "binary")
		if err := os.WriteFile(binaryPath, []byte("#!/bin/bash\necho hello\n"), 0755); err != nil {
			t.Logf("WriteFile binary error: %v", err)
			return false
		}

		// Create a symlink in binDir pointing to the longer-named app's binary
		symlinkPath := filepath.Join(binDir, longerName+"-bin")
		if err := os.Symlink(binaryPath, symlinkPath); err != nil {
			t.Logf("Symlink error: %v", err)
			return false
		}

		// Set up registry with the short-named app registered
		registryPathOverride = filepath.Join(tempDir, "registry.json")
		defer func() { registryPathOverride = "" }()

		reg := &Registry{
			Apps: map[string]AppMetadata{
				baseName: {
					Name:        baseName,
					Version:     "1.0.0",
					BinaryPath:  filepath.Join(appOptFolder, "somebin"),
					SymlinkPath: filepath.Join(binDir, baseName),
				},
			},
		}
		if err := SaveRegistry(reg); err != nil {
			t.Logf("SaveRegistry error: %v", err)
			return false
		}

		// Create config pointing to our temp dirs
		config := &Config{
			OptDir:      optDir,
			BinDir:      binDir,
			AppsDir:     filepath.Join(tempDir, "apps"),
			IconsDir:    filepath.Join(tempDir, "icons"),
			AutoConfirm: true,
		}

		// Run UninstallApp for the short-named app
		err := UninstallApp(baseName, config, reg)
		if err != nil {
			t.Logf("UninstallApp error: %v", err)
			return false
		}

		// Assert: the symlink belonging to the longer-named app should still exist
		if _, err := os.Lstat(symlinkPath); os.IsNotExist(err) {
			t.Logf("COUNTEREXAMPLE: Uninstalling %q incorrectly deleted symlink %q which points to %q (belongs to app %q)",
				baseName, symlinkPath, binaryPath, longerName)
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Bug confirmed - prefix-colliding symlinks are incorrectly deleted: %v", err)
	}
}
