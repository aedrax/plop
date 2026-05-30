package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

// GithubAsset represents a release file on GitHub
type GithubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// GithubRelease represents the latest release response from GitHub
type GithubRelease struct {
	TagName string        `json:"tag_name"`
	HTMLURL string        `json:"html_url"`
	Assets  []GithubAsset `json:"assets"`
}

// FetchLatestRelease queries the GitHub API or scrapes a generic web page for the latest release
func FetchLatestRelease(updateURL string, token string) (*GithubRelease, error) {
	// If it is a source checking plugin, execute the local script/binary!
	if strings.HasPrefix(updateURL, "plugin://") {
		scriptPath := strings.TrimPrefix(updateURL, "plugin://")
		PrintInfo("Executing source checking plugin script: %s...", scriptPath)
		return RunSourceCheckingPlugin(scriptPath)
	}

	// If it is a generic/non-GitHub URL, route to the HTML scraper
	if !strings.Contains(updateURL, "github.com/") {
		if IsDarwin() {
			PrintInfo("Update URL is a generic web page. Scraping for compatible macOS downloads...")
		} else {
			PrintInfo("Update URL is a generic web page. Scraping for compatible Linux downloads...")
		}
		return ScrapeGenericRelease(updateURL)
	}

	// Extract user/repo from GitHub URL
	parts := strings.Split(updateURL, "github.com/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid github url: %s", updateURL)
	}

	repo := strings.TrimSuffix(parts[1], ".git")
	repo = strings.TrimSuffix(repo, "/")

	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)

	client := &http.Client{}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "plop-installer/1.0")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("no releases found for repository %s", repo)
		}
		if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, fmt.Errorf("GitHub API rate limit exceeded. Please configure a github_token in ~/.config/plop/config.json")
		}
		return nil, fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	var release GithubRelease
	err = json.NewDecoder(resp.Body).Decode(&release)
	if err != nil {
		return nil, err
	}

	return &release, nil
}

// FindBestAsset selects the best release asset for the current platform and architecture.
// On macOS: matches darwin/macos/osx + arm64/aarch64 or amd64/x86_64/x64
// On Linux: matches linux + amd64/x86_64/x64 (existing logic)
func FindBestAsset(release *GithubRelease) (string, string) {
	if len(release.Assets) == 1 {
		return release.Assets[0].BrowserDownloadURL, release.Assets[0].Name
	}

	if runtime.GOOS == "darwin" {
		return findBestDarwinAsset(release)
	}
	return findBestLinuxAsset(release)
}

// findBestDarwinAsset selects the best macOS release asset for the current architecture.
func findBestDarwinAsset(release *GithubRelease) (string, string) {
	// Determine architecture identifiers based on GOARCH
	var archIdentifiers []string
	switch runtime.GOARCH {
	case "arm64":
		archIdentifiers = []string{"arm64", "aarch64"}
	default: // amd64
		archIdentifiers = []string{"amd64", "x86_64", "x64"}
	}

	// Phase 1: Exact platform + architecture + valid extension match
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		urlStr := strings.ToLower(asset.BrowserDownloadURL)

		if !isDarwinPlatform(name, urlStr) {
			continue
		}
		if !isDarwinValidExtension(name) {
			continue
		}
		if matchesArch(name, urlStr, archIdentifiers) {
			return asset.BrowserDownloadURL, asset.Name
		}
	}

	// Phase 2: Platform match + valid extension (no arch match, fallback)
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		urlStr := strings.ToLower(asset.BrowserDownloadURL)

		if !isDarwinPlatform(name, urlStr) {
			continue
		}
		if !isDarwinValidExtension(name) {
			continue
		}
		return asset.BrowserDownloadURL, asset.Name
	}

	// Phase 3: No platform match, return first available asset
	if len(release.Assets) > 0 {
		return release.Assets[0].BrowserDownloadURL, release.Assets[0].Name
	}

	return "", ""
}

// isDarwinPlatform checks if the asset name or URL contains a macOS platform identifier.
func isDarwinPlatform(name, urlStr string) bool {
	return strings.Contains(name, "darwin") || strings.Contains(urlStr, "darwin") ||
		strings.Contains(name, "macos") || strings.Contains(urlStr, "macos") ||
		strings.Contains(name, "osx") || strings.Contains(urlStr, "osx")
}

