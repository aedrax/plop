package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
)

// GitHubURLType classifies a URL for routing in the install flow
type GitHubURLType int

const (
	GitHubURLRepo        GitHubURLType = iota // https://github.com/owner/repo
	GitHubURLDirect                           // https://github.com/.../releases/download/...
	GitHubURLUnsupported                      // https://github.com/owner/repo/tree/...
	NotGitHub                                 // Any non-GitHub URL
)

// unsupportedSegments lists path segments that indicate an unsupported GitHub URL
var unsupportedSegments = []string{
	"/tree/",
	"/wiki/",
	"/issues/",
	"/pull/",
	"/pulls/",
	"/actions/",
	"/discussions/",
	"/commits/",
	"/commit/",
	"/blob/",
	"/branches/",
	"/tags/",
	"/settings/",
	"/projects/",
	"/security/",
	"/network/",
	"/stargazers/",
	"/watchers/",
	"/graphs/",
	"/compare/",
}

// ClassifyGitHubURL determines the type of a GitHub URL for routing decisions
func ClassifyGitHubURL(input string) GitHubURLType {
	// Case-insensitive prefix check
	if !hasGitHubPrefix(input) {
		return NotGitHub
	}

	// Extract the path after github.com/
	path := extractPath(input)
	if path == "" {
		return NotGitHub
	}

	// Check for direct download paths (releases/ or archive/)
	pathLower := strings.ToLower(path)
	segments := strings.Split(strings.TrimSuffix(pathLower, "/"), "/")

	// Need at least owner/repo
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return NotGitHub
	}

	// Check for path segments beyond owner/repo
	if len(segments) > 2 {
		// Check for direct download indicators
		if segments[2] == "releases" || segments[2] == "archive" {
			return GitHubURLDirect
		}

		// Check for unsupported segments
		for _, seg := range unsupportedSegments {
			if strings.Contains("/"+strings.Join(segments[2:], "/")+"/", seg) {
				return GitHubURLUnsupported
			}
		}

		// Any other extra path segment is unsupported
		return GitHubURLUnsupported
	}

	return GitHubURLRepo
}

// IsGitHubRepoURL returns true if the input is a GitHub repository URL
// (not a direct download or other path)
func IsGitHubRepoURL(input string) bool {
	return ClassifyGitHubURL(input) == GitHubURLRepo
}

// IsUnsupportedGitHubURL returns true if the URL is a GitHub URL with
// unsupported path segments beyond {owner}/{repo}
func IsUnsupportedGitHubURL(input string) bool {
	return ClassifyGitHubURL(input) == GitHubURLUnsupported
}

// ExtractOwnerRepo parses a validated GitHub repository URL and extracts
// the owner and repo components. Strips trailing `/` and `.git` suffix.
func ExtractOwnerRepo(url string) (owner, repo string, err error) {
	if !hasGitHubPrefix(url) {
		return "", "", fmt.Errorf("not a GitHub URL: %s", url)
	}

	path := extractPath(url)
	if path == "" {
		return "", "", fmt.Errorf("invalid GitHub URL: %s", url)
	}

	// Strip trailing slash
	path = strings.TrimSuffix(path, "/")

	// Strip .git suffix
	path = strings.TrimSuffix(path, ".git")
	path = strings.TrimSuffix(path, ".GIT")

	// Split into segments
	segments := strings.Split(path, "/")
	if len(segments) < 2 || segments[0] == "" || segments[1] == "" {
		return "", "", fmt.Errorf("could not extract owner/repo from URL: %s", url)
	}

	return segments[0], segments[1], nil
}

// hasGitHubPrefix checks if the input starts with https://github.com/ (case-insensitive)
func hasGitHubPrefix(input string) bool {
	const prefix = "https://github.com/"
	if len(input) < len(prefix) {
		return false
	}
	return strings.EqualFold(input[:len(prefix)], prefix)
}

// extractPath returns the path portion after https://github.com/
func extractPath(input string) string {
	const prefixLen = len("https://github.com/")
	if len(input) <= prefixLen {
		return ""
	}
	return input[prefixLen:]
}

// githubAPIBaseURL is the base URL for the GitHub API. It can be overridden in tests
// to point to a httptest server.
var githubAPIBaseURL = "https://api.github.com"

// FetchReleaseList queries the GitHub Releases API for the given owner/repo and returns
// all releases that have at least one asset. If token is non-empty, it is sent as a
// Bearer authorization header. Returns a descriptive error for rate limits, 404s, and
// other non-200 responses.
func FetchReleaseList(owner, repo, token string) ([]GithubRelease, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/releases", githubAPIBaseURL, owner, repo)

	client := &http.Client{}
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "plop")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return nil, fmt.Errorf("GitHub API rate limit exceeded. Please configure a github_token in ~/.config/plop/config.json")
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("no releases found for repository %s/%s", owner, repo)
		}
		return nil, fmt.Errorf("GitHub API returned status: %d", resp.StatusCode)
	}

	var releases []GithubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("failed to decode GitHub API response: %v", err)
	}

	filtered := FilterReleasesWithAssets(releases)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no installable releases found for repository %s/%s", owner, repo)
	}

	return filtered, nil
}

