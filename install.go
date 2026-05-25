package main

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ExecutableCandidate holds details of a detected runnable binary or script
type ExecutableCandidate struct {
	FullPath string
	SubPath  string
	IsELF    bool
}

// DeduceAppDetails extracts a clean app name and version from an archive filename
func DeduceAppDetails(filename string) (name, version string) {
	base := filepath.Base(filename)
	// Strip known archive extensions
	for {
		ext := filepath.Ext(base)
		if ext == "" {
			break
		}
		extLower := strings.ToLower(ext)
		if extLower == ".tar" || extLower == ".gz" || extLower == ".xz" || extLower == ".bz2" || extLower == ".zip" || extLower == ".tgz" || extLower == ".txz" || extLower == ".tbz2" || extLower == ".appimage" {
			base = strings.TrimSuffix(base, ext)
		} else {
			break
		}
	}

	name = "unknown"
	version = "1.0.0"

	// Special heuristic for SQLite 7-digit encoded versions (e.g., 3530100 -> 3.53.1)
	sqliteRegex := regexp.MustCompile(`\b3[0-9]{6}\b`)
	sqliteLoc := sqliteRegex.FindStringIndex(base)
	if sqliteLoc != nil {
		matchStr := base[sqliteLoc[0]:sqliteLoc[1]]
		major := string(matchStr[0])
		minorStr := matchStr[1:3]
		patchStr := matchStr[3:5]
		buildStr := matchStr[5:7]

		var minor, patch, build int
		fmt.Sscanf(minorStr, "%d", &minor)
		fmt.Sscanf(patchStr, "%d", &patch)
		fmt.Sscanf(buildStr, "%d", &build)

		if build > 0 {
			version = fmt.Sprintf("%s.%d.%d.%d", major, minor, patch, build)
		} else {
			version = fmt.Sprintf("%s.%d.%d", major, minor, patch)
		}

		prefix := base[:sqliteLoc[0]]
		prefix = strings.TrimFunc(prefix, func(r rune) bool {
			return r == '-' || r == '_' || r == '.' || r == ' '
		})
		if prefix != "" {
			name = strings.ToLower(prefix)
			parts := strings.Split(name, "-")
			if len(parts) > 0 {
				name = parts[0]
			}
		}
		return name, version
	}

	// Parse version heuristic using regexp that handles precise SemVer pre-releases
	versionRegex := regexp.MustCompile(`(?i)v?([0-9]+\.[0-9]+(?:\.[0-9]+)*(?:-(?:rc|alpha|beta|dev|pre|preview|patch|post|b|a)(?:\.[0-9]+|[0-9]+)*)?)|(?i)\bv([0-9]+)\b`)
	loc := versionRegex.FindStringIndex(base)

	// If no version matches in base name, try on the full filename/URL path
	if loc == nil {
		locFull := versionRegex.FindStringIndex(filename)
		if locFull != nil {
			matchStr := filename[locFull[0]:locFull[1]]
			version = matchStr
			if strings.HasPrefix(strings.ToLower(version), "v") {
				version = version[1:]
			}
			// App name is deduced from the base name split
			parts := strings.FieldsFunc(base, func(r rune) bool {
				return r == '-' || r == '_'
			})
			if len(parts) > 0 {
				name = strings.ToLower(parts[0])
			}
			return name, version
		}
	}

	if loc != nil {
		// Version is the matched substring
		matchStr := base[loc[0]:loc[1]]
		version = matchStr
		if strings.HasPrefix(strings.ToLower(version), "v") {
			version = version[1:]
		}

		// App name is everything before the version match (or after if before is empty)
		prefix := base[:loc[0]]
		prefix = strings.TrimFunc(prefix, func(r rune) bool {
			return r == '-' || r == '_' || r == '.' || r == ' '
		})

		if prefix != "" {
			name = strings.ToLower(prefix)
		} else {
			// Try everything after the version match
			suffix := base[loc[1]:]
			suffix = strings.TrimFunc(suffix, func(r rune) bool {
				return r == '-' || r == '_' || r == '.' || r == ' '
			})
			if suffix != "" {
				name = strings.ToLower(suffix)
			}
		}
	} else {
		// Fallback if no version is matched in the string
		parts := strings.FieldsFunc(base, func(r rune) bool {
			return r == '-' || r == '_'
		})
		if len(parts) > 0 {
			name = strings.ToLower(parts[0])
		}
	}

	return name, version
}

