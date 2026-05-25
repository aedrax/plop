package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SetAppOptions defines fields that can be modified
type SetAppOptions struct {
	NewName          string
	NewVersion       string
	NewSource        string
	NewInstallScript string
}

// SetAppMetadata modifies the metadata and files of an installed application
func SetAppMetadata(appName string, options SetAppOptions, config *Config, reg *Registry) error {
	appName = strings.ToLower(strings.TrimSpace(appName))
	app, exists := reg.Apps[appName]
	if !exists {
		return fmt.Errorf("application '%s' is not registered", appName)
	}

	// Interactive prompts if no flags are provided
	if options.NewName == "" && options.NewVersion == "" && options.NewSource == "" && options.NewInstallScript == "" {
		fmt.Println("\n" + color(ColorBold, "Modify Application Metadata:"))
		fmt.Printf("Leave blank and press Enter to keep current values.\n\n")

		nameAns := PromptUser(fmt.Sprintf("New Name [%s]: ", app.Name), app.Name)
		options.NewName = strings.TrimSpace(nameAns)

		versionAns := PromptUser(fmt.Sprintf("New Version [%s]: ", app.Version), app.Version)
		options.NewVersion = strings.TrimSpace(versionAns)

		sourceAns := PromptUser(fmt.Sprintf("New Update Source [%s]: ", app.UpdateURL), app.UpdateURL)
		options.NewSource = strings.TrimSpace(sourceAns)

		scriptAns := PromptUser(fmt.Sprintf("New Custom Install Script [%s]: ", app.InstallScript), app.InstallScript)
		options.NewInstallScript = strings.TrimSpace(scriptAns)
	}

	hasChanges := false

	// Apply version change
	if options.NewVersion != "" && options.NewVersion != app.Version {
		PrintInfo("Updating version: %s -> %s", app.Version, options.NewVersion)
		app.Version = options.NewVersion
		hasChanges = true
	}

	// Apply source change (only if specified, supports "none" to clear)
	if options.NewSource != "" {
		newSource := options.NewSource
		if strings.ToLower(newSource) == "none" {
			newSource = ""
		}
		if newSource != app.UpdateURL {
			PrintInfo("Updating update source: %s -> %s", app.UpdateURL, newSource)
			app.UpdateURL = newSource
			hasChanges = true
		}
	}

	// Apply custom install script change (only if specified, supports "none" to clear)
	if options.NewInstallScript != "" {
		newScript := options.NewInstallScript
		if strings.ToLower(newScript) == "none" {
			newScript = ""
		}
		if newScript != app.InstallScript {
			PrintInfo("Updating custom install script: %s -> %s", app.InstallScript, newScript)
			app.InstallScript = newScript
			hasChanges = true
		}
	}

	// Apply name change (requires renaming directories, symlinks, desktop files)
	if options.NewName != "" && strings.ToLower(options.NewName) != appName {
		newNameSanitized := strings.ToLower(strings.ReplaceAll(options.NewName, " ", "-"))

		// Check for name collisions in registry
		if _, collision := reg.Apps[newNameSanitized]; collision {
			return fmt.Errorf("an application named '%s' is already registered", newNameSanitized)
		}

		PrintInfo("Renaming application and files: '%s' -> '%s'...", appName, newNameSanitized)

		// A. Rename opt folder
		optDir := ExpandTilde(config.OptDir)
		oldOptPath := filepath.Join(optDir, appName)
		newOptPath := filepath.Join(optDir, newNameSanitized)

		if FileExists(oldOptPath) {
			err := os.Rename(oldOptPath, newOptPath)
			if err != nil {
				return fmt.Errorf("failed to rename installation folder: %v", err)
			}
			PrintInfo("Renamed folder to: %s", newOptPath)
		}

		// Update BinaryPath inside the newly moved folder
		if strings.HasPrefix(app.BinaryPath, oldOptPath) {
			relBin, _ := filepath.Rel(oldOptPath, app.BinaryPath)
			app.BinaryPath = filepath.Join(newOptPath, relBin)
		}

		// B. Recreate or Rename Symlinks
		binDir := ExpandTilde(config.BinDir)
		oldSymlinkPath := app.SymlinkPath
		newSymlinkPath := filepath.Join(binDir, newNameSanitized)

		if oldSymlinkPath != "" {
			// os.Remove deletes the symlink itself (even if it is a broken symlink).
			err := os.Remove(oldSymlinkPath)
			if err != nil && !os.IsNotExist(err) {
				PrintWarning("Could not remove old symlink: %v", err)
			}
		}

		err := os.Symlink(app.BinaryPath, newSymlinkPath)
		if err != nil {
			return fmt.Errorf("failed to recreate symlink: %v", err)
		}
		app.SymlinkPath = newSymlinkPath
		PrintInfo("Recreated symlink at: %s", newSymlinkPath)

		// Recreate all secondary symlinks to point to the new opt path
		for _, subPath := range app.SelectedBinaries {
			baseName := filepath.Base(subPath)
			if strings.HasSuffix(strings.ToLower(baseName), ".appimage") {
				baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))
			}
			baseName = strings.ReplaceAll(baseName, " ", "-")

			symPath := filepath.Join(binDir, baseName)
			if symPath != newSymlinkPath {
				os.Remove(symPath)
				newTarget := filepath.Join(newOptPath, subPath)
				err = os.Symlink(newTarget, symPath)
				if err != nil {
					PrintWarning("Could not recreate secondary symlink for '%s': %v", baseName, err)
				} else {
					PrintInfo("Recreated secondary symlink at: %s", symPath)
				}
			}
		}

		// C. Rename Icon (if any custom icon exists)
		iconsDir := ExpandTilde(config.IconsDir)
		if app.IconPath != "" && app.IconPath != "application-x-executable" {
			oldIconPath := app.IconPath
			ext := filepath.Ext(oldIconPath)
			newIconPath := filepath.Join(iconsDir, newNameSanitized+ext)

			if FileExists(oldIconPath) {
				err := os.Rename(oldIconPath, newIconPath)
				if err == nil {
					app.IconPath = newIconPath
					PrintInfo("Renamed icon to: %s", newIconPath)
				} else {
					PrintWarning("Could not rename icon file: %v", err)
				}
			}
		}

		// D. Rename Desktop file and update its contents
		appsDir := ExpandTilde(config.AppsDir)
		oldDesktopPath := app.DesktopPath
		newDesktopPath := filepath.Join(appsDir, newNameSanitized+".desktop")

		if FileExists(oldDesktopPath) {
			// Read desktop file contents
			content, err := os.ReadFile(oldDesktopPath)
			if err == nil {
				lines := strings.Split(string(content), "\n")
				for i, line := range lines {
					if strings.HasPrefix(line, "Name=") {
						lines[i] = "Name=" + options.NewName
					} else if strings.HasPrefix(line, "Exec=") {
						lines[i] = "Exec=\"" + newSymlinkPath + "\""
					} else if strings.HasPrefix(line, "Icon=") {
						lines[i] = "Icon=" + app.IconPath
					}
				}
				newContent := strings.Join(lines, "\n")
				err = os.WriteFile(newDesktopPath, []byte(newContent), 0644)
				if err == nil {
					os.Remove(oldDesktopPath)
					app.DesktopPath = newDesktopPath
					PrintInfo("Updated and renamed desktop launcher to: %s", newDesktopPath)
				} else {
					PrintWarning("Could not write new desktop file: %v", err)
				}
			} else {
				PrintWarning("Could not read old desktop file: %v", err)
			}
		}

		// E. Save under new key and delete old key
		app.Name = newNameSanitized
		reg.Apps[newNameSanitized] = app
		delete(reg.Apps, appName)
		hasChanges = true
	} else {
		// Just save updated version or source under existing name
		reg.Apps[appName] = app
	}

	if hasChanges {
		err := SaveRegistry(reg)
		if err != nil {
			return fmt.Errorf("failed to save registry: %v", err)
		}
		PrintSuccess("Successfully updated application metadata!")
	} else {
		PrintInfo("No metadata changes detected.")
	}

	return nil
}
