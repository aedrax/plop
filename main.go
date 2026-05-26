package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// PrintManual displays a help menu for plop
func PrintManual() {
	configPath, _ := GetConfigPath()
	registryPath, _ := GetRegistryPath()

	// Default paths
	optDir := "~/.local/opt"
	binDir := "~/.local/bin"
	if val := os.Getenv("XDG_BIN_HOME"); val != "" {
		binDir = val
	}

	// Try loading actual active configuration for dynamic directory mapping
	cfg, err := LoadConfig()
	if err == nil && cfg != nil {
		optDir = cfg.OptDir
		binDir = cfg.BinDir
	}

	// Clean paths using tilde notations where possible
	home, err := os.UserHomeDir()
	if err == nil {
		if strings.HasPrefix(configPath, home) {
			configPath = "~" + strings.TrimPrefix(configPath, home)
		}
		if strings.HasPrefix(registryPath, home) {
			registryPath = "~" + strings.TrimPrefix(registryPath, home)
		}
		if strings.HasPrefix(optDir, home) {
			optDir = "~" + strings.TrimPrefix(optDir, home)
		}
		if strings.HasPrefix(binDir, home) {
			binDir = "~" + strings.TrimPrefix(binDir, home)
		}
	}

	fmt.Println(color(ColorBold+ColorCyan, "plop (Pull, Link, Organize, Place): Local Archive Installer"))
	fmt.Println(color(ColorGray, "=========================================================================="))
	fmt.Println("Automatically pulls archives, links binaries, organizes desktop files,")
	fmt.Println("and places them neatly in your local user space sandbox.")
	fmt.Println()
	fmt.Println(color(ColorBold, "Usage:"))
	fmt.Printf("  plop <command> [arguments]\n")
	fmt.Printf("  plop <archive-path-or-url>  (Implicit install)\n")
	fmt.Println()
	fmt.Println(color(ColorBold, "Commands:"))
	fmt.Printf("  %-25s %s\n", color(ColorGreen, "install <path-or-url>"), "Installs an application from a local archive or remote URL")
	fmt.Printf("                            %-10s %s\n", "-n, --name", "Explicitly force the application name")
	fmt.Printf("                            %-10s %s\n", "-v, --version", "Explicitly force the application version")
	fmt.Printf("                            %-10s %s\n", "-b, --bin", "Explicitly force/specify the binary symlink name")
	fmt.Printf("                            %-10s %s\n", "-s, --source", "Explicitly force/associate the upgrade source URL")
	fmt.Printf("                            %-10s %s\n", "-i, --install-script", "Use a custom installation script/executable")
	fmt.Printf("  %-25s %s\n", color(ColorGreen, "list"), "Lists all applications currently managed by plop")
	fmt.Printf("  %-25s %s\n", color(ColorGreen, "update"), "Checks for new releases of all registered applications")
	fmt.Printf("  %-25s %s\n", color(ColorGreen, "upgrade [app]"), "Upgrades a specific application (or all applications if blank) to the latest version")
	fmt.Printf("  %-25s %s\n", color(ColorGreen, "uninstall <app>"), "Completely uninstalls an application, removing binaries, symlinks, and launchers")
	fmt.Printf("  %-25s %s\n", color(ColorGreen, "set <app>"), "Modifies application name, version, or update source URL after installation")
	fmt.Printf("                            %-10s %s\n", "-n, --name", "Change the application name (renames directories & files)")
	fmt.Printf("                            %-10s %s\n", "-v, --version", "Change the application version metadata")
	fmt.Printf("                            %-10s %s\n", "-s, --source", "Change the application update source URL")
	fmt.Printf("                            %-10s %s\n", "-i, --install-script", "Change/set/clear the custom install script path")
	fmt.Printf("  %-25s %s\n", color(ColorGreen, "version"), "Prints the version of the plop package manager (-v, --version)")
	fmt.Printf("  %-25s %s\n", color(ColorGreen, "help"), "Displays this command help manual")
	fmt.Println()
	fmt.Println(color(ColorBold, "Global Directories & Registry (XDG Compliant):"))
	fmt.Printf("  - Config path:   %s\n", configPath)
	fmt.Printf("  - Registry path: %s\n", registryPath)
	fmt.Printf("  - Bin Directory: %s\n", binDir)
	fmt.Printf("  - Opt Sandbox:   %s/<app>\n", optDir)
	fmt.Println(color(ColorGray, "--------------------------------------------------------------------------"))
	fmt.Println()
}