// ParseGithubRepo extracts the GitHub repository URL from a release asset URL
func ParseGithubRepo(urlStr string) string {
	if strings.Contains(urlStr, "github.com/") {
		parts := strings.Split(urlStr, "github.com/")
		if len(parts) > 1 {
			subparts := strings.Split(parts[1], "/")
			if len(subparts) >= 2 {
				return "https://github.com/" + subparts[0] + "/" + subparts[1]
			}
		}
	}
	return ""
}

// PromptUser asks a question via stdin and returns the trimmed response
func PromptUser(prompt string, defaultVal string) string {
	fmt.Printf(prompt)
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return defaultVal
	}
	input = strings.TrimSpace(input)
	if input == "" {
		return defaultVal
	}
	return input
}

// InstallOptions defines custom installation properties supplied via CLI flags or upgrades
type InstallOptions struct {
	ForcedName          string
	ForcedVersion       string
	ForcedSource        string
	ForcedBinaryName    string
	ForcedInstallScript string
}

// downloadOrPrepareArchive handles downloading from a URL, reading from stdin, or validating a local file path for the source archive
func downloadOrPrepareArchive(sourcePathOrURL string) (archivePath string, archiveSource string, err error) {
	isRemote := strings.HasPrefix(sourcePathOrURL, "http://") || strings.HasPrefix(sourcePathOrURL, "https://")
	if sourcePathOrURL == "-" {
		archiveSource = "stdin"
		tempDownloadDir := filepath.Join(os.TempDir(), "plop-downloads")
		err = os.MkdirAll(tempDownloadDir, 0755)
		if err != nil {
			return "", "", err
		}
		archivePath = filepath.Join(tempDownloadDir, "stdin-archive.tar.gz")
		PrintInfo("Reading archive from standard input...")

		f, err := os.Create(archivePath)
		if err != nil {
			return "", "", fmt.Errorf("failed to create temporary file for stdin: %v", err)
		}

		_, err = io.Copy(f, os.Stdin)
		f.Close()
		if err != nil {
			return "", "", fmt.Errorf("failed to read from stdin: %v", err)
		}

		// Sniff magic bytes of the temp file to determine the correct extension
		ext := ".tar.gz" // default fallback
		fRead, oerr := os.Open(archivePath)
		if oerr == nil {
			header := make([]byte, 6)
			n, _ := fRead.Read(header)
			fRead.Close()
			if n >= 2 && header[0] == 0x50 && header[1] == 0x4B { // 'PK' (ZIP)
				ext = ".zip"
			} else if n >= 2 && header[0] == 0x1f && header[1] == 0x8b { // Gzip
				ext = ".tar.gz"
			} else if n >= 3 && header[0] == 'B' && header[1] == 'Z' && header[2] == 'h' { // Bzip2
				ext = ".tar.bz2"
			} else if n >= 6 && header[0] == 0xFD && header[1] == 0x37 && header[2] == 0x7A && header[3] == 0x58 && header[4] == 0x5A && header[5] == 0x00 { // Xz
				ext = ".tar.xz"
			}
		}

		if ext != ".tar.gz" {
			newPath := strings.TrimSuffix(archivePath, ".tar.gz") + ext
			os.Rename(archivePath, newPath)
			archivePath = newPath
		}
	} else if isRemote {
		archiveSource = sourcePathOrURL
		tempDownloadDir := filepath.Join(os.TempDir(), "plop-downloads")
		err = os.MkdirAll(tempDownloadDir, 0755)
		if err != nil {
			return "", "", err
		}

		uparts := strings.Split(sourcePathOrURL, "/")
		uName := uparts[len(uparts)-1]
		if uName == "" {
			uName = "downloaded-archive.tar.gz"
		}

		archivePath = filepath.Join(tempDownloadDir, uName)
		PrintInfo("Downloading remote archive: %s...", sourcePathOrURL)
		err = DownloadFile(sourcePathOrURL, archivePath)
		if err != nil {
			return "", "", fmt.Errorf("download failed: %v", err)
		}
	} else {
		archivePath = ExpandTilde(sourcePathOrURL)
		archiveSource = "local"
		if !FileExists(archivePath) {
			return "", "", fmt.Errorf("local file does not exist: %s", archivePath)
		}
	}
	return archivePath, archiveSource, nil
}

