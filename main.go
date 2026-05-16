package main

import (
	"fmt"
	"os"
	"strings"
)

// PrintManual displays a help menu for plop
func PrintManual() {
	configPath, _ := GetConfigPath()

	// Default paths
	optDir := "~/.local/opt"
	binDir := "~/.local/bin"
	if val := os.Getenv("XDG_BIN_HOME"); val != "" {
		binDir = val
	}

	// Clean paths using tilde notations where possible
	home, err := os.UserHomeDir()
	if err == nil {
		if strings.HasPrefix(configPath, home) {
			configPath = "~" + strings.TrimPrefix(configPath, home)
		}
		if strings.HasPrefix(optDir, home) {
			optDir = "~" + strings.TrimPrefix(optDir, home)
		}
		if strings.HasPrefix(binDir, home) {
			binDir = "~" + strings.TrimPrefix(binDir, home)
		}
	}

	fmt.Println()
	fmt.Println(color(ColorBold+ColorCyan, "plop (Pull, Link, Organize, Place): Local Archive Installer"))
	fmt.Println(color(ColorGray, "=========================================================================="))
	fmt.Println("Automatically pulls archives, links binaries, organizes desktop files,")
	fmt.Println("and places them neatly in your local user space sandbox.")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Printf("  plop <command> [arguments]\n")
	fmt.Printf("  plop <archive-path-or-url>  (Implicit install)\n")
	fmt.Println()
	fmt.Println(color(ColorBold, "Global Directories & Registry (XDG Compliant):"))
	fmt.Printf("  - Config path:   %s\n", configPath)
	fmt.Printf("  - Bin Directory: %s\n", binDir)
	fmt.Printf("  - Opt Sandbox:   %s/<app>\n", optDir)
}

func main() {
	// Parse CLI arguments
	args := os.Args[1:]

	if len(args) == 0 {
		PrintManual()
		return
	}
}