// isDarwinValidExtension checks if the asset has an accepted macOS extension and is not an AppImage.
func isDarwinValidExtension(name string) bool {
	if strings.HasSuffix(name, ".appimage") {
		return false
	}
	return strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tar.xz") ||
		strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".dmg")
}

// matchesArch checks if the asset name or URL contains one of the given architecture identifiers.
func matchesArch(name, urlStr string, archIdentifiers []string) bool {
	for _, arch := range archIdentifiers {
		if strings.Contains(name, arch) || strings.Contains(urlStr, arch) {
			return true
		}
	}
	return false
}

// findBestLinuxAsset preserves the existing Linux asset selection logic.
func findBestLinuxAsset(release *GithubRelease) (string, string) {
	// 1. Strict architecture and format matching
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		urlStr := strings.ToLower(asset.BrowserDownloadURL)

		// Linux AppImage or shell script installer (.run) are inherently Linux-only formats
		isLinux := strings.Contains(name, "linux") || strings.Contains(urlStr, "linux") ||
			strings.Contains(name, "ubuntu") || strings.Contains(urlStr, "ubuntu") ||
			strings.Contains(name, "debian") || strings.Contains(urlStr, "debian") ||
			strings.HasSuffix(name, ".appimage") || strings.HasSuffix(name, ".run")

		isArch := strings.Contains(name, "amd64") || strings.Contains(urlStr, "amd64") ||
			strings.Contains(name, "x86_64") || strings.Contains(urlStr, "x86_64") ||
			strings.Contains(name, "x64") || strings.Contains(urlStr, "x64") ||
			strings.HasSuffix(name, ".appimage") || strings.HasSuffix(name, ".run")

		if isLinux && isArch {
			if strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tar.xz") ||
				strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tgz") ||
				strings.HasSuffix(name, ".txz") || strings.HasSuffix(name, ".tar.bz2") ||
				strings.HasSuffix(name, ".tbz2") || strings.HasSuffix(name, ".tar") ||
				strings.HasSuffix(name, ".appimage") || strings.HasSuffix(name, ".run") {
				return asset.BrowserDownloadURL, asset.Name
			}
		}
	}

	// 2. Looser fallback matching (at least isLinux or isArch + known extension)
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.Name)
		urlStr := strings.ToLower(asset.BrowserDownloadURL)

		isLinux := strings.Contains(name, "linux") || strings.Contains(urlStr, "linux") ||
			strings.HasSuffix(name, ".appimage") || strings.HasSuffix(name, ".run")

		isArch := strings.Contains(name, "amd64") || strings.Contains(urlStr, "amd64") ||
			strings.Contains(name, "x86_64") || strings.Contains(urlStr, "x86_64") ||
			strings.Contains(name, "x64") || strings.Contains(urlStr, "x64")

		if isLinux || isArch {
			if strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".tar.xz") ||
				strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".appimage") ||
				strings.HasSuffix(name, ".run") {
				return asset.BrowserDownloadURL, asset.Name
			}
		}
	}

	// 3. Last resort fallback (return first asset if available)
	if len(release.Assets) > 0 {
		return release.Assets[0].BrowserDownloadURL, release.Assets[0].Name
	}

	return "", ""
}