// FilterReleasesWithAssets returns only releases that have at least one asset.
func FilterReleasesWithAssets(releases []GithubRelease) []GithubRelease {
	var result []GithubRelease
	for _, r := range releases {
		if len(r.Assets) > 0 {
			result = append(result, r)
		}
	}
	return result
}

// FormatReleaseList formats a numbered list of releases for display.
// Shows at most max entries, newest first (input is pre-sorted by publication date).
// Format: "  [1] v1.0.0\n  [2] v0.9.0\n..."
func FormatReleaseList(releases []GithubRelease, max int) string {
	count := len(releases)
	if max > 0 && count > max {
		count = max
	}
	var sb strings.Builder
	for i := 0; i < count; i++ {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("  [%d] %s", i+1, releases[i].TagName))
	}
	return sb.String()
}

// stripVPrefix removes a single leading 'v' or 'V' from a string for comparison.
func stripVPrefix(s string) string {
	if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
		return s[1:]
	}
	return s
}

// ExtractVersion produces a version string from a release tag.
// If versionFlag is non-empty, it is returned as-is.
// Otherwise, a single leading 'v' or 'V' is stripped from tag.
func ExtractVersion(tag string, versionFlag string) string {
	if versionFlag != "" {
		return versionFlag
	}
	return stripVPrefix(tag)
}

// SelectAsset handles asset selection:
// - Uses existing FindBestAsset to identify platform-compatible asset
// - If found and autoConfirm, proceeds silently
// - If found and interactive, shows selection and prompts for confirmation or alternative
// - If not found and autoConfirm, returns error
// - If not found and interactive, presents all assets for manual selection
func SelectAsset(release *GithubRelease, autoConfirm bool) (*GithubAsset, error) {
	if len(release.Assets) == 0 {
		return nil, fmt.Errorf("no assets available in this release")
	}

	// Use FindBestAsset to identify platform-compatible asset
	bestURL, bestName := FindBestAsset(release)

	// Find the matching asset struct
	var bestAsset *GithubAsset
	if bestURL != "" && bestName != "" {
		for i := range release.Assets {
			if release.Assets[i].BrowserDownloadURL == bestURL {
				bestAsset = &release.Assets[i]
				break
			}
		}
	}

	// Determine if FindBestAsset actually found a platform-specific match
	// FindBestAsset falls back to the first asset when no platform match is found,
	// so we need to check if it genuinely matched the platform
	genuineMatch := bestAsset != nil && isPlatformMatch(bestAsset.Name)

	if genuineMatch {
		// Compatible asset found
		if autoConfirm {
			return bestAsset, nil
		}

		// Interactive: show selection and prompt for confirmation or alternative
		fmt.Printf("\nSelected: %s\n", bestAsset.Name)
		fmt.Println("Press Enter to confirm, or enter a number to choose a different asset:")
		for i, asset := range release.Assets {
			fmt.Printf("  [%d] %s\n", i+1, asset.Name)
		}
		fmt.Println()

		reader := bufio.NewReader(os.Stdin)
		for {
			fmt.Print("Selection [Enter to confirm]: ")
			input, err := reader.ReadString('\n')
			if err != nil {
				return bestAsset, nil
			}
			input = strings.TrimSpace(input)

			// If user presses Enter, use the auto-selected asset
			if input == "" {
				return bestAsset, nil
			}

			// If user enters a number, use that asset
			var idx int
			_, parseErr := fmt.Sscanf(input, "%d", &idx)
			if parseErr != nil || idx < 1 || idx > len(release.Assets) {
				PrintWarning("Invalid selection. Please enter a number between 1 and %d, or press Enter to confirm.", len(release.Assets))
				continue
			}

			return &release.Assets[idx-1], nil
		}
	}

	// No compatible asset found
	if autoConfirm {
		return nil, fmt.Errorf("no compatible asset found for current platform")
	}

	// Interactive: present all assets for manual selection
	fmt.Println("\nNo compatible asset found for your platform. Available assets:")
	for i, asset := range release.Assets {
		fmt.Printf("  [%d] %s\n", i+1, asset.Name)
	}
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Select asset: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read input")
		}
		input = strings.TrimSpace(input)

		var idx int
		_, parseErr := fmt.Sscanf(input, "%d", &idx)
		if parseErr != nil || idx < 1 || idx > len(release.Assets) {
			PrintWarning("Invalid selection. Please enter a number between 1 and %d.", len(release.Assets))
			continue
		}

		return &release.Assets[idx-1], nil
	}
}