func determineAppMetadata(sourcePathOrURL, archivePath string, options InstallOptions, config *Config) (appName string, appVersion string, err error) {
	appName, appVersion = DeduceAppDetails(sourcePathOrURL)
	if appName == "unknown" || appVersion == "1.0.0" {
		name2, ver2 := DeduceAppDetails(archivePath)
		if appName == "unknown" && name2 != "unknown" {
			appName = name2
		}
		if appVersion == "1.0.0" && ver2 != "1.0.0" {
			appVersion = ver2
		}
	}
	if options.ForcedName != "" {
		appName = options.ForcedName
	}
	if options.ForcedVersion != "" {
		appVersion = options.ForcedVersion
	}
	PrintInfo("Deduced application name: '%s', version: '%s'", appName, appVersion)

	if options.ForcedName == "" && !config.AutoConfirm {
		appName = PromptUser(fmt.Sprintf("Confirm application name [%s]: ", appName), appName)
	}
	appName = strings.ToLower(strings.TrimSpace(appName))

	if options.ForcedVersion == "" && !config.AutoConfirm {
		appVersion = PromptUser(fmt.Sprintf("Confirm application version [%s]: ", appVersion), appVersion)
	}
	appVersion = strings.TrimSpace(appVersion)

	return appName, appVersion, nil
}

func handleExistingInstallation(appName string, config *Config) (targetAppDir string, err error) {
	optDir := ExpandTilde(config.OptDir)
	targetAppDir = filepath.Join(optDir, appName)

	if FileExists(targetAppDir) {
		if !config.AutoConfirm {
			ans := PromptUser(fmt.Sprintf("Application '%s' is already installed at '%s'. Overwrite? [y/N]: ", appName, targetAppDir), "n")
			if strings.ToLower(ans) != "y" {
				PrintWarning("Installation cancelled.")
				return "", fmt.Errorf("installation cancelled by user")
			}
		}
		PrintInfo("Removing existing installation folder...")
		err = os.RemoveAll(targetAppDir)
		if err != nil {
			return "", fmt.Errorf("failed to clear existing directory: %v", err)
		}
	}
	return targetAppDir, nil
}

func extractAppImage(archivePath, appName, targetAppDir string) (sourceRoot string, cleanupDir string, err error) {
	PrintInfo("AppImage detected. Copying file and extracting squashfs contents to scan for desktop files/icons...")
	err = os.MkdirAll(targetAppDir, 0755)
	if err != nil {
		return "", "", err
	}

	targetAppImagePath := filepath.Join(targetAppDir, appName+".AppImage")
	err = CopyFile(archivePath, targetAppImagePath)
	if err != nil {
		return "", "", fmt.Errorf("failed to copy AppImage to target directory: %v", err)
	}

	os.Chmod(archivePath, 0755)
	os.Chmod(targetAppImagePath, 0755)

	rand.Seed(time.Now().UnixNano())
	tempExtractDir := filepath.Join(os.TempDir(), fmt.Sprintf("plop-appimage-%s-%d", appName, rand.Intn(100000)))
	err = os.MkdirAll(tempExtractDir, 0755)
	if err == nil {
		cmd := exec.Command(archivePath, "--appimage-extract")
		cmd.Dir = tempExtractDir
		extractErr := cmd.Run()
		if extractErr == nil {
			sourceRoot = filepath.Join(tempExtractDir, "squashfs-root")
			cleanupDir = tempExtractDir
		} else {
			PrintWarning("Could not extract AppImage squashfs (failed to run --appimage-extract): %v. Will fall back to auto-generating desktop entry.", extractErr)
			os.RemoveAll(tempExtractDir)
		}
	}
	return sourceRoot, cleanupDir, nil
}

func extractRawBinary(archivePath, targetAppDir string) error {
	PrintInfo("Raw standalone binary/script detected. Copying to sandboxed directory...")
	err := os.MkdirAll(targetAppDir, 0755)
	if err != nil {
		return err
	}

	binaryBaseName := filepath.Base(archivePath)
	targetBinaryPath := filepath.Join(targetAppDir, binaryBaseName)
	err = CopyFile(archivePath, targetBinaryPath)
	if err != nil {
		return fmt.Errorf("failed to copy raw binary to target directory: %v", err)
	}

	os.Chmod(archivePath, 0755)
	os.Chmod(targetBinaryPath, 0755)
	return nil
}