// RunUpdateCheck queries all installed apps and prints update summaries
func RunUpdateCheck(config *Config, reg *Registry) error {
	if len(reg.Apps) == 0 {
		PrintInfo("No applications currently installed.")
		return nil
	}

	PrintInfo("Checking for new releases...")
	hasUpdates := false

	// Collect keys and sort alphabetically
	var keys []string
	for name := range reg.Apps {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	var outputLines []string

	for _, name := range keys {
		app := reg.Apps[name]
		if app.UpdateURL == "" {
			line := fmt.Sprintf("  %-15s Current: %-10s -> Update URL: [Not Registered]", name, app.Version)
			outputLines = append(outputLines, line)
			continue
		}

		release, err := FetchLatestRelease(app.UpdateURL, config.GithubToken)
		if err != nil {
			line := fmt.Sprintf("  %-15s Current: %-10s -> Error checking update: %v", name, app.Version, err)
			outputLines = append(outputLines, line)
			continue
		}

		// check the version information from the release, and if it is newer than the current version, print it out as an available update
		latestVersion := strings.TrimPrefix(release.TagName, "v")
		if latestVersion != app.Version {
			line := fmt.Sprintf("  %-15s Current: "+color(ColorYellow, "%-10s")+" -> Latest: "+color(ColorGreen, "%-10s")+" ("+color(ColorBold, "Available!")+")", name, app.Version, latestVersion)
			outputLines = append(outputLines, line)
			hasUpdates = true
		} else {
			line := fmt.Sprintf("  %-15s Current: "+color(ColorGreen, "%-10s")+" -> Latest: %-10s (Up to date)", name, app.Version, latestVersion)
			outputLines = append(outputLines, line)
		}
	}

	// Print all lines at once now that everything is fetched!
	fmt.Println()
	for _, line := range outputLines {
		fmt.Println(line)
	}

	if hasUpdates {
		fmt.Println("\nRun `plop upgrade <app>` or `plop upgrade` to download and apply updates!")
	} else {
		PrintSuccess("\nAll applications are fully up to date!")
	}

	return nil
}

// ScrapeGenericRelease fetches a non-GitHub web page and finds the latest version/download link
func ScrapeGenericRelease(updateURL string) (*GithubRelease, error) {
	resp, err := http.Get(updateURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web page returned HTTP status %d", resp.StatusCode)
	}

	// Read body (up to 2MB to prevent memory exhaustion on giant pages)
	limitedReader := io.LimitReader(resp.Body, 2*1024*1024)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}
	bodyStr := string(bodyBytes)

	// Scan for both standard HTML href="..." links AND any quoted archive URLs (bypasses JS obfuscations like SQLite's)
	hrefRegex := regexp.MustCompile(`(?i)href=["']([^"']+)["']|["']([^"']+\.(?:zip|tar|tar\.gz|tar\.xz|tgz|txz|tbz2|tar\.bz2|AppImage|appimage|dmg))["']`)
	matches := hrefRegex.FindAllStringSubmatch(bodyStr, -1)

	// Scan for script and modulepreload JS files in HTML to scrape modern SPAs
	scriptRegex := regexp.MustCompile(`(?i)(?:src|href)=["']([^"']+\.js)["']`)
	scriptMatches := scriptRegex.FindAllStringSubmatch(bodyStr, -1)

	for _, sMatch := range scriptMatches {
		if len(sMatch) < 2 {
			continue
		}
		rawScript := sMatch[1]
		scriptURL := ResolveRelativeURL(updateURL, rawScript)

		// Only scrape same-origin JS scripts to avoid tracking script bloat
		if isSameOrigin(updateURL, scriptURL) {
			jsResp, err := http.Get(scriptURL)
			if err == nil {
				defer jsResp.Body.Close()
				if jsResp.StatusCode == http.StatusOK {
					// Read up to 3MB of JS bundle
					jsReader := io.LimitReader(jsResp.Body, 3*1024*1024)
					jsBytes, _ := io.ReadAll(jsReader)
					jsStr := string(jsBytes)

					// Extract matches from JS bundle
					jsMatches := hrefRegex.FindAllStringSubmatch(jsStr, -1)
					matches = append(matches, jsMatches...)
				}
			}
		}
	}

	var bestURL string
	var bestVersion string
	var bestFilename string

	for _, match := range matches {
		rawLink := ""
		if len(match) > 1 && match[1] != "" {
			rawLink = match[1]
		} else if len(match) > 2 && match[2] != "" {
			rawLink = match[2]
		}
		if rawLink == "" {
			continue
		}

		// Resolve relative URL to absolute URL
		absoluteURL := ResolveRelativeURL(updateURL, rawLink)
		lowerURL := strings.ToLower(absoluteURL)

		var isPlatform, isArch, isArchive bool

		if runtime.GOOS == "darwin" {
			// macOS: match darwin/macos/osx platform identifiers
			isPlatform = strings.Contains(lowerURL, "darwin") || strings.Contains(lowerURL, "macos") || strings.Contains(lowerURL, "osx")

			// macOS: match architecture identifiers based on host arch
			if runtime.GOARCH == "arm64" {
				isArch = strings.Contains(lowerURL, "arm64") || strings.Contains(lowerURL, "aarch64")
			} else {
				isArch = strings.Contains(lowerURL, "x86_64") || strings.Contains(lowerURL, "amd64") || strings.Contains(lowerURL, "x64")
			}

			// macOS: accept .dmg in addition to common archive extensions
			ext := strings.ToLower(filepath.Ext(lowerURL))
			isArchive = ext == ".zip" || ext == ".gz" || ext == ".xz" || ext == ".bz2" || ext == ".tgz" || ext == ".txz" || ext == ".tbz2" || ext == ".tar" || ext == ".dmg"
		} else {
			// Linux: preserve existing matching logic
			isPlatform = strings.Contains(lowerURL, "linux") || strings.Contains(lowerURL, "ubuntu") || strings.Contains(lowerURL, "debian")
			isArch = strings.Contains(lowerURL, "amd64") || strings.Contains(lowerURL, "x86_64") || strings.Contains(lowerURL, "x64")

			ext := strings.ToLower(filepath.Ext(lowerURL))
			isArchive = ext == ".zip" || ext == ".gz" || ext == ".xz" || ext == ".bz2" || ext == ".tgz" || ext == ".txz" || ext == ".tbz2" || ext == ".tar" || ext == ".appimage"
		}

		if isArchive && (isPlatform || isArch) {
			// Extract version using our DeduceAppDetails (pass absoluteURL to extract versions from URL paths if needed)
			_, ver := DeduceAppDetails(absoluteURL)
			filename := filepath.Base(absoluteURL)

			// Compare versions to find the highest / latest one
			if ver != "" && ver != "1.0.0" {
				if bestVersion == "" || CompareVersions(ver, bestVersion) > 0 {
					bestVersion = ver
					bestURL = absoluteURL
					bestFilename = filename
				}
			} else if bestURL == "" {
				bestURL = absoluteURL
				bestFilename = filename
				bestVersion = "1.0.0"
			}
		}
	}

	if bestURL == "" {
		if runtime.GOOS == "darwin" {
			return nil, fmt.Errorf("could not find any compatible macOS download link on page: %s", updateURL)
		}
		return nil, fmt.Errorf("could not find any compatible Linux download link on page: %s", updateURL)
	}

	PrintSuccess("Scraped latest release version: '%s'", bestVersion)
	PrintInfo("Scraped download link: %s", bestURL)

	// Construct mocked GithubRelease
	return &GithubRelease{
		TagName: "v" + bestVersion,
		HTMLURL: updateURL,
		Assets: []GithubAsset{
			{
				Name:               bestFilename,
				BrowserDownloadURL: bestURL,
			},
		},
	}, nil
}

