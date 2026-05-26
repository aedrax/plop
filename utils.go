package main

import (
	"bytes"
	"debug/elf"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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