func extractArchiveAndBuild(archivePath, appName, targetAppDir, binDir, customInstallScript string) (sourceRoot string, customInstallExecuted bool, cleanupDir string, err error) {
	rand.Seed(time.Now().UnixNano())
	tempExtractDir := filepath.Join(os.TempDir(), fmt.Sprintf("plop-extract-%s-%d", appName, rand.Intn(100000)))
	PrintInfo("Extracting archive contents...")
	err = ExtractArchive(archivePath, tempExtractDir)
	if err != nil {
		return "", false, "", err
	}
	cleanupDir = tempExtractDir

	if customInstallScript != "" {
		scriptPath := ExpandTilde(customInstallScript)
		if !FileExists(scriptPath) {
			return "", false, cleanupDir, fmt.Errorf("custom install script does not exist: %s", scriptPath)
		}
		PrintInfo("Executing custom install script: %s...", scriptPath)

		cmd := exec.Command(scriptPath, tempExtractDir, targetAppDir, binDir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err = cmd.Run()
		if err != nil {
			return "", false, cleanupDir, fmt.Errorf("custom install script failed: %v", err)
		}
		PrintSuccess("Custom install script completed successfully!")

		sourceRoot = targetAppDir
		customInstallExecuted = true
	} else {
		items, err := os.ReadDir(tempExtractDir)
		if err != nil {
			return "", false, cleanupDir, err
		}

		var significantItems []os.DirEntry
		for _, item := range items {
			name := item.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "._") || name == "__MACOSX" || name == "pax_global_header" {
				continue
			}
			significantItems = append(significantItems, item)
		}

		sourceRoot = tempExtractDir
		if len(significantItems) == 1 && significantItems[0].IsDir() {
			sourceRoot = filepath.Join(tempExtractDir, significantItems[0].Name())
			PrintInfo("Detected single significant top-level folder '%s'. Unwrapping contents...", significantItems[0].Name())
		}
	}
	return sourceRoot, customInstallExecuted, cleanupDir, nil
}

func extractSourceAndRunBuild(archivePath, appName, targetAppDir, binDir, customInstallScript string, isAppImage, isRawBinary bool) (sourceRoot string, customInstallExecuted bool, cleanupDir string, err error) {
	if isAppImage {
		sourceRoot, cleanupDir, err = extractAppImage(archivePath, appName, targetAppDir)
		return sourceRoot, false, cleanupDir, err
	}
	if isRawBinary {
		err = extractRawBinary(archivePath, targetAppDir)
		return "", false, "", err
	}
	return extractArchiveAndBuild(archivePath, appName, targetAppDir, binDir, customInstallScript)
}

func scanArchiveContents(sourceRoot string, isAppImage bool) (executables []ExecutableCandidate, desktopFiles []string, imageFiles []string, err error) {
	if sourceRoot == "" {
		return nil, nil, nil, nil
	}

	err = filepath.Walk(sourceRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			dirName := strings.ToLower(info.Name())
			if dirName == "lib" || dirName == "libexec" || dirName == "share" || dirName == "src" || dirName == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		sub, _ := filepath.Rel(sourceRoot, path)
		ext := strings.ToLower(filepath.Ext(path))

		if ext == ".desktop" {
			desktopFiles = append(desktopFiles, path)
		}

		if ext == ".png" || ext == ".svg" || ext == ".xpm" || ext == ".jpg" || ext == ".jpeg" {
			imageFiles = append(imageFiles, path)
		}

		if !isAppImage && info.Mode().IsRegular() && (info.Mode()&0111 != 0) {
			isElf, _ := IsELF(path)
			isScript, _ := IsScript(path)

			if isElf || isScript {
				fileName := strings.ToLower(info.Name())
				if !strings.HasPrefix(fileName, "lib") || !strings.Contains(fileName, ".so") {
					executables = append(executables, ExecutableCandidate{
						FullPath: path,
						SubPath:  sub,
						IsELF:    isElf,
					})
				}
			}
		}
		return nil
	})
	return executables, desktopFiles, imageFiles, err
}