// ConstructUpdateURL builds the canonical GitHub repository URL from owner and repo.
// This is used as the update_url for future update checks.
func ConstructUpdateURL(owner, repo string) string {
	return fmt.Sprintf("https://github.com/%s/%s", owner, repo)
}

// ResolveGitHubInstall orchestrates the full GitHub release install flow:
// classify URL -> extract owner/repo -> fetch releases -> select release -> select asset -> build options.
// It returns the resolved download URL and modified InstallOptions ready for InstallApp.
func ResolveGitHubInstall(input string, opts InstallOptions, config *Config) (downloadURL string, resolvedOpts InstallOptions, err error) {
	// 1. Extract owner and repo from the GitHub URL
	owner, repo, err := ExtractOwnerRepo(input)
	if err != nil {
		return "", opts, err
	}

	// 2. Fetch the list of releases from the GitHub API
	releases, err := FetchReleaseList(owner, repo, config.GithubToken)
	if err != nil {
		return "", opts, err
	}

	// 3. Select a release (respects --version flag and auto_confirm)
	release, err := SelectRelease(releases, opts.ForcedVersion, config.AutoConfirm)
	if err != nil {
		return "", opts, err
	}

	// 4. Select an asset from the chosen release
	asset, err := SelectAsset(release, config.AutoConfirm)
	if err != nil {
		return "", opts, err
	}

	// 5. Build resolved options
	resolvedOpts = opts
	resolvedOpts.ForcedVersion = ExtractVersion(release.TagName, opts.ForcedVersion)

	// 6. Set ForcedSource to the GitHub repo URL unless --source was explicitly provided
	if opts.ForcedSource == "" {
		resolvedOpts.ForcedSource = ConstructUpdateURL(owner, repo)
	}

	// 7. ForcedName and ForcedBinaryName pass through unchanged (already in opts)

	return asset.BrowserDownloadURL, resolvedOpts, nil
}

// isPlatformMatch checks if an asset name contains platform-specific identifiers
// for the current OS. This is used to distinguish a genuine FindBestAsset match
// from its fallback behavior of returning the first asset.
func isPlatformMatch(name string) bool {
	lower := strings.ToLower(name)
	if runtime.GOOS == "darwin" {
		return strings.Contains(lower, "darwin") ||
			strings.Contains(lower, "macos") ||
			strings.Contains(lower, "osx")
	}
	return strings.Contains(lower, "linux") ||
		strings.Contains(lower, "ubuntu") ||
		strings.Contains(lower, "debian") ||
		strings.HasSuffix(lower, ".appimage") ||
		strings.HasSuffix(lower, ".run")
}

// SelectRelease handles release selection logic:
// - If versionFlag is set, finds matching release by tag (with v-prefix normalization)
// - If autoConfirm or only one release, selects latest automatically
// - Otherwise, presents numbered list (max 20, newest first) and prompts user
func SelectRelease(releases []GithubRelease, versionFlag string, autoConfirm bool) (*GithubRelease, error) {
	if len(releases) == 0 {
		return nil, fmt.Errorf("no releases available")
	}

	// If --version flag is set, find matching release by tag with v-prefix normalization
	if versionFlag != "" {
		normalizedFlag := stripVPrefix(versionFlag)
		for i := range releases {
			normalizedTag := stripVPrefix(releases[i].TagName)
			if strings.EqualFold(normalizedFlag, normalizedTag) {
				return &releases[i], nil
			}
		}
		// No match found return error with available tags
		count := len(releases)
		if count > 20 {
			count = 20
		}
		var tags []string
		for i := 0; i < count; i++ {
			tags = append(tags, releases[i].TagName)
		}
		return nil, fmt.Errorf("no release found matching version '%s'. Available tags:\n%s", versionFlag, strings.Join(tags, "\n"))
	}

	// If auto_confirm or only one release, select latest automatically
	if autoConfirm || len(releases) == 1 {
		return &releases[0], nil
	}

	// Present numbered list (max 20, newest first) and prompt user
	fmt.Println("\nAvailable releases:")
	fmt.Println(FormatReleaseList(releases, 20))
	fmt.Println()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Select release [1]: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			// On read error, default to first release
			return &releases[0], nil
		}
		input = strings.TrimSpace(input)

		// If user presses Enter with no input, select default (1)
		if input == "" {
			return &releases[0], nil
		}

		var idx int
		_, parseErr := fmt.Sscanf(input, "%d", &idx)
		displayCount := len(releases)
		if displayCount > 20 {
			displayCount = 20
		}
		if parseErr != nil || idx < 1 || idx > displayCount {
			PrintWarning("Invalid selection. Please enter a number between 1 and %d.", displayCount)
			continue
		}

		return &releases[idx-1], nil
	}
}
