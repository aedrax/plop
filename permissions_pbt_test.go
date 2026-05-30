package main

import (
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/quick"
)

// PermMode is a custom type for generating valid Unix permission modes
// that are NOT 0755, so we can observe the bug (source gets changed to 0755).
type PermMode struct {
	Mode os.FileMode
}

// Generate implements quick.Generator for PermMode.
// It produces valid Unix permission modes in the range [0o000, 0o777] excluding 0o755.
// Owner-read bit (0o400) is always set so CopyFile can read the source.
func (PermMode) Generate(r *rand.Rand, size int) reflect.Value {
	for {
		mode := os.FileMode(r.Intn(0o777+1)) | 0o400 // ensure owner-read
		if mode != 0o755 {
			return reflect.ValueOf(PermMode{Mode: mode})
		}
	}
}

// TestBugCondition_SourceFilePermissionsModifiedByExtractRawBinary is a property-based
// test that verifies extractRawBinary does NOT modify source file permissions.
//
// EXPECTED OUTCOME on UNFIXED code: This test FAILS because extractRawBinary calls
// os.Chmod(archivePath, 0755) on the source file, changing its permissions.
// Failure confirms the bug exists.
//
// EXPECTED OUTCOME on FIXED code: This test PASSES because the source chmod is removed.
func TestBugCondition_SourceFilePermissionsModifiedByExtractRawBinary(t *testing.T) {
	f := func(pm PermMode) bool {
		// Create a temp source file with the generated permission mode
		sourceDir := t.TempDir()
		sourcePath := filepath.Join(sourceDir, "testbinary")

		err := os.WriteFile(sourcePath, []byte("#!/bin/sh\necho hello\n"), 0o644)
		if err != nil {
			t.Logf("WriteFile error: %v", err)
			return false
		}

		// Set the source file to the random permission mode
		originalMode := pm.Mode
		err = os.Chmod(sourcePath, originalMode)
		if err != nil {
			t.Logf("Chmod error: %v", err)
			return false
		}

		// Verify the permission was set correctly
		info, err := os.Stat(sourcePath)
		if err != nil {
			t.Logf("Stat error before call: %v", err)
			return false
		}
		if info.Mode().Perm() != originalMode {
			t.Logf("Failed to set initial permissions: wanted %04o, got %04o", originalMode, info.Mode().Perm())
			return false
		}

		// Create a temp target directory
		targetDir := t.TempDir()
		targetAppDir := filepath.Join(targetDir, "app")

		// Call extractRawBinary
		err = extractRawBinary(sourcePath, targetAppDir)
		if err != nil {
			t.Logf("extractRawBinary error: %v", err)
			return false
		}

		// Assert: source file permissions after call == original permissions before call
		infoAfter, err := os.Stat(sourcePath)
		if err != nil {
			t.Logf("Stat error after call: %v", err)
			return false
		}

		resultMode := infoAfter.Mode().Perm()
		if resultMode != originalMode {
			t.Logf("COUNTEREXAMPLE: source file with permissions %04o was changed to %04o after extractRawBinary",
				originalMode, resultMode)
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Bug confirmed - extractRawBinary modifies source file permissions: %v", err)
	}
}