func selectTargetBinaries(appName string, executables []ExecutableCandidate, reg *Registry, config *Config, options InstallOptions) ([]ExecutableCandidate, error) {
	var selectedBinaries []ExecutableCandidate
	var oldSelected []string
	optDir := ExpandTilde(config.OptDir)

	oldApp, exists := reg.Apps[appName]
	if exists {
		if len(oldApp.SelectedBinaries) > 0 {
			oldSelected = oldApp.SelectedBinaries
		} else if oldApp.BinaryPath != "" {
			oldOptPath := filepath.Join(optDir, appName)
			rel, err := filepath.Rel(oldOptPath, oldApp.BinaryPath)
			if err == nil && !strings.HasPrefix(rel, "..") {
				oldSelected = []string{rel}
			}
		}
	}

	hasAdditional := false
	if len(oldSelected) > 0 {
		for _, exe := range executables {
			found := false
			for _, old := range oldSelected {
				if exe.SubPath == old {
					found = true
					break
				}
			}
			if !found {
				hasAdditional = true
				break
			}
		}
	}

	var matchedBinaries []ExecutableCandidate
	if len(oldSelected) > 0 {
		for _, old := range oldSelected {
			for _, exe := range executables {
				if exe.SubPath == old {
					matchedBinaries = append(matchedBinaries, exe)
					break
				}
			}
		}
	}

	if len(oldSelected) > 0 && !hasAdditional && len(matchedBinaries) > 0 {
		selectedBinaries = matchedBinaries
		PrintSuccess("Automatically preserved previous binary symlinks: %v", oldSelected)
	} else if len(executables) == 1 {
		selectedBinaries = append(selectedBinaries, executables[0])
		PrintSuccess("Automatically detected main executable: %s", executables[0].SubPath)
	} else {
		matchIndex := -1
		for i, exe := range executables {
			baseName := strings.ToLower(filepath.Base(exe.FullPath))
			if baseName == appName {
				matchIndex = i
				break
			}
		}

		if matchIndex != -1 && config.AutoConfirm && len(oldSelected) == 0 {
			selectedBinaries = append(selectedBinaries, executables[matchIndex])
			PrintSuccess("Automatically matched primary executable: %s", executables[matchIndex].SubPath)
		} else if len(oldSelected) > 0 && len(matchedBinaries) > 0 && config.AutoConfirm {
			selectedBinaries = matchedBinaries
			PrintSuccess("Automatically preserved previous binary symlinks (AutoConfirm): %v", oldSelected)
		} else {
			fmt.Println("\n" + color(ColorBold, "Multiple executables detected. Please select the binaries to symlink:"))
			if len(oldSelected) > 0 {
				fmt.Printf("  (Previously selected: %v)\n", oldSelected)
			}
			fmt.Println("  (You can enter a single number, comma-separated numbers like '1,3', or 'all')")
			for i, exe := range executables {
				binaryType := "Launch Script"
				if exe.IsELF {
					binaryType = "ELF Executable"
				}
				isPrev := false
				for _, old := range oldSelected {
					if exe.SubPath == old {
						isPrev = true
						break
					}
				}
				highlight := ""
				if isPrev {
					highlight = " " + color(ColorGreen, "[previously selected]")
				}
				fmt.Printf("  ["+color(ColorYellow, "%d")+"] %s (%s)%s\n", i+1, exe.SubPath, binaryType, highlight)
			}

			for {
				ans := PromptUser("Select binary number(s) [1]: ", "1")
				ans = strings.ToLower(strings.TrimSpace(ans))

				if ans == "all" {
					selectedBinaries = executables
					break
				}

				parts := strings.Split(ans, ",")
				valid := true
				var tempSelected []ExecutableCandidate

				for _, p := range parts {
					p = strings.TrimSpace(p)
					var idx int
					_, parseErr := fmt.Sscanf(p, "%d", &idx)
					if parseErr != nil || idx < 1 || idx > len(executables) {
						valid = false
						break
					}
					tempSelected = append(tempSelected, executables[idx-1])
				}

				if valid && len(tempSelected) > 0 {
					selectedBinaries = tempSelected
					break
				}
				PrintWarning("Invalid selection. Please enter numbers between 1 and %d, comma-separated, or 'all'.", len(executables))
			}
		}
	}
	return selectedBinaries, nil
}