// ResolveRelativeURL resolves a relative URL reference against a base URL
func ResolveRelativeURL(baseStr, refStr string) string {
	base, err := url.Parse(baseStr)
	if err != nil {
		return refStr
	}
	ref, err := url.Parse(refStr)
	if err != nil {
		return refStr
	}
	return base.ResolveReference(ref).String()
}

// parsePreRelease extracts the pre-release type and numeric suffix from a
// pre-release string. For example, "rc1" returns ("rc", 1) and "alpha" returns ("alpha", 0).
func parsePreRelease(suffix string) (string, int) {
	for _, t := range []string{"alpha", "beta", "rc"} {
		if strings.HasPrefix(suffix, t) {
			numStr := suffix[len(t):]
			if numStr == "" {
				return t, 0
			}
			var n int
			fmt.Sscanf(numStr, "%d", &n)
			return t, n
		}
	}
	return suffix, 0
}

// preReleaseRank maps a pre-release type to a numeric rank for comparison.
// alpha=1, beta=2, rc=3, unknown=0.
func preReleaseRank(preType string) int {
	switch preType {
	case "alpha":
		return 1
	case "beta":
		return 2
	case "rc":
		return 3
	default:
		return 0
	}
}

// CompareVersions returns 1 if v1 > v2, -1 if v1 < v2, and 0 if v1 == v2
func CompareVersions(v1, v2 string) int {
	v1 = strings.TrimPrefix(strings.ToLower(v1), "v")
	v2 = strings.TrimPrefix(strings.ToLower(v2), "v")

	// Split pre-release suffix from the version string.
	// The pre-release suffix is attached to the last segment after a hyphen.
	var pre1, pre2 string
	if idx := strings.Index(v1, "-"); idx >= 0 {
		pre1 = v1[idx+1:]
		v1 = v1[:idx]
	}
	if idx := strings.Index(v2, "-"); idx >= 0 {
		pre2 = v2[idx+1:]
		v2 = v2[:idx]
	}

	// Compare base version segments numerically (existing logic preserved)
	p1 := strings.Split(v1, ".")
	p2 := strings.Split(v2, ".")

	maxLen := len(p1)
	if len(p2) > maxLen {
		maxLen = len(p2)
	}

	for i := 0; i < maxLen; i++ {
		var n1, n2 int
		if i < len(p1) {
			fmt.Sscanf(p1[i], "%d", &n1)
		}
		if i < len(p2) {
			fmt.Sscanf(p2[i], "%d", &n2)
		}

		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}

	// Base versions are equal, compare pre-release metadata
	if pre1 == "" && pre2 == "" {
		return 0 // both stable
	}
	if pre1 == "" && pre2 != "" {
		return 1 // stable > pre-release
	}
	if pre1 != "" && pre2 == "" {
		return -1 // pre-release < stable
	}

	// Both have pre-release, compare type rank, then numeric suffix
	type1, num1 := parsePreRelease(pre1)
	type2, num2 := parsePreRelease(pre2)

	rank1 := preReleaseRank(type1)
	rank2 := preReleaseRank(type2)

	if rank1 > rank2 {
		return 1
	}
	if rank1 < rank2 {
		return -1
	}
	if num1 > num2 {
		return 1
	}
	if num1 < num2 {
		return -1
	}
	return 0
}

