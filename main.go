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

func handleHelp() {
	PrintManual()
}

func handleVersion() {
	fmt.Printf("plop v0.1.0\n")
}

func handleInstall(args []string) {
	options := InstallOptions{}
	archivePath := ""
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if arg == "--name" || arg == "-n" {
			if i+1 < len(args) {
				options.ForcedName = args[i+1]
				i++
			}
		} else if arg == "--version" || arg == "-v" || arg == "--ver" {
			if i+1 < len(args) {
				options.ForcedVersion = args[i+1]
				i++
			}
		} else if arg == "--bin" || arg == "-b" || arg == "--binary" {
			if i+1 < len(args) {
				options.ForcedBinaryName = args[i+1]
				i++
			}
		} else if arg == "--source" || arg == "-s" {
			if i+1 < len(args) {
				options.ForcedSource = args[i+1]
				i++
			}
		} else if arg == "--install-script" || arg == "-i" {
			if i+1 < len(args) {
				options.ForcedInstallScript = args[i+1]
				i++
			}
		} else if !strings.HasPrefix(arg, "-") {
			if archivePath == "" {
				archivePath = arg
			}
		}
	}

	if archivePath == "" {
		PrintError("Missing argument: plop install <archive-path-or-url>")
		os.Exit(2)
	}
	err := InstallApp(archivePath, options)
	if err != nil {
		PrintError("Installation failed: %v", err)
		os.Exit(1)
	}
}

func main() {
	// Parse CLI arguments
	args := os.Args[1:]

	if len(args) == 0 {
		PrintManual()
		return
	}

	command := args[0]

	// Command Routing
	switch command {
	case "help", "--help", "-h":
		handleHelp()
	case "version", "--version", "-v":
		handleVersion()
	case "install":
		handleInstall(args)
	}
}