func scoreAndSelectIcon(appName, iconsDir string, imageFiles []string) (selectedIcon string, err error) {
	if len(imageFiles) > 0 {
		highScore := -1
		bestIconPath := ""

		for _, imgPath := range imageFiles {
			score := 0
			lowerPath := strings.ToLower(imgPath)
			lowerName := strings.ToLower(filepath.Base(imgPath))

			if strings.Contains(lowerName, appName) {
				score += 50
			}

			if strings.Contains(lowerPath, "icon") || strings.Contains(lowerPath, "pixmap") || strings.Contains(lowerPath, "logo") {
				score += 40
			}

			if strings.Contains(lowerPath, "256x256") || strings.Contains(lowerPath, "512x512") || strings.Contains(lowerPath, "scalable") {
				score += 30
			} else if strings.Contains(lowerPath, "128x128") || strings.Contains(lowerPath, "48x48") {
				score += 15
			}

			if strings.HasSuffix(lowerName, ".jpg") || strings.HasSuffix(lowerName, ".jpeg") {
				score -= 10
			}

			if score > highScore {
				highScore = score
				bestIconPath = imgPath
			}
		}

		if bestIconPath != "" {
			err = os.MkdirAll(iconsDir, 0755)
			if err == nil {
				ext := filepath.Ext(bestIconPath)
				targetIconPath := filepath.Join(iconsDir, appName+ext)
				srcFile, srcErr := os.ReadFile(bestIconPath)
				if srcErr == nil {
					writeErr := os.WriteFile(targetIconPath, srcFile, 0644)
					if writeErr == nil {
						selectedIcon = targetIconPath
						PrintInfo("Selected and registered application icon: %s", filepath.Base(targetIconPath))
					}
				}
			}
		}
	}

	if selectedIcon == "" {
		selectedIcon = "application-x-executable"
		PrintInfo("No icon found in archive. Registering generic system fallback icon.")
	}
	return selectedIcon, nil
}

func deployFilesToOpt(sourceRoot, targetAppDir string, isAppImage, isRawBinary, customInstallExecuted bool) error {
	if !isAppImage && !isRawBinary && !customInstallExecuted {
		PrintInfo("Installing files to target directory '%s'...", targetAppDir)
		err := os.MkdirAll(filepath.Dir(targetAppDir), 0755)
		if err != nil {
			return err
		}

		err = os.Rename(sourceRoot, targetAppDir)
		if err != nil {
			cmd := exec.Command("cp", "-r", sourceRoot, targetAppDir)
			err = cmd.Run()
			if err != nil {
				return fmt.Errorf("failed to copy files to target opt directory: %v", err)
			}
		}
	}
	return nil
}

func createSymlinks(selectedBinaries []ExecutableCandidate, targetAppDir, binDir string, options InstallOptions, config *Config) (symlinkPath string, err error) {
	err = os.MkdirAll(binDir, 0755)
	if err != nil {
		return "", err
	}

	for i, exe := range selectedBinaries {
		currInstalledExecPath := filepath.Join(targetAppDir, exe.SubPath)
		currBinaryName := filepath.Base(exe.FullPath)
		isAppImage := strings.HasSuffix(strings.ToLower(exe.FullPath), ".appimage")
		if isAppImage {
			currBinaryName = strings.TrimSuffix(currBinaryName, filepath.Ext(currBinaryName))
		}
		currBinaryName = strings.ReplaceAll(currBinaryName, " ", "-")

		if i == 0 {
			if options.ForcedBinaryName != "" {
				currBinaryName = options.ForcedBinaryName
			} else if !config.AutoConfirm {
				currBinaryName = PromptUser(fmt.Sprintf("Confirm primary binary name [%s]: ", currBinaryName), currBinaryName)
			}
			currBinaryName = strings.TrimSpace(currBinaryName)
			symlinkPath = filepath.Join(binDir, currBinaryName)
		} else {
			PrintInfo("Creating secondary binary symlink for: %s...", currBinaryName)
		}

		currSymlinkPath := filepath.Join(binDir, currBinaryName)
		if FileExists(currSymlinkPath) {
			os.Remove(currSymlinkPath)
		}

		err = os.Symlink(currInstalledExecPath, currSymlinkPath)
		if err != nil {
			return "", fmt.Errorf("failed to create executable symlink for %s: %v", currBinaryName, err)
		}
		PrintSuccess("Linked executable to: %s", currSymlinkPath)
	}
	return symlinkPath, nil
}

func registerDesktopLauncher(appName, symlinkPath, selectedIcon, targetAppDir, sourceRoot string, desktopFiles []string, isAppImage, isGUI bool, config *Config) (desktopPath string, err error) {
	appsDir := ExpandTilde(config.AppsDir)
	err = os.MkdirAll(appsDir, 0755)
	if err != nil {
		return "", err
	}

	desktopPath = filepath.Join(appsDir, appName+".desktop")

	shippedDesktop := ""
	for _, dPath := range desktopFiles {
		shippedDesktop = dPath
		if strings.Contains(strings.ToLower(filepath.Base(dPath)), appName) {
			break
		}
	}

	if shippedDesktop != "" {
		PrintInfo("Using pre-existing desktop entry shipped in the application...")
		desktopSource := shippedDesktop
		if !isAppImage {
			shippedRel, _ := filepath.Rel(sourceRoot, shippedDesktop)
			desktopSource = filepath.Join(targetAppDir, shippedRel)
		}
		err = ProcessDesktopFile(desktopSource, desktopPath, symlinkPath, selectedIcon)
		if err != nil {
			PrintWarning("Failed to adapt shipped desktop file: %v. Falling back to generation.", err)
			err = GenerateDesktopFile(desktopPath, appName, symlinkPath, selectedIcon, !isGUI)
		}
	} else {
		err = GenerateDesktopFile(desktopPath, appName, symlinkPath, selectedIcon, !isGUI)
	}

	if err != nil {
		PrintWarning("Could not create desktop launcher file: %v", err)
		return "", err
	}
	PrintSuccess("Desktop launcher registered at: %s", desktopPath)
	return desktopPath, nil
}