// isSameOrigin checks if two URLs share the same origin (host)
func isSameOrigin(baseStr, targetStr string) bool {
	base, err := url.Parse(baseStr)
	if err != nil {
		return false
	}
	target, err := url.Parse(targetStr)
	if err != nil {
		return false
	}
	return base.Host == target.Host
}

// PluginOutput represents the structured JSON output option of a source checking plugin
type PluginOutput struct {
	Version string `json:"version"`
	URL     string `json:"url"`
}

// RunSourceCheckingPlugin executes a registered external script or binary, parsing its version & download URL
func RunSourceCheckingPlugin(scriptPath string) (*GithubRelease, error) {
	scriptPath = ExpandTilde(scriptPath)

	if !FileExists(scriptPath) {
		return nil, fmt.Errorf("plugin script does not exist at path: %s", scriptPath)
	}

	// Execute external command
	cmd := exec.Command(scriptPath)
	outputBytes, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute plugin script: %v", err)
	}

	outputStr := strings.TrimSpace(string(outputBytes))
	if outputStr == "" {
		return nil, fmt.Errorf("plugin script returned empty output")
	}

	var version string
	var downloadURL string

	// Try to parse as JSON first
	if strings.HasPrefix(outputStr, "{") {
		var pOut PluginOutput
		jsonErr := json.Unmarshal(outputBytes, &pOut)
		if jsonErr == nil && pOut.Version != "" && pOut.URL != "" {
			version = pOut.Version
			downloadURL = pOut.URL
		}
	}

	// Fallback to parsing two non-empty lines (Line 1: Version, Line 2: Download URL)
	if version == "" || downloadURL == "" {
		lines := strings.Split(outputStr, "\n")
		var nonLocLines []string
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				nonLocLines = append(nonLocLines, trimmed)
			}
		}

		if len(nonLocLines) < 2 {
			return nil, fmt.Errorf("plugin output must be JSON or contain at least two non-empty lines (Line 1: Version, Line 2: Download URL)")
		}

		version = nonLocLines[0]
		downloadURL = nonLocLines[1]
	}

	version = strings.TrimPrefix(strings.ToLower(version), "v")
	filename := filepath.Base(downloadURL)

	PrintSuccess("Plugin successfully returned version: '%s'", version)
	PrintInfo("Plugin returned download link: %s", downloadURL)

	// Construct mocked GithubRelease
	return &GithubRelease{
		TagName: "v" + version,
		HTMLURL: scriptPath,
		Assets: []GithubAsset{
			{
				Name:               filename,
				BrowserDownloadURL: downloadURL,
			},
		},
	}, nil
}
