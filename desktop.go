package main

import (
	"fmt"
	"os"
	"strings"
)

// ProcessDesktopFile reads an existing desktop entry, modifies its Exec/Icon fields, and saves it
func ProcessDesktopFile(srcPath, destPath, execPath, iconPath string) error {
	content, err := os.ReadFile(srcPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(content), "\n")
	var newLines []string
	inDesktopEntry := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Monitor current section
		if strings.HasPrefix(trimmed, "[") {
			if strings.HasPrefix(trimmed, "[Desktop Entry]") {
				inDesktopEntry = true
			} else {
				inDesktopEntry = false
			}
		}

		// Only modify Exec and Icon fields within the [Desktop Entry] section
		if inDesktopEntry {
			if strings.HasPrefix(trimmed, "Exec=") {
				// Safely preserve launcher arguments (like %u, %F, etc.)
				originalValue := strings.TrimPrefix(trimmed, "Exec=")
				parts := strings.SplitN(originalValue, " ", 2)
				flags := ""
				if len(parts) > 1 {
					flags = " " + parts[1]
				}
				execVal := execPath
				if strings.Contains(execVal, " ") {
					execVal = "\"" + execVal + "\""
				}
				line = "Exec=" + execVal + flags
			} else if strings.HasPrefix(trimmed, "Icon=") {
				line = "Icon=" + iconPath
			}
		}

		newLines = append(newLines, line)
	}

	return os.WriteFile(destPath, []byte(strings.Join(newLines, "\n")), 0644)
}

// GenerateDesktopFile creates a brand-new desktop entry file
func GenerateDesktopFile(destPath, appName, execPath, iconPath string, terminal bool) error {
	termVal := "false"
	if terminal {
		termVal = "true"
	}

	// Capitalize display name
	displayName := appName
	if len(appName) > 0 {
		displayName = strings.ToUpper(string(appName[0])) + appName[1:]
	}

	execVal := execPath
	if strings.Contains(execVal, " ") {
		execVal = "\"" + execVal + "\""
	}

	content := fmt.Sprintf(`[Desktop Entry]
Version=1.0
Type=Application
Name=%s
Comment=Installed via plop
Exec=%s
Icon=%s
Terminal=%s
Categories=Utility;Development;
`, displayName, execVal, iconPath, termVal)

	return os.WriteFile(destPath, []byte(content), 0644)
}