func associateUpdateURL(sourcePathOrURL string, isRemote bool, options InstallOptions, config *Config) (updateURL string) {
	if options.ForcedSource != "" {
		updateURL = options.ForcedSource
	} else if isRemote {
		updateURL = ParseGithubRepo(sourcePathOrURL)
	}

	if options.ForcedSource == "" && !config.AutoConfirm {
		fmt.Println()
		defaultPrompt := "Leave blank to skip"
		if updateURL != "" {
			defaultPrompt = updateURL
		}
		ans := PromptUser(fmt.Sprintf("Associate a GitHub repository for updates?\n(e.g., https://github.com/user/repo) [%s]: ", defaultPrompt), updateURL)
		updateURL = ans
	}
	return updateURL
}

func registerInstalledApp(appName, appVersion, archiveSource, updateURL, installedExecPath, symlinkPath, desktopPath, selectedIcon, customInstallScript string, selectedSubPaths []string, reg *Registry) error {
	reg.Apps[appName] = AppMetadata{
		Name:             appName,
		Version:          appVersion,
		InstalledAt:      time.Now().Format("2006-01-02 15:04:05"),
		ArchiveSource:    archiveSource,
		UpdateURL:        updateURL,
		BinaryPath:       installedExecPath,
		SymlinkPath:      symlinkPath,
		DesktopPath:      desktopPath,
		IconPath:         selectedIcon,
		InstallScript:    customInstallScript,
		SelectedBinaries: selectedSubPaths,
	}

	err := SaveRegistry(reg)
	if err != nil {
		return fmt.Errorf("failed to save registry metadata: %v", err)
	}

	fmt.Println()
	PrintSuccess("Successfully installed %s (%s)!", appName, appVersion)
	return nil
}

func detectFormat(archivePath string) (isAppImage bool, isRawBinary bool) {
	isAppImage = strings.HasSuffix(strings.ToLower(archivePath), ".appimage")
	if isAppImage {
		return true, false
	}
	isElf, _ := IsELF(archivePath)
	isScript, _ := IsScript(archivePath)
	ext := strings.ToLower(filepath.Ext(archivePath))
	isArchiveExt := ext == ".zip" || ext == ".tar" || ext == ".gz" || ext == ".xz" || ext == ".bz2" || ext == ".tgz" || ext == ".txz" || ext == ".tbz2"
	if (isElf || isScript) && !isArchiveExt {
		return false, true
	}
	return false, false
}

func resolveCustomInstallScript(options InstallOptions, config *Config, isAppImage, isRawBinary bool) string {
	customInstallScript := options.ForcedInstallScript
	if customInstallScript == "" && !config.AutoConfirm && !isAppImage && !isRawBinary {
		ans := PromptUser("Specify custom install script (optional) [None]: ", "")
		customInstallScript = strings.TrimSpace(ans)
	}
	return customInstallScript
}

func scanInstallCandidates(sourceRoot, archivePath, targetAppDir, appName string, isAppImage, isRawBinary bool) (executables []ExecutableCandidate, desktopFiles []string, imageFiles []string, err error) {
	if isRawBinary {
		binaryBaseName := filepath.Base(archivePath)
		targetBinaryPath := filepath.Join(targetAppDir, binaryBaseName)
		isElf, _ := IsELF(targetBinaryPath)
		executables = append(executables, ExecutableCandidate{
			FullPath: targetBinaryPath,
			SubPath:  binaryBaseName,
			IsELF:    isElf,
		})
	} else if isAppImage {
		targetAppImagePath := filepath.Join(targetAppDir, appName+".AppImage")
		executables = append(executables, ExecutableCandidate{
			FullPath: targetAppImagePath,
			SubPath:  appName + ".AppImage",
			IsELF:    true,
		})
		execs, desktops, imgs, scanErr := scanArchiveContents(sourceRoot, isAppImage)
		if scanErr == nil {
			desktopFiles = desktops
			imageFiles = imgs
			_ = execs
		}
	} else {
		execs, desktops, imgs, scanErr := scanArchiveContents(sourceRoot, isAppImage)
		if scanErr != nil {
			return nil, nil, nil, scanErr
		}
		executables = execs
		desktopFiles = desktops
		imageFiles = imgs
	}
	return executables, desktopFiles, imageFiles, nil
}

