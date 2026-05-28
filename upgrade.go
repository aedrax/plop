package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// UpgradeApp upgrades a single installed application to its latest GitHub release
func UpgradeApp(appName string, config *Config, reg *Registry) error {
	appName = strings.ToLower(strings.TrimSpace(appName))
	app, exists := reg.Apps[appName]
	if !exists {
		return fmt.Errorf("application '%s' is not registered", appName)
	}

	if app.UpdateURL == "" {
		return fmt.Errorf("no update URL is registered for '%s'. Please reinstall using plop and provide a GitHub repository", appName)
	}

	PrintInfo("Querying GitHub for '%s' updates...", appName)
	release, err := FetchLatestRelease(app.UpdateURL, config.GithubToken)
	if err != nil {
		return err
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	if latestVersion == app.Version {
		PrintInfo("Application '%s' is already at the latest version (%s).", appName, app.Version)
		if !config.AutoConfirm {
			ans := PromptUser("Reinstall anyway? [y/N]: ", "n")
			if strings.ToLower(ans) != "y" {
				return nil
			}
		}
	} else {
		PrintInfo("New version available for '%s': %s (Installed: %s)", appName, latestVersion, app.Version)
	}

	downloadURL, assetName := FindBestAsset(release)
	if downloadURL == "" {
		return fmt.Errorf("could not find a compatible binary asset in the latest release")
	}

	PrintInfo("Found release asset: %s", assetName)

	// Temporarily enable auto-confirm during upgrade to overwrite the old directory
	oldConfirm := config.AutoConfirm
	config.AutoConfirm = true
	defer func() { config.AutoConfirm = oldConfirm }()

	binaryName := ""
	if app.SymlinkPath != "" {
		binaryName = filepath.Base(app.SymlinkPath)
	}

	// Execute standard installation flow using remote URL
	return InstallApp(downloadURL, InstallOptions{
		ForcedName:          appName,
		ForcedVersion:       latestVersion,
		ForcedBinaryName:    binaryName,
		ForcedInstallScript: app.InstallScript,
		ForcedSource:        app.UpdateURL,
	}, config, reg)
}

// UpgradeAll upgrades all installed applications that have available updates
type UpgradeTarget struct {
	AppName     string
	DownloadURL string
	Version     string
}

// UpgradeAll checks all registered applications for updates and offers to upgrade them in sequence
func UpgradeAll(config *Config, reg *Registry) error {
	if len(reg.Apps) == 0 {
		PrintInfo("No applications currently installed.")
		return nil
	}

	PrintInfo("Checking all packages for updates...")
	var targets []UpgradeTarget

	for name, app := range reg.Apps {
		if app.UpdateURL == "" {
			continue
		}

		release, err := FetchLatestRelease(app.UpdateURL, config.GithubToken)
		if err != nil {
			PrintWarning("Skipping '%s': failed to check for updates: %v", name, err)
			continue
		}

		latestVersion := strings.TrimPrefix(release.TagName, "v")
		if latestVersion != app.Version {
			downloadURL, _ := FindBestAsset(release)
			if downloadURL != "" {
				targets = append(targets, UpgradeTarget{
					AppName:     name,
					DownloadURL: downloadURL,
					Version:     latestVersion,
				})
			}
		}
	}

	if len(targets) == 0 {
		PrintSuccess("All applications are fully up to date! Nothing to upgrade.")
		return nil
	}

	fmt.Println("\n" + color(ColorBold, "The following applications will be upgraded:"))
	for _, target := range targets {
		current := reg.Apps[target.AppName].Version
		fmt.Printf("  - %-15s Current: %-10s -> Latest: "+color(ColorGreen, "%-10s")+"\n", target.AppName, current, target.Version)
	}
	fmt.Println()

	if !config.AutoConfirm {
		ans := PromptUser("Would you like to proceed with upgrading all the above? [y/N]: ", "n")
		if strings.ToLower(ans) != "y" {
			PrintWarning("Upgrade aborted.")
			return nil
		}
	}

	// Upgrade targets sequentially
	for _, target := range targets {
		fmt.Printf("\nUpgrading %s to version %s...\n", target.AppName, target.Version)

		// Temporarily enable auto-confirm during upgrades
		oldConfirm := config.AutoConfirm
		config.AutoConfirm = true

		binaryName := ""
		existingApp, exists := reg.Apps[target.AppName]
		if exists && existingApp.SymlinkPath != "" {
			binaryName = filepath.Base(existingApp.SymlinkPath)
		}

		err := InstallApp(target.DownloadURL, InstallOptions{
			ForcedName:          target.AppName,
			ForcedVersion:       target.Version,
			ForcedBinaryName:    binaryName,
			ForcedInstallScript: existingApp.InstallScript,
			ForcedSource:        existingApp.UpdateURL,
		}, config, reg)
		config.AutoConfirm = oldConfirm

		if err != nil {
			PrintError("Failed to upgrade %s: %v", target.AppName, err)
		}
	}

	return nil
}