// RunListApps displays a clean, visual summary table of all installed packages sorted by name
func RunListApps(config *Config, reg *Registry) error {
	if len(reg.Apps) == 0 {
		PrintInfo("No applications currently installed by plop.")
		return nil
	}

	fmt.Println("\n" + color(ColorBold, "Installed Applications:"))
	fmt.Printf("  "+color(ColorCyan, "%-15s %-10s %-20s %s")+"\n", "NAME", "VERSION", "INSTALLED AT", "UPDATE SOURCE")
	fmt.Printf("  %-15s %-10s %-20s %s\n", "----", "-------", "------------", "-------------")

	// Collect keys and sort alphabetically
	var keys []string
	for name := range reg.Apps {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	for _, name := range keys {
		app := reg.Apps[name]
		updateSource := app.UpdateURL
		if updateSource == "" {
			updateSource = color(ColorGray, "[None]")
		}
		fmt.Printf("  %-15s %-10s %-20s %s\n", app.Name, app.Version, app.InstalledAt, updateSource)
	}
	fmt.Println()
	return nil
}

func handleHelp() {
	PrintManual()
}

func handleVersion() {
	fmt.Printf("plop v1.1.0\n")
}

func handleList(args []string, config *Config, reg *Registry) {
	asJSON := false
	for _, arg := range args[1:] {
		if arg == "--json" || arg == "-j" {
			asJSON = true
		}
	}

	if asJSON {
		data, err := json.MarshalIndent(reg.Apps, "", "  ")
		if err != nil {
			PrintError("Failed to format list as JSON: %v", err)
			os.Exit(1)
		}
		fmt.Println(string(data))
	} else {
		err := RunListApps(config, reg)
		if err != nil {
			PrintError("Failed to list applications: %v", err)
			os.Exit(1)
		}
	}
}

func handleUpdate(config *Config, reg *Registry) {
	err := RunUpdateCheck(config, reg)
	if err != nil {
		PrintError("Update check failed: %v", err)
		os.Exit(1)
	}
}

func handleInstall(args []string, config *Config, reg *Registry) {
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
	err := InstallApp(archivePath, options, config, reg)
	if err != nil {
		PrintError("Installation failed: %v", err)
		os.Exit(1)
	}
}

func handleUpgrade(args []string, config *Config, reg *Registry) {
	var err error
	if len(args) >= 2 {
		err = UpgradeApp(args[1], config, reg)
	} else {
		err = UpgradeAll(config, reg)
	}
	if err != nil {
		PrintError("Upgrade failed: %v", err)
		os.Exit(1)
	}
}

func handleUninstall(args []string, config *Config, reg *Registry) {
	if len(args) < 2 {
		PrintError("Missing argument: plop uninstall <app-name>")
		os.Exit(2)
	}
	err := UninstallApp(args[1], config, reg)
	if err != nil {
		PrintError("Uninstallation failed: %v", err)
		os.Exit(1)
	}
}

// handleSet allows modifying app metadata like name, version, or update source after installation
func handleSet(args []string, config *Config, reg *Registry) {
	if len(args) < 2 {
		PrintError("Missing argument: plop set <app-name>")
		os.Exit(2)
	}
	appName := args[1]
	options := SetAppOptions{}
	for i := 2; i < len(args); i++ {
		arg := args[i]
		if arg == "--name" || arg == "-n" {
			if i+1 < len(args) {
				options.NewName = args[i+1]
				i++
			}
		} else if arg == "--version" || arg == "-v" || arg == "--ver" {
			if i+1 < len(args) {
				options.NewVersion = args[i+1]
				i++
			}
		} else if arg == "--source" || arg == "-s" {
			if i+1 < len(args) {
				options.NewSource = args[i+1]
				i++
			}
		} else if arg == "--install-script" || arg == "-i" {
			if i+1 < len(args) {
				options.NewInstallScript = args[i+1]
				i++
			}
		}
	}

	err := SetAppMetadata(appName, options, config, reg)
	if err != nil {
		PrintError("Failed to set application metadata: %v", err)
		os.Exit(1)
	}
}

func handleImplicitInstall(args []string, config *Config, reg *Registry, command string) {
	isURL := strings.HasPrefix(command, "http://") || strings.HasPrefix(command, "https://")
	ext := strings.ToLower(filepath.Ext(command))
	isArchive := ext == ".zip" || ext == ".gz" || ext == ".xz" || ext == ".bz2" || ext == ".tgz" || ext == ".txz" || ext == ".tbz2" || ext == ".tar" || ext == ".appimage"

	if isURL || isArchive || FileExists(ExpandTilde(command)) {
		options := InstallOptions{}
		archivePath := command
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
			}
		}
		err := InstallApp(archivePath, options, config, reg)
		if err != nil {
			PrintError("Implicit installation failed: %v", err)
			os.Exit(1)
		}
	} else {
		PrintError("Unknown command or unrecognized archive: '%s'", command)
		fmt.Println("Run 'plop help' to see a list of valid commands.")
		os.Exit(2)
	}
}

func main() {
	// Parse CLI arguments
	args := os.Args[1:]

	if len(args) == 0 {
		PrintManual()
		return
	}

	// Load Configurations & Registries
	config, err := LoadConfig()
	if err != nil {
		PrintError("Failed to load configuration: %v", err)
		os.Exit(1)
	}

	reg, err := LoadRegistry()
	if err != nil {
		PrintError("Failed to load application registry: %v", err)
		os.Exit(1)
	}

	command := args[0]

	// Command Routing
	switch command {
	case "help", "--help", "-h":
		handleHelp()
	case "version", "--version", "-v":
		handleVersion()
	case "list":
		handleList(args, config, reg)
	case "update":
		handleUpdate(config, reg)
	case "install":
		handleInstall(args, config, reg)
	case "upgrade":
		handleUpgrade(args, config, reg)
	case "uninstall":
		handleUninstall(args, config, reg)
	case "set", "edit":
		handleSet(args, config, reg)
	default:
		handleImplicitInstall(args, config, reg, command)
	}
}
