package main

import (
	"bytes"
	"debug/elf"
	"debug/macho"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ANSI terminal colors
const (
	ColorReset  = "\033[0m"
	ColorBold   = "\033[1m"
	ColorRed    = "\033[31m"
	ColorGreen  = "\033[32m"
	ColorYellow = "\033[33m"
	ColorBlue   = "\033[34m"
	ColorCyan   = "\033[36m"
	ColorGray   = "\033[90m"
)

var colorsEnabled = true

// Any initialization logic for the package can go here
func init() {
	InitColors()
}

// InitColors initializes the colorsEnabled state based on standard environment checks
func InitColors() {
	noColorEnv := os.Getenv("NO_COLOR") != ""
	cliColorForce := os.Getenv("CLICOLOR_FORCE") != ""

	// Check if stdout is a character device (TTY)
	stdoutIsTTY := true
	if fi, err := os.Stdout.Stat(); err == nil {
		stdoutIsTTY = (fi.Mode() & os.ModeCharDevice) != 0
	}

	// Disable colors if NO_COLOR is set or if output is piped/redirected (and not forced)
	if noColorEnv || (!stdoutIsTTY && !cliColorForce) {
		colorsEnabled = false
	} else {
		colorsEnabled = true
	}
}

// color formats text with the given ANSI style prefix and appends ColorReset,
// unless colors are disabled, in which case it returns the text as-is.
func color(style, text string) string {
	if !colorsEnabled || text == "" {
		return text
	}
	return style + text + ColorReset
}

// PrintSuccess prints a formatted success message
func PrintSuccess(format string, a ...interface{}) {
	fmt.Printf(color(ColorGreen, format)+"\n", a...)
}

// PrintInfo prints a formatted info message
func PrintInfo(format string, a ...interface{}) {
	fmt.Printf(color(ColorCyan, format)+"\n", a...)
}

// PrintWarning prints a formatted warning message
func PrintWarning(format string, a ...interface{}) {
	fmt.Printf(color(ColorYellow, format)+"\n", a...)
}

// PrintError prints a formatted error message
func PrintError(format string, a ...interface{}) {
	fmt.Printf(color(ColorRed+ColorBold, format)+"\n", a...)
}

// ExtractArchive extracts tarballs or zips to a target directory using system commands
func ExtractArchive(srcPath, destDir string) error {
	err := os.MkdirAll(destDir, 0755)
	if err != nil {
		return err
	}

	// Handle .dmg files before tar/zip
	if strings.HasSuffix(strings.ToLower(srcPath), ".dmg") {
		if IsDarwin() {
			warning, err := ExtractDMG(srcPath, destDir)
			if err != nil {
				return err
			}
			if warning != "" {
				PrintWarning(warning)
			}
			return nil
		}
		return fmt.Errorf("unsupported archive format: .dmg files are not supported on this platform")
	}

	ext := strings.ToLower(srcPath)
	var cmd *exec.Cmd

	if strings.HasSuffix(ext, ".zip") {
		// unzip -q -o <archive> -d <destDir>
		cmd = exec.Command("unzip", "-q", "-o", srcPath, "-d", destDir)
	} else if strings.HasSuffix(ext, ".tar") ||
		strings.HasSuffix(ext, ".tar.gz") || strings.HasSuffix(ext, ".tgz") ||
		strings.HasSuffix(ext, ".tar.xz") || strings.HasSuffix(ext, ".txz") ||
		strings.HasSuffix(ext, ".tar.bz2") || strings.HasSuffix(ext, ".tbz2") {
		// tar -xf <archive> -C <destDir>
		cmd = exec.Command("tar", "-xf", srcPath, "-C", destDir)
	} else {
		return fmt.Errorf("unsupported archive format: %s", filepath.Base(srcPath))
	}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("extraction failed: %v, stderr: %s", err, stderr.String())
	}
	return nil
}

// DownloadFile downloads a remote file to a destination path, reporting progress
func DownloadFile(urlStr, destPath string) error {
	// Create client
	client := &http.Client{}
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return err
	}

	// Set user agent so APIs/sites don't block us
	req.Header.Set("User-Agent", "plop-installer/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download: HTTP Status %d", resp.StatusCode)
	}

	// Make sure destination directory exists
	err = os.MkdirAll(filepath.Dir(destPath), 0755)
	if err != nil {
		return err
	}

	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer out.Close()

	// Print loading state
	fmt.Printf("Downloading: %s\n", filepath.Base(destPath))

	// Stream file
	_, err = io.Copy(out, resp.Body)
	return err
}

// IsELF checks if a file is a valid Linux ELF executable
func IsELF(path string) (bool, error) {
	f, err := elf.Open(path)
	if err != nil {
		return false, nil // standard elf.Open returns error for non-ELFs
	}
	f.Close()
	return true, nil
}

// IsMachO checks if a file is a valid macOS Mach-O executable (32-bit, 64-bit, or universal/fat binary)
func IsMachO(path string) (bool, error) {
	// Try opening as a fat (universal) binary first, this handles the 0xCAFEBABE magic
	// which could also be a Java .class file, so OpenFat validates the fat header structure
	fatFile, err := macho.OpenFat(path)
	if err == nil {
		fatFile.Close()
		return true, nil
	}

	// Fall back to standard Mach-O open (handles 0xFEEDFACE 32-bit and 0xFEEDFACF 64-bit)
	f, err := macho.Open(path)
	if err == nil {
		f.Close()
		return true, nil
	}

	// Not a Mach-O file, return false with no error (matches IsELF pattern)
	return false, nil
}