func handleDesktopLauncher(appName, symlinkPath, selectedIcon, targetAppDir, sourceRoot string, desktopFiles []string, isAppImage bool, config *Config) (desktopPath string, err error) {
	isGUI := true
	createLauncher := true
	if config.DefaultGUI != nil {
		isGUI = *config.DefaultGUI
	} else if !config.AutoConfirm {
		launcherAns := PromptUser("Create a desktop launcher? [Y/n]: ", "y")
		if strings.ToLower(launcherAns) != "y" {
			createLauncher = false
		} else {
			guiAns := PromptUser("Is this a GUI application? (If no, it runs in a terminal) [Y/n]: ", "y")
			if strings.ToLower(guiAns) != "y" {
				isGUI = false
			}
		}
	}

	if createLauncher {
		return registerDesktopLauncher(
			appName, symlinkPath, selectedIcon, targetAppDir, sourceRoot, desktopFiles, isAppImage, isGUI, config,
		)
	}
	return "", nil
}

// InstallApp carries out the absolute installation lifecycle
func InstallApp(sourcePathOrURL string, options InstallOptions, config *Config, reg *Registry) error {
	archivePath, archiveSource, err := downloadOrPrepareArchive(sourcePathOrURL)
	if err != nil {
		return err
	}

	appName, appVersion, err := determineAppMetadata(sourcePathOrURL, archivePath, options, config)
	if err != nil {
		return err
	}

	targetAppDir, err := handleExistingInstallation(appName, config)
	if err != nil {
		return err
	}

	isAppImage, isRawBinary := detectFormat(archivePath)
	customInstallScript := resolveCustomInstallScript(options, config, isAppImage, isRawBinary)

	sourceRoot, customInstallExecuted, cleanupDir, err := extractSourceAndRunBuild(
		archivePath, appName, targetAppDir, ExpandTilde(config.BinDir), customInstallScript, isAppImage, isRawBinary,
	)
	if err != nil {
		return err
	}
	if cleanupDir != "" {
		defer os.RemoveAll(cleanupDir)
	}

	executables, desktopFiles, imageFiles, err := scanInstallCandidates(sourceRoot, archivePath, targetAppDir, appName, isAppImage, isRawBinary)
	if err != nil {
		return err
	}

	if len(executables) == 0 {
		return fmt.Errorf("no executable ELF binaries or launch scripts detected in the archive")
	}

	selectedBinaries, err := selectTargetBinaries(appName, executables, reg, config, options)
	if err != nil {
		return err
	}
	selectedBinary := selectedBinaries[0]

	selectedIcon, err := scoreAndSelectIcon(appName, ExpandTilde(config.IconsDir), imageFiles)
	if err != nil {
		return err
	}

	err = deployFilesToOpt(sourceRoot, targetAppDir, isAppImage, isRawBinary, customInstallExecuted)
	if err != nil {
		return err
	}

	symlinkPath, err := createSymlinks(selectedBinaries, targetAppDir, ExpandTilde(config.BinDir), options, config)
	if err != nil {
		return err
	}

	desktopPath, err := handleDesktopLauncher(appName, symlinkPath, selectedIcon, targetAppDir, sourceRoot, desktopFiles, isAppImage, config)
	if err != nil {
		return err
	}

	isRemote := strings.HasPrefix(sourcePathOrURL, "http://") || strings.HasPrefix(sourcePathOrURL, "https://")
	updateURL := associateUpdateURL(sourcePathOrURL, isRemote, options, config)

	var selectedSubPaths []string
	for _, exe := range selectedBinaries {
		selectedSubPaths = append(selectedSubPaths, exe.SubPath)
	}

	installedExecPath := filepath.Join(targetAppDir, selectedBinary.SubPath)
	return registerInstalledApp(
		appName, appVersion, archiveSource, updateURL, installedExecPath, symlinkPath, desktopPath, selectedIcon, customInstallScript, selectedSubPaths, reg,
	)
}
