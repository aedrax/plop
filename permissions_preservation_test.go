package main

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"testing/quick"
)

// PermTestInput represents a generated test input for extractRawBinary preservation tests.
type PermTestInput struct {
	SourceMode os.FileMode // Random permission mode for the source file
	Content    []byte      // Random non-empty file content
}

// Generate implements quick.Generator for PermTestInput.
func (PermTestInput) Generate(rand *rand.Rand, size int) reflect.Value {
	// Generate a random Unix permission mode (0o000 to 0o777)
	// Ensure at least owner-read so CopyFile can read the source
	mode := os.FileMode(rand.Intn(0o777) + 1)
	// Ensure owner-read bit is set (required for CopyFile to read the file)
	mode = mode | 0o400

	// Generate random non-empty content (1 to 1024 bytes)
	contentLen := rand.Intn(1024) + 1
	content := make([]byte, contentLen)
	for i := range content {
		content[i] = byte(rand.Intn(256))
	}

	return reflect.ValueOf(PermTestInput{
		SourceMode: mode,
		Content:    content,
	})
}

// TestPreservation_TargetFileGets0755 verifies that extractRawBinary always sets
// the target file permissions to 0755, regardless of the source file's permissions.
func TestPreservation_TargetFileGets0755(t *testing.T) {
	f := func(input PermTestInput) bool {
		// Create a temp directory for the source file
		srcDir := t.TempDir()
		srcPath := filepath.Join(srcDir, "testbinary")

		// Write source file with random content and random permissions
		err := os.WriteFile(srcPath, input.Content, input.SourceMode)
		if err != nil {
			t.Logf("WriteFile failed: %v", err)
			return false
		}
		// Explicitly set permissions (WriteFile may be affected by umask)
		err = os.Chmod(srcPath, input.SourceMode)
		if err != nil {
			t.Logf("Chmod source failed: %v", err)
			return false
		}

		// Create a target directory path (extractRawBinary will create it)
		targetDir := filepath.Join(t.TempDir(), "target")

		// Call extractRawBinary
		err = extractRawBinary(srcPath, targetDir)
		if err != nil {
			t.Logf("extractRawBinary failed: %v", err)
			return false
		}

		// Check target file permissions
		targetPath := filepath.Join(targetDir, "testbinary")
		info, err := os.Stat(targetPath)
		if err != nil {
			t.Logf("Stat target failed: %v", err)
			return false
		}

		targetPerm := info.Mode().Perm()
		if targetPerm != 0755 {
			t.Logf("FAILURE: target file has permissions %04o, expected 0755 (source was %04o)",
				targetPerm, input.SourceMode)
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Preservation property failed, target file should always get 0755: %v", err)
	}
}

// TestPreservation_TargetContentMatchesSource verifies that extractRawBinary
// copies the source file content byte-for-byte to the target.
func TestPreservation_TargetContentMatchesSource(t *testing.T) {
	f := func(input PermTestInput) bool {
		// Create a temp directory for the source file
		srcDir := t.TempDir()
		srcPath := filepath.Join(srcDir, "testbinary")

		// Write source file with random content and random permissions
		err := os.WriteFile(srcPath, input.Content, input.SourceMode)
		if err != nil {
			t.Logf("WriteFile failed: %v", err)
			return false
		}
		err = os.Chmod(srcPath, input.SourceMode)
		if err != nil {
			t.Logf("Chmod source failed: %v", err)
			return false
		}

		// Create a target directory path
		targetDir := filepath.Join(t.TempDir(), "target")

		// Call extractRawBinary
		err = extractRawBinary(srcPath, targetDir)
		if err != nil {
			t.Logf("extractRawBinary failed: %v", err)
			return false
		}

		// Read target file content
		targetPath := filepath.Join(targetDir, "testbinary")
		targetContent, err := os.ReadFile(targetPath)
		if err != nil {
			t.Logf("ReadFile target failed: %v", err)
			return false
		}

		// Verify byte-for-byte match
		if !bytes.Equal(input.Content, targetContent) {
			t.Logf("FAILURE: target content does not match source content (source len=%d, target len=%d)",
				len(input.Content), len(targetContent))
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Preservation property failed, target content should match source: %v", err)
	}
}

// TestPreservation_TargetDirectoryCreatedWith0755 verifies that extractRawBinary
// creates the target directory with 0755 permissions when it doesn't exist.
func TestPreservation_TargetDirectoryCreatedWith0755(t *testing.T) {
	f := func(input PermTestInput) bool {
		// Create a temp directory for the source file
		srcDir := t.TempDir()
		srcPath := filepath.Join(srcDir, "testbinary")

		// Write source file with random content and random permissions
		err := os.WriteFile(srcPath, input.Content, input.SourceMode)
		if err != nil {
			t.Logf("WriteFile failed: %v", err)
			return false
		}
		err = os.Chmod(srcPath, input.SourceMode)
		if err != nil {
			t.Logf("Chmod source failed: %v", err)
			return false
		}

		// Create a target directory path that does NOT exist yet
		baseDir := t.TempDir()
		targetDir := filepath.Join(baseDir, "newdir")

		// Call extractRawBinary (should create targetDir)
		err = extractRawBinary(srcPath, targetDir)
		if err != nil {
			t.Logf("extractRawBinary failed: %v", err)
			return false
		}

		// Check target directory permissions
		info, err := os.Stat(targetDir)
		if err != nil {
			t.Logf("Stat target dir failed: %v", err)
			return false
		}

		if !info.IsDir() {
			t.Logf("FAILURE: target path is not a directory")
			return false
		}

		dirPerm := info.Mode().Perm()
		if dirPerm != 0755 {
			t.Logf("FAILURE: target directory has permissions %04o, expected 0755", dirPerm)
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Preservation property failed, target directory should be created with 0755: %v", err)
	}
}

// TestPreservation_ExtractAppImageSourceChmodUnaffected verifies that
// extractAppImage still chmods its source file to 0755. This confirms
// the fix to extractRawBinary does not affect extractAppImage behavior.
func TestPreservation_ExtractAppImageSourceChmodUnaffected(t *testing.T) {
	// This is a code inspection test - verify extractAppImage still contains
	// os.Chmod(archivePath, 0755) by calling it and checking the source gets 0755.
	// We create a fake "AppImage" file (it won't actually extract, but the chmod
	// happens before extraction attempt).

	f := func(input PermTestInput) bool {
		srcDir := t.TempDir()
		srcPath := filepath.Join(srcDir, "test.AppImage")

		// Write a fake AppImage file with random permissions
		err := os.WriteFile(srcPath, input.Content, input.SourceMode)
		if err != nil {
			t.Logf("WriteFile failed: %v", err)
			return false
		}
		err = os.Chmod(srcPath, input.SourceMode)
		if err != nil {
			t.Logf("Chmod source failed: %v", err)
			return false
		}

		targetDir := filepath.Join(t.TempDir(), "appimage-target")

		// Call extractAppImage - it will fail on --appimage-extract since this
		// isn't a real AppImage, but the chmod happens before that attempt
		_, _, _ = extractAppImage(srcPath, "testapp", targetDir)

		// Verify source file was chmod'd to 0755 by extractAppImage
		info, err := os.Stat(srcPath)
		if err != nil {
			t.Logf("Stat source failed: %v", err)
			return false
		}

		srcPerm := info.Mode().Perm()
		if srcPerm != 0755 {
			t.Logf("FAILURE: extractAppImage did not chmod source to 0755 (got %04o)", srcPerm)
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 50}); err != nil {
		t.Errorf("Preservation property failed, extractAppImage should still chmod source to 0755: %v", err)
	}
}