// IsScript checks if a file begins with a shebang launcher sequence
func IsScript(path string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()

	buf := make([]byte, 2)
	n, err := file.Read(buf)
	if err != nil || n < 2 {
		return false, nil
	}

	return string(buf) == "#!", nil
}

// IsExecutable checks if a file is a platform-appropriate executable binary.
// On macOS: checks Mach-O format. On Linux: checks ELF format.
// On both: checks for shebang scripts.
// Returns (isExecutable bool, isNativeBinary bool, err error)
func IsExecutable(path string) (bool, bool, error) {
	switch runtime.GOOS {
	case "darwin":
		ok, err := IsMachO(path)
		if err != nil {
			return false, false, err
		}
		if ok {
			return true, true, nil
		}
	case "linux":
		ok, err := IsELF(path)
		if err != nil {
			return false, false, err
		}
		if ok {
			return true, true, nil
		}
	}

	// On both platforms, check for shebang scripts
	ok, err := IsScript(path)
	if err != nil {
		return false, false, err
	}
	if ok {
		return true, false, nil
	}

	return false, false, nil
}

// ExtractDMG mounts a .dmg file, copies non-hidden contents to destDir, then detaches.
// Returns an error if mounting fails. Returns a warning string if detach fails.
func ExtractDMG(srcPath, destDir string) (warning string, err error) {
	// Create a temporary mount point
	tmpMount, err := os.MkdirTemp("", "plop-dmg-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp mount point: %v", err)
	}
	defer os.RemoveAll(tmpMount)

	// Mount the DMG
	attachCmd := exec.Command("hdiutil", "attach", "-nobrowse", "-noverify", "-mountpoint", tmpMount, srcPath)
	var attachStdout, attachStderr bytes.Buffer
	attachCmd.Stdout = &attachStdout
	attachCmd.Stderr = &attachStderr
	if err := attachCmd.Run(); err != nil {
		return "", fmt.Errorf("failed to mount DMG %s: %v, stderr: %s", srcPath, err, attachStderr.String())
	}

	// Parse the mount point from stdout, look for /Volumes/ path
	mountPoint := ""
	for _, line := range strings.Split(attachStdout.String(), "\n") {
		if idx := strings.Index(line, "/Volumes/"); idx >= 0 {
			mountPoint = strings.TrimSpace(line[idx:])
			break
		}
	}
	// If no /Volumes/ path found, use the tmpMount we specified
	if mountPoint == "" {
		mountPoint = tmpMount
	}

	// Ensure destDir exists
	if err := os.MkdirAll(destDir, 0755); err != nil {
		// Attempt to detach before returning
		exec.Command("hdiutil", "detach", mountPoint).Run()
		return "", fmt.Errorf("failed to create destination directory: %v", err)
	}

	// Copy all non-hidden files/directories from the volume root to destDir
	entries, err := os.ReadDir(mountPoint)
	if err != nil {
		exec.Command("hdiutil", "detach", mountPoint).Run()
		return "", fmt.Errorf("failed to read mounted volume: %v", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		// Skip hidden files (names starting with '.')
		if strings.HasPrefix(name, ".") {
			continue
		}

		srcEntry := filepath.Join(mountPoint, name)
		dstEntry := filepath.Join(destDir, name)

		if entry.IsDir() {
			if err := copyDir(srcEntry, dstEntry); err != nil {
				exec.Command("hdiutil", "detach", mountPoint).Run()
				return "", fmt.Errorf("failed to copy directory %s: %v", name, err)
			}
		} else {
			if err := CopyFile(srcEntry, dstEntry); err != nil {
				exec.Command("hdiutil", "detach", mountPoint).Run()
				return "", fmt.Errorf("failed to copy file %s: %v", name, err)
			}
			// Preserve executable permissions
			if info, err := os.Stat(srcEntry); err == nil {
				os.Chmod(dstEntry, info.Mode())
			}
		}
	}

	// Detach the mounted volume
	detachCmd := exec.Command("hdiutil", "detach", mountPoint)
	var detachStderr bytes.Buffer
	detachCmd.Stderr = &detachStderr
	if err := detachCmd.Run(); err != nil {
		return fmt.Sprintf("warning: failed to detach volume %s: %v", mountPoint, err), nil
	}

	return "", nil
}

// copyDir recursively copies a directory from src to dst
func copyDir(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else if entry.Type()&os.ModeSymlink != 0 {
			// Handle symlinks
			link, err := os.Readlink(srcPath)
			if err != nil {
				return err
			}
			if err := os.Symlink(link, dstPath); err != nil {
				return err
			}
		} else {
			if err := CopyFile(srcPath, dstPath); err != nil {
				return err
			}
			// Preserve file permissions
			if info, err := os.Stat(srcPath); err == nil {
				os.Chmod(dstPath, info.Mode())
			}
		}
	}

	return nil
}

// FileExists checks if a path points to an existing file/directory
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}

// CopyFile copies a file from source to destination by streaming the bytes
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return out.Sync()
}
