package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// UninstallApp removes all local files and path registration for an application
func UninstallApp(appName string, config *Config, reg *Registry) error {
	appName = strings.ToLower(strings.TrimSpace(appName))
	app, exists := reg.Apps[appName]
	if !exists {
		return fmt.Errorf("application '%s' is not registered in plop", appName)
	}

	PrintInfo("Uninstalling application '%s'...", appName)

	// Confirm uninstallation with user (unless auto-confirm is enabled)
	if !config.AutoConfirm {
		ans := PromptUser(fmt.Sprintf("Are you sure you want to completely uninstall '%s'? [y/N]: ", appName), "n")
		if strings.ToLower(ans) != "y" {
			PrintWarning("Uninstallation aborted.")
			return nil
		}
	}

	// Remove opt installation folder
	optDir := ExpandTilde(config.OptDir)
	appOptFolder := filepath.Join(optDir, appName)
	if FileExists(appOptFolder) {
		PrintInfo("Removing opt installation files...")
		err := os.RemoveAll(appOptFolder)
		if err != nil {
			PrintWarning("Could not delete opt directory '%s': %v", appOptFolder, err)
		}
	}

	// Remove PATH symlinks (including any broken/orphaned ones pointing to the opt folder)
	binDir := ExpandTilde(config.BinDir)
	binFiles, err := os.ReadDir(binDir)
	if err == nil {
		for _, file := range binFiles {
			filePath := filepath.Join(binDir, file.Name())
			// Read the symlink destination (if it is a symlink)
			target, err := os.Readlink(filePath)
			if err == nil {
				// Resolve target to absolute path
				absTarget := target
				if !filepath.IsAbs(target) {
					absTarget = filepath.Join(binDir, target)
				}
				absTarget = filepath.Clean(absTarget)

				// If it points inside our app's opt folder, delete it!
				if strings.HasPrefix(absTarget, appOptFolder) {
					PrintInfo("Removing executable symlink '%s'...", file.Name())
					err := os.Remove(filePath)
					if err != nil {
						PrintWarning("Could not delete symlink '%s': %v", filePath, err)
					}
				}
			}
		}
	}

	// Explicitly remove the primary symlink path from registry if it exists and wasn't inside opt
	if app.SymlinkPath != "" {
		if _, lerr := os.Lstat(app.SymlinkPath); lerr == nil {
			err := os.Remove(app.SymlinkPath)
			if err != nil && !os.IsNotExist(err) {
				PrintWarning("Could not delete symlink '%s': %v", app.SymlinkPath, err)
			}
		}
	}

	// Remove desktop launcher
	if app.DesktopPath != "" && FileExists(app.DesktopPath) {
		PrintInfo("Removing desktop launcher entry...")
		err := os.Remove(app.DesktopPath)
		if err != nil {
			PrintWarning("Could not delete desktop file '%s': %v", app.DesktopPath, err)
		}
	}

	// Remove custom icon
	// Only delete the icon if it points to a local custom file in our icons directory
	iconsDir := ExpandTilde(config.IconsDir)
	if app.IconPath != "" && strings.HasPrefix(app.IconPath, iconsDir) && FileExists(app.IconPath) {
		PrintInfo("Removing custom icon file...")
		err := os.Remove(app.IconPath)
		if err != nil {
			PrintWarning("Could not delete icon file '%s': %v", app.IconPath, err)
		}
	}

	// Unregister from registry
	delete(reg.Apps, appName)
	err = SaveRegistry(reg)
	if err != nil {
		return fmt.Errorf("failed to save registry metadata: %v", err)
	}

	PrintSuccess("Successfully uninstalled '%s' and updated registry!", appName)
	return nil
}
