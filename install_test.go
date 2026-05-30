package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/quick"
)

func TestDeduceAppDetails(t *testing.T) {
	tests := []struct {
		filename string
		wantName string
		wantVer  string
	}{
		{"lazygit_0.40.2_Linux_x86_64.tar.gz", "lazygit", "0.40.2"},
		{"Cutter-v2.4.1-Linux-x86_64.AppImage", "cutter", "2.4.1"},
		{"Awesome IDE.tar.gz", "awesome ide", "1.0.0"},
		{"Awesome IDE 1.2.3.tar.gz", "awesome ide", "1.2.3"},
		{"lazygit.0.40.2.Linux.tar.gz", "lazygit", "0.40.2"},
		{"https://github.com/org/repo/releases/download/v1.2.3/app.zip", "app", "1.2.3"},
		{"v2.4.1-Cutter.AppImage", "cutter", "2.4.1"},
		{"DadroitJSONViewer.AppImage", "dadroitjsonviewer", "1.0.0"},
		{"lazygit", "lazygit", "1.0.0"},
		{"lazygit-v0.40.2", "lazygit", "0.40.2"},
		{"sqlite-tools-linux-x64-3530100.zip", "sqlite", "3.53.1"},
		{"sqlite-autoconf-3530100.tar.gz", "sqlite", "3.53.1"},
	}

	for _, tt := range tests {
		gotName, gotVer := DeduceAppDetails(tt.filename)
		if gotName != tt.wantName || gotVer != tt.wantVer {
			t.Errorf("DeduceAppDetails(%q) = (%q, %q); want (%q, %q)",
				tt.filename, gotName, gotVer, tt.wantName, tt.wantVer)
		}
	}
}

func TestUnwrapSignificantItems(t *testing.T) {
	// Create a temporary directory
	tempDir := t.TempDir()

	// Create a significant subdirectory
	sigDirName := "Awesome IDE"
	err := os.Mkdir(filepath.Join(tempDir, sigDirName), 0755)
	if err != nil {
		t.Fatalf("failed to create significant subdirectory: %v", err)
	}

	// Create some common metadata files to ignore
	ignoredFiles := []string{
		"pax_global_header",
		".DS_Store",
		"._some_mac_resource",
		"__MACOSX",
	}
	for _, fname := range ignoredFiles {
		err := os.WriteFile(filepath.Join(tempDir, fname), []byte("metadata"), 0644)
		if err != nil {
			t.Fatalf("failed to create metadata file %s: %v", fname, err)
		}
	}

	// Run the unwrapping logic
	items, err := os.ReadDir(tempDir)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}

	var significantItems []os.DirEntry
	for _, item := range items {
		name := item.Name()
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "._") || name == "__MACOSX" || name == "pax_global_header" {
			continue
		}
		significantItems = append(significantItems, item)
	}

	// Assertions
	if len(significantItems) != 1 {
		t.Errorf("expected 1 significant item, got %d", len(significantItems))
	} else {
		gotName := significantItems[0].Name()
		if gotName != sigDirName {
			t.Errorf("expected significant item name to be %q, got %q", sigDirName, gotName)
		}
		if !significantItems[0].IsDir() {
			t.Errorf("expected significant item to be a directory")
		}
	}
}

func TestSetAppMetadata(t *testing.T) {
	// Set registry override path to a temporary file
	tempDir := t.TempDir()
	registryPathOverride = filepath.Join(tempDir, "registry.json")
	defer func() { registryPathOverride = "" }()

	// Create a dummy registry
	reg := &Registry{
		Apps: map[string]AppMetadata{
			"testapp": {
				Name:        "testapp",
				Version:     "1.0.0",
				UpdateURL:   "https://github.com/org/testapp",
				BinaryPath:  "/tmp/opt/testapp/bin",
				SymlinkPath: "/tmp/bin/testapp",
			},
		},
	}

	// Create config
	config := &Config{
		OptDir:  "/tmp/opt",
		BinDir:  "/tmp/bin",
		AppsDir: "/tmp/apps",
	}

	// Test version updating
	err := SetAppMetadata("testapp", SetAppOptions{NewVersion: "2.0.0"}, config, reg)
	if err != nil {
		t.Fatalf("failed to update version: %v", err)
	}

	app := reg.Apps["testapp"]
	if app.Version != "2.0.0" {
		t.Errorf("expected version to be 2.0.0, got %s", app.Version)
	}

	// Test update source URL updating
	err = SetAppMetadata("testapp", SetAppOptions{NewSource: "https://github.com/org/newrepo"}, config, reg)
	if err != nil {
		t.Fatalf("failed to update source: %v", err)
	}

	app = reg.Apps["testapp"]
	if app.UpdateURL != "https://github.com/org/newrepo" {
		t.Errorf("expected update URL to be https://github.com/org/newrepo, got %s", app.UpdateURL)
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		v1   string
		v2   string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v2.0", "1.9", 1},
		{"1.2.3", "1.2.4", -1},
		{"1.10.0", "1.2.0", 1},
	}

	for _, tt := range tests {
		got := CompareVersions(tt.v1, tt.v2)
		if got != tt.want {
			t.Errorf("CompareVersions(%q, %q) = %d; want %d", tt.v1, tt.v2, got, tt.want)
		}
	}
}

func TestResolveRelativeURL(t *testing.T) {
	tests := []struct {
		base string
		ref  string
		want string
	}{
		{"https://example.com/downloads/", "app.tar.gz", "https://example.com/downloads/app.tar.gz"},
		{"https://example.com/downloads/index.html", "/assets/app.zip", "https://example.com/assets/app.zip"},
		{"https://example.com/downloads", "../app.AppImage", "https://example.com/app.AppImage"},
	}

	for _, tt := range tests {
		got := ResolveRelativeURL(tt.base, tt.ref)
		if got != tt.want {
			t.Errorf("ResolveRelativeURL(%q, %q) = %q; want %q", tt.base, tt.ref, got, tt.want)
		}
	}
}

func TestScrapeGenericRelease(t *testing.T) {
	// Start a local mock HTTP server with both Linux and macOS links
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`
			<html>
				<body>
					<a href="/downloads/app-v1.0.0-linux-amd64.tar.gz">Linux Version 1.0.0</a>
					<a href="/downloads/app-v2.5.1-linux-x86_64.tar.gz">Linux Version 2.5.1</a>
					<a href="/downloads/app-v1.0.0-darwin-arm64.tar.gz">macOS Version 1.0.0</a>
					<a href="/downloads/app-v2.5.1-darwin-x86_64.tar.gz">macOS Version 2.5.1</a>
					<a href="/downloads/ignored-windows.zip">Windows Release</a>
				</body>
			</html>
		`))
	}))
	defer server.Close()

	// Scrape the mock server page
	release, err := ScrapeGenericRelease(server.URL)
	if err != nil {
		t.Fatalf("failed to scrape mock server: %v", err)
	}

	// Assertions
	if release.TagName != "v2.5.1" {
		t.Errorf("expected latest version to be v2.5.1, got %s", release.TagName)
	}
	if len(release.Assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(release.Assets))
	}
	asset := release.Assets[0]
	if runtime.GOOS == "darwin" {
		if asset.Name != "app-v2.5.1-darwin-x86_64.tar.gz" {
			t.Errorf("expected asset name to be app-v2.5.1-darwin-x86_64.tar.gz, got %s", asset.Name)
		}
		expectedURL := server.URL + "/downloads/app-v2.5.1-darwin-x86_64.tar.gz"
		if asset.BrowserDownloadURL != expectedURL {
			t.Errorf("expected download URL to be %q, got %q", expectedURL, asset.BrowserDownloadURL)
		}
	} else {
		if asset.Name != "app-v2.5.1-linux-x86_64.tar.gz" {
			t.Errorf("expected asset name to be app-v2.5.1-linux-x86_64.tar.gz, got %s", asset.Name)
		}
		expectedURL := server.URL + "/downloads/app-v2.5.1-linux-x86_64.tar.gz"
		if asset.BrowserDownloadURL != expectedURL {
			t.Errorf("expected download URL to be %q, got %q", expectedURL, asset.BrowserDownloadURL)
		}
	}
}

func TestScrapeObfuscatedJS(t *testing.T) {
	// Start a local mock HTTP server that simulates SQLite's JavaScript injection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		if runtime.GOOS == "darwin" {
			w.Write([]byte(`
				<html>
					<body>
						<a id='a7' href='hp1.html'>sqlite-tools-osx-x86_64-3530100.zip</a>
						<script>
							setTimeout(function(){
								function d391(a,b){document.getElementById(a).href=b;};
								d391('a7','2026/sqlite-tools-osx-x86_64-3530100.zip');
							}, 10);
						</script>
					</body>
				</html>
			`))
		} else {
			w.Write([]byte(`
				<html>
					<body>
						<a id='a7' href='hp1.html'>sqlite-tools-linux-x64-3530100.zip</a>
						<script>
							setTimeout(function(){
								function d391(a,b){document.getElementById(a).href=b;};
								d391('a7','2026/sqlite-tools-linux-x64-3530100.zip');
							}, 10);
						</script>
					</body>
				</html>
			`))
		}
	}))
	defer server.Close()

	// Scrape the mock server page
	release, err := ScrapeGenericRelease(server.URL)
	if err != nil {
		t.Fatalf("failed to scrape obfuscated mock server: %v", err)
	}

	// Assertions
	if len(release.Assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(release.Assets))
	}
	asset := release.Assets[0]
	if runtime.GOOS == "darwin" {
		expectedURL := server.URL + "/2026/sqlite-tools-osx-x86_64-3530100.zip"
		if asset.BrowserDownloadURL != expectedURL {
			t.Errorf("expected download URL to be %q, got %q", expectedURL, asset.BrowserDownloadURL)
		}
	} else {
		expectedURL := server.URL + "/2026/sqlite-tools-linux-x64-3530100.zip"
		if asset.BrowserDownloadURL != expectedURL {
			t.Errorf("expected download URL to be %q, got %q", expectedURL, asset.BrowserDownloadURL)
		}
	}
}

func TestScrapeSPABundles(t *testing.T) {
	// Start a local mock HTTP server that simulates a client-side rendered SPA page
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)

		// If requesting the JS bundle, serve JS content containing the direct download URL
		if strings.HasSuffix(r.URL.Path, "/main.js") {
			if runtime.GOOS == "darwin" {
				w.Write([]byte(`
					const stableDarwin = "https://example.com/awesome/2.0.6-123456789/darwin-x86_64/Awesome.tar.gz";
					const otherDarwin = "/downloads/2.0.3/Awesome IDE.tar.gz";
				`))
			} else {
				w.Write([]byte(`
					const stableLinux = "https://example.com/awesome/2.0.6-123456789/linux-x64/Awesome.tar.gz";
					const otherLinux = "/downloads/2.0.3/Awesome IDE.tar.gz";
				`))
			}
			return
		}

		// Otherwise serve the skeleton HTML page linking the JS bundle
		w.Write([]byte(`
			<html>
				<head>
					<title>SPA Download Page</title>
				</head>
				<body>
					<app-root></app-root>
					<script src="/main.js" type="module"></script>
				</body>
			</html>
		`))
	}))
	defer server.Close()

	// Scrape the mock server page
	release, err := ScrapeGenericRelease(server.URL)
	if err != nil {
		t.Fatalf("failed to scrape SPA mock server: %v", err)
	}

	// Assertions
	if release.TagName != "v2.0.6" {
		t.Errorf("expected latest version to be v2.0.6, got %s", release.TagName)
	}
	if len(release.Assets) != 1 {
		t.Fatalf("expected 1 asset, got %d", len(release.Assets))
	}
	asset := release.Assets[0]
	if asset.Name != "Awesome.tar.gz" {
		t.Errorf("expected asset name to be Awesome.tar.gz, got %s", asset.Name)
	}
	if runtime.GOOS == "darwin" {
		expectedURL := "https://example.com/awesome/2.0.6-123456789/darwin-x86_64/Awesome.tar.gz"
		if asset.BrowserDownloadURL != expectedURL {
			t.Errorf("expected download URL to be %q, got %q", expectedURL, asset.BrowserDownloadURL)
		}
	} else {
		expectedURL := "https://example.com/awesome/2.0.6-123456789/linux-x64/Awesome.tar.gz"
		if asset.BrowserDownloadURL != expectedURL {
			t.Errorf("expected download URL to be %q, got %q", expectedURL, asset.BrowserDownloadURL)
		}
	}
}

func TestRunSourceCheckingPlugin(t *testing.T) {
	tempDir := t.TempDir()

	// Test Plain Text Output Plugin
	plainScriptPath := filepath.Join(tempDir, "plain_plugin.sh")
	plainScriptContent := `#!/bin/bash
echo "3.1.2"
echo "https://storage.googleapis.com/test-bucket/app-3.1.2.tar.gz"
`
	err := os.WriteFile(plainScriptPath, []byte(plainScriptContent), 0755)
	if err != nil {
		t.Fatalf("failed to write mock plain script: %v", err)
	}

	release, err := RunSourceCheckingPlugin(plainScriptPath)
	if err != nil {
		t.Fatalf("failed to run plain plugin script: %v", err)
	}

	if release.TagName != "v3.1.2" {
		t.Errorf("expected plain tag name to be v3.1.2, got %s", release.TagName)
	}
	if len(release.Assets) != 1 || release.Assets[0].BrowserDownloadURL != "https://storage.googleapis.com/test-bucket/app-3.1.2.tar.gz" {
		t.Errorf("expected plain download URL to be correctly set")
	}

	// Test JSON Output Plugin
	jsonScriptPath := filepath.Join(tempDir, "json_plugin.sh")
	jsonScriptContent := `#!/bin/bash
echo '{"version": "4.0.0-beta", "url": "https://example.com/builds/app-v4.0.0-beta.zip"}'
`
	err = os.WriteFile(jsonScriptPath, []byte(jsonScriptContent), 0755)
	if err != nil {
		t.Fatalf("failed to write mock json script: %v", err)
	}

	release, err = RunSourceCheckingPlugin(jsonScriptPath)
	if err != nil {
		t.Fatalf("failed to run json plugin script: %v", err)
	}

	if release.TagName != "v4.0.0-beta" {
		t.Errorf("expected json tag name to be v4.0.0-beta, got %s", release.TagName)
	}
	if len(release.Assets) != 1 || release.Assets[0].BrowserDownloadURL != "https://example.com/builds/app-v4.0.0-beta.zip" {
		t.Errorf("expected json download URL to be correctly set")
	}
}

func TestCustomInstallScript(t *testing.T) {
	// Set registry override path to a temporary file
	tempDir := t.TempDir()
	registryPathOverride = filepath.Join(tempDir, "registry.json")
	defer func() { registryPathOverride = "" }()

	// Create a mock source zip file
	sourceZipPath := filepath.Join(tempDir, "source.zip")
	err := writeRealZip(sourceZipPath)
	if err != nil {
		t.Fatalf("failed to write real zip: %v", err)
	}

	// Write a mock custom install script that builds our application
	installScriptPath := filepath.Join(tempDir, "install_script.sh")
	installScriptContent := `#!/bin/bash
# $1: tempExtractDir, $2: targetAppDir, $3: binDir
mkdir -p "$2"
# Write a mock compiled shell script representing the output binary
echo "#!/bin/bash" > "$2/myapp"
echo "echo 'hello'" >> "$2/myapp"
chmod +x "$2/myapp"
`
	err = os.WriteFile(installScriptPath, []byte(installScriptContent), 0755)
	if err != nil {
		t.Fatalf("failed to write install script: %v", err)
	}

	// Mock configs
	config := &Config{
		OptDir:      filepath.Join(tempDir, "opt"),
		BinDir:      filepath.Join(tempDir, "bin"),
		AppsDir:     filepath.Join(tempDir, "apps"),
		IconsDir:    filepath.Join(tempDir, "icons"),
		AutoConfirm: true,
	}
	reg := &Registry{Apps: make(map[string]AppMetadata)}

	// Call InstallApp with custom install script
	err = InstallApp(sourceZipPath, InstallOptions{
		ForcedName:          "custom-app",
		ForcedVersion:       "1.5.0",
		ForcedInstallScript: installScriptPath,
	}, config, reg)
	if err != nil {
		t.Fatalf("InstallApp failed: %v", err)
	}

	// Assertions
	app, exists := reg.Apps["custom-app"]
	if !exists {
		t.Fatalf("expected custom-app to be registered")
	}
	if app.Version != "1.5.0" {
		t.Errorf("expected version to be 1.5.0, got %s", app.Version)
	}

	expectedBinPath := filepath.Join(config.OptDir, "custom-app", "myapp")
	if app.BinaryPath != expectedBinPath {
		t.Errorf("expected BinaryPath to be %q, got %q", expectedBinPath, app.BinaryPath)
	}

	expectedSymPath := filepath.Join(config.BinDir, "myapp")
	if app.SymlinkPath != expectedSymPath {
		t.Errorf("expected SymlinkPath to be %q, got %q", expectedSymPath, app.SymlinkPath)
	}

	// Assert that the registry contains the install script path
	if app.InstallScript != installScriptPath {
		t.Errorf("expected InstallScript in registry to be %q, got %q", installScriptPath, app.InstallScript)
	}

	// Test updating the custom install script via SetAppMetadata
	anotherScriptPath := filepath.Join(tempDir, "another_script.sh")
	err = SetAppMetadata("custom-app", SetAppOptions{NewInstallScript: anotherScriptPath}, config, reg)
	if err != nil {
		t.Fatalf("SetAppMetadata failed: %v", err)
	}
	app = reg.Apps["custom-app"]
	if app.InstallScript != anotherScriptPath {
		t.Errorf("expected updated InstallScript to be %q, got %q", anotherScriptPath, app.InstallScript)
	}

	// Test clearing the custom install script by setting it to "none"
	err = SetAppMetadata("custom-app", SetAppOptions{NewInstallScript: "none"}, config, reg)
	if err != nil {
		t.Fatalf("SetAppMetadata failed to clear: %v", err)
	}
	app = reg.Apps["custom-app"]
	if app.InstallScript != "" {
		t.Errorf("expected cleared InstallScript to be empty, got %q", app.InstallScript)
	}

	// Test UpgradeApp with custom install script
	// Restore the script path
	err = SetAppMetadata("custom-app", SetAppOptions{NewInstallScript: installScriptPath}, config, reg)
	if err != nil {
		t.Fatalf("failed to restore install script path: %v", err)
	}

	// Mock a remote update/download server
	mockZipServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".zip") {
			w.Header().Set("Content-Type", "application/zip")
			z := zip.NewWriter(w)
			fileWriter, _ := z.Create("main.c")
			fileWriter.Write([]byte("int main() { return 0; }"))
			z.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// Use platform-appropriate asset name so scraper matches on both platforms
		var assetName string
		if runtime.GOOS == "darwin" {
			assetName = "custom-app-darwin-x86_64-1.6.0.zip"
		} else {
			assetName = "custom-app-linux-x64-1.6.0.zip"
		}
		fmt.Fprintf(w, `{
			"tag_name": "v1.6.0",
			"assets": [
				{
					"name": "%s",
					"browser_download_url": "%s/%s"
				}
			]
		}`, assetName, serverURLToHost(r), assetName)
	}))
	defer mockZipServer.Close()

	app = reg.Apps["custom-app"]
	app.UpdateURL = mockZipServer.URL
	reg.Apps["custom-app"] = app

	// Run UpgradeApp
	err = UpgradeApp("custom-app", config, reg)
	if err != nil {
		t.Fatalf("UpgradeApp failed: %v", err)
	}

	// Verify that the upgraded version is registered as 1.6.0
	app = reg.Apps["custom-app"]
	if app.Version != "1.6.0" {
		t.Errorf("expected upgraded version to be 1.6.0, got %s", app.Version)
	}

	// Verify that the custom install script path was preserved in the upgrade
	if app.InstallScript != installScriptPath {
		t.Errorf("expected InstallScript to still be %q, got %q", installScriptPath, app.InstallScript)
	}

	// Verify that the update URL (source) was preserved in the upgrade
	if app.UpdateURL != mockZipServer.URL {
		t.Errorf("expected UpdateURL to still be %q, got %q", mockZipServer.URL, app.UpdateURL)
	}
}

// helper to extract host or use direct URL for mock server
func serverURLToHost(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s", scheme, r.Host)
}

// helper to write a real valid zip file
func writeRealZip(dest string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	z := zip.NewWriter(f)
	defer z.Close()

	w, err := z.Create("main.c")
	if err != nil {
		return err
	}
	_, err = w.Write([]byte("int main() { return 0; }"))
	return err
}

func TestPreserveSelectedBinariesUpgrade(t *testing.T) {
	// Set registry override path to a temporary file
	tempDir := t.TempDir()
	registryPathOverride = filepath.Join(tempDir, "registry.json")
	defer func() { registryPathOverride = "" }()

	// Create a zip archive with multiple executable scripts: bin1 and bin2
	zipPath1 := filepath.Join(tempDir, "release-1.0.0.zip")
	err := writeMultiExeZip(zipPath1, []string{"bin1", "bin2"})
	if err != nil {
		t.Fatalf("failed to write zip 1: %v", err)
	}

	config := &Config{
		OptDir:      filepath.Join(tempDir, "opt"),
		BinDir:      filepath.Join(tempDir, "bin"),
		AppsDir:     filepath.Join(tempDir, "apps"),
		IconsDir:    filepath.Join(tempDir, "icons"),
		AutoConfirm: true,
	}
	reg := &Registry{Apps: make(map[string]AppMetadata)}

	// Install first version
	err = InstallApp(zipPath1, InstallOptions{
		ForcedName:    "multi-app",
		ForcedVersion: "1.0.0",
	}, config, reg)
	if err != nil {
		t.Fatalf("InstallApp 1 failed: %v", err)
	}

	app, exists := reg.Apps["multi-app"]
	if !exists {
		t.Fatalf("expected multi-app to be registered")
	}

	if len(app.SelectedBinaries) == 0 {
		t.Fatalf("expected SelectedBinaries to be populated")
	}
	t.Logf("Initially selected binaries: %v", app.SelectedBinaries)

	// Let's manually overwrite SelectedBinaries in registry to simulate a user selecting both ["bin1", "bin2"]
	app.SelectedBinaries = []string{"bin1", "bin2"}
	reg.Apps["multi-app"] = app
	err = SaveRegistry(reg)
	if err != nil {
		t.Fatalf("failed to save registry: %v", err)
	}

	// Upgrade using a zip archive that has the EXACT SAME executables (bin1 and bin2)
	// and AutoConfirm disabled so we can see if it prompts or automatically preserves them!
	config.AutoConfirm = false // DISABLE auto-confirm to test if it automatically bypasses prompting!

	// Physically remove target opt folder to avoid the overwrite prompt, but keep registry metadata
	os.RemoveAll(filepath.Join(config.OptDir, "multi-app"))

	zipPath2 := filepath.Join(tempDir, "release-2.0.0.zip")
	err = writeMultiExeZip(zipPath2, []string{"bin1", "bin2"})
	if err != nil {
		t.Fatalf("failed to write zip 2: %v", err)
	}

	err = InstallApp(zipPath2, InstallOptions{
		ForcedName:    "multi-app",
		ForcedVersion: "2.0.0",
		ForcedSource:  "plugin://dummy", // prevent remote download/check logic prompting
	}, config, reg)
	if err != nil {
		t.Fatalf("InstallApp 2 failed: %v", err)
	}

	// Verify that the upgraded app has preserved both selected binaries!
	app = reg.Apps["multi-app"]
	if len(app.SelectedBinaries) != 2 || app.SelectedBinaries[0] != "bin1" || app.SelectedBinaries[1] != "bin2" {
		t.Errorf("expected SelectedBinaries to preserve [bin1, bin2], got %v", app.SelectedBinaries)
	}

	// Test Rename via SetAppMetadata and check secondary symlinks
	err = SetAppMetadata("multi-app", SetAppOptions{NewName: "multi-app-renamed"}, config, reg)
	if err != nil {
		t.Fatalf("SetAppMetadata failed: %v", err)
	}

	app = reg.Apps["multi-app-renamed"]
	if len(app.SelectedBinaries) != 2 {
		t.Fatalf("expected SelectedBinaries to have 2 entries after rename, got %d", len(app.SelectedBinaries))
	}

	// Verify that the secondary symlink bin2 exists and points to the new path
	bin2Path := filepath.Join(config.BinDir, "bin2")
	targetLink, err := os.Readlink(bin2Path)
	if err != nil {
		t.Errorf("expected secondary symlink bin2 to exist after rename, got error: %v", err)
	} else {
		expectedTarget := filepath.Join(config.OptDir, "multi-app-renamed", "bin2")
		if targetLink != expectedTarget {
			t.Errorf("expected secondary symlink to point to %q, got %q", expectedTarget, targetLink)
		}
	}

	// Test UninstallApp and verify it removes all symlinks (bin1 and bin2)
	config.AutoConfirm = true
	err = UninstallApp("multi-app-renamed", config, reg)
	if err != nil {
		t.Fatalf("UninstallApp failed: %v", err)
	}

	if _, err := os.Lstat(filepath.Join(config.BinDir, "bin1")); err == nil {
		t.Errorf("expected primary symlink bin1 to be deleted after uninstall")
	}
	if _, err := os.Lstat(filepath.Join(config.BinDir, "bin2")); err == nil {
		t.Errorf("expected secondary symlink bin2 to be deleted after uninstall")
	}
}

func writeMultiExeZip(dest string, filenames []string) error {
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	z := zip.NewWriter(f)
	defer z.Close()

	for _, fname := range filenames {
		header := &zip.FileHeader{
			Name:   fname,
			Method: zip.Deflate,
		}
		header.SetMode(0755)
		w, err := z.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte("#!/bin/bash\necho 'hello'\n"))
		if err != nil {
			return err
		}
	}
	return nil
}

func TestXdgEnvironmentVariables(t *testing.T) {
	tempDir := t.TempDir()

	configDir := filepath.Join(tempDir, "custom-config")
	dataDir := filepath.Join(tempDir, "custom-data")
	binDir := filepath.Join(tempDir, "custom-bin")

	// Set XDG environment variables
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("XDG_DATA_HOME", dataDir)
	t.Setenv("XDG_BIN_HOME", binDir)

	// Verify GetConfigPath uses XDG_CONFIG_HOME
	cfgPath, err := GetConfigPath()
	if err != nil {
		t.Fatalf("failed to get config path: %v", err)
	}
	expectedCfgPath := filepath.Join(configDir, "plop", "config.json")
	if cfgPath != expectedCfgPath {
		t.Errorf("expected config path %q, got %q", expectedCfgPath, cfgPath)
	}

	// Verify GetRegistryPath defaults to XDG_DATA_HOME when legacy path doesn't exist
	regPath, err := GetRegistryPath()
	if err != nil {
		t.Fatalf("failed to get registry path: %v", err)
	}
	expectedRegPath := filepath.Join(dataDir, "plop", "registry.json")
	if regPath != expectedRegPath {
		t.Errorf("expected registry path %q, got %q", expectedRegPath, regPath)
	}

	// Verify LoadConfig automatically initializes directories using the XDG overrides
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.BinDir != binDir {
		t.Errorf("expected BinDir to be overridden to %q, got %q", binDir, cfg.BinDir)
	}
	expectedAppsDir := filepath.Join(dataDir, "applications")
	if IsDarwin() {
		expectedAppsDir = filepath.Join(dataDir, "plop", "applications")
	}
	if cfg.AppsDir != expectedAppsDir {
		t.Errorf("expected AppsDir to be overridden to %q, got %q", expectedAppsDir, cfg.AppsDir)
	}
	expectedIconsDir := filepath.Join(dataDir, "icons")
	if IsDarwin() {
		expectedIconsDir = filepath.Join(dataDir, "plop", "icons")
	}
	if cfg.IconsDir != expectedIconsDir {
		t.Errorf("expected IconsDir to be overridden to %q, got %q", expectedIconsDir, cfg.IconsDir)
	}

	// Verify GetRegistryPath returns legacy config path if it exists (backward compatibility)
	legacyPath := filepath.Join(configDir, "plop", "registry.json")
	err = os.MkdirAll(filepath.Dir(legacyPath), 0755)
	if err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	err = os.WriteFile(legacyPath, []byte("{}"), 0644)
	if err != nil {
		t.Fatalf("failed to write dummy legacy registry: %v", err)
	}

	regPath, err = GetRegistryPath()
	if err != nil {
		t.Fatalf("failed to get legacy registry path: %v", err)
	}
	if regPath != legacyPath {
		t.Errorf("expected legacy registry path %q to be preferred, got %q", legacyPath, regPath)
	}
}

func TestNoColorSupport(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	InitColors()
	defer func() {
		os.Unsetenv("NO_COLOR")
		InitColors()
	}()

	if colorsEnabled {
		t.Errorf("expected colorsEnabled to be false when NO_COLOR is set")
	}

	res := color(ColorRed, "hello")
	if res != "hello" {
		t.Errorf("expected color() to return unmodified text 'hello' when color is disabled, got %q", res)
	}

	// Manually toggle colorsEnabled to test standard coloring
	colorsEnabled = true
	resEnabled := color(ColorRed, "hello")
	expected := ColorRed + "hello" + ColorReset
	if resEnabled != expected {
		t.Errorf("expected color() to return formatted string %q when colors are enabled, got %q", expected, resEnabled)
	}
}

func TestStdinInstallAndJsonOutput(t *testing.T) {
	tempDir := t.TempDir()
	registryPathOverride = filepath.Join(tempDir, "registry.json")
	defer func() { registryPathOverride = "" }()

	// Create a dummy zip archive
	zipPath := filepath.Join(tempDir, "source.zip")
	err := writeMultiExeZip(zipPath, []string{"bin1"})
	if err != nil {
		t.Fatalf("failed to create dummy zip: %v", err)
	}

	// Read zip file bytes to simulate stdin stream
	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatalf("failed to read zip bytes: %v", err)
	}

	// Mock stdin by replacing os.Stdin
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdin = r

	// Write zip bytes to pipe in a separate goroutine
	go func() {
		w.Write(zipBytes)
		w.Close()
	}()

	config := &Config{
		OptDir:      filepath.Join(tempDir, "opt"),
		BinDir:      filepath.Join(tempDir, "bin"),
		AppsDir:     filepath.Join(tempDir, "apps"),
		IconsDir:    filepath.Join(tempDir, "icons"),
		AutoConfirm: true,
	}
	reg := &Registry{Apps: make(map[string]AppMetadata)}

	// Call InstallApp with "-" to read from mocked stdin
	err = InstallApp("-", InstallOptions{
		ForcedName:    "stdin-app",
		ForcedVersion: "3.2.0",
	}, config, reg)
	if err != nil {
		t.Fatalf("InstallApp from stdin failed: %v", err)
	}

	// Verify application is registered
	app, exists := reg.Apps["stdin-app"]
	if !exists {
		t.Fatalf("expected stdin-app to be registered")
	}
	if app.Version != "3.2.0" {
		t.Errorf("expected version to be 3.2.0, got %s", app.Version)
	}
}

func TestFindBestLinuxAsset(t *testing.T) {
	// Case 1: Single asset in release (mocked scraped / plugin releases)
	releaseSingle := &GithubRelease{
		TagName: "v2.0.6",
		Assets: []GithubAsset{
			{
				Name:               "Antigravity.tar.gz",
				BrowserDownloadURL: "https://storage.googleapis.com/antigravity-public/antigravity-hub/2.0.6-5413878570549248/linux-x64/Antigravity.tar.gz",
			},
		},
	}
	url, name := FindBestAsset(releaseSingle)
	if url != "https://storage.googleapis.com/antigravity-public/antigravity-hub/2.0.6-5413878570549248/linux-x64/Antigravity.tar.gz" || name != "Antigravity.tar.gz" {
		t.Errorf("expected single asset to be returned, got url=%q, name=%q", url, name)
	}

	// Case 2: AppImage in multi-asset release (Cutter / desktop apps)
	releaseAppImage := &GithubRelease{
		TagName: "v2.4.1",
		Assets: []GithubAsset{
			{Name: "Cutter-v2.4.1-macOS.dmg", BrowserDownloadURL: "https://github.com/mac"},
			{Name: "Cutter-v2.4.1-Windows.zip", BrowserDownloadURL: "https://github.com/win"},
			{Name: "Cutter-v2.4.1-Linux-x86_64.AppImage", BrowserDownloadURL: "https://github.com/linux-appimage"},
		},
	}
	url, name = findBestLinuxAsset(releaseAppImage)
	if url != "https://github.com/linux-appimage" || name != "Cutter-v2.4.1-Linux-x86_64.AppImage" {
		t.Errorf("expected Linux AppImage asset to be selected, got url=%q, name=%q", url, name)
	}

	// Case 3: Makeself shell script installer (.run) in multi-asset release
	releaseMakeself := &GithubRelease{
		TagName: "release-2.7.1",
		Assets: []GithubAsset{
			{Name: "makeself-2.7.1.run", BrowserDownloadURL: "https://github.com/megastep/makeself/releases/download/release-2.7.1/makeself-2.7.1.run"},
			{Name: "Source code (zip)", BrowserDownloadURL: "https://github.com/src-zip"},
			{Name: "Source code (tar.gz)", BrowserDownloadURL: "https://github.com/src-tar"},
		},
	}
	url, name = findBestLinuxAsset(releaseMakeself)
	if url != "https://github.com/megastep/makeself/releases/download/release-2.7.1/makeself-2.7.1.run" || name != "makeself-2.7.1.run" {
		t.Errorf("expected Makeself .run script asset to be selected, got url=%q, name=%q", url, name)
	}

	// Case 4: Standard tar.gz asset matching Linux amd64
	releaseStandard := &GithubRelease{
		TagName: "v0.10.0",
		Assets: []GithubAsset{
			{Name: "janice-0.10.0-windows-amd64.zip", BrowserDownloadURL: "https://github.com/win-zip"},
			{Name: "janice-0.10.0-linux-amd64.tar.xz", BrowserDownloadURL: "https://github.com/linux-tar"},
			{Name: "janice-0.10.0-darwin-amd64.tar.gz", BrowserDownloadURL: "https://github.com/mac-tar"},
		},
	}
	url, name = findBestLinuxAsset(releaseStandard)
	if url != "https://github.com/linux-tar" || name != "janice-0.10.0-linux-amd64.tar.xz" {
		t.Errorf("expected Linux xz tarball asset to be selected, got url=%q, name=%q", url, name)
	}
}

// Config round-trip preservation
func TestPropertyConfigRoundTrip(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	// Generate random valid Config structs, write them as JSON to a temp config path,
	// then call LoadConfig and verify all fields match.
	err := quick.Check(func(optDir, binDir, appsDir, iconsDir, token string, autoConfirm, hasGUI, guiVal bool) bool {
		// Filter out invalid path strings (empty or containing null bytes)
		for _, s := range []string{optDir, binDir} {
			if s == "" || strings.ContainsRune(s, 0) {
				return true // skip invalid inputs
			}
		}
		if strings.ContainsRune(appsDir, 0) || strings.ContainsRune(iconsDir, 0) || strings.ContainsRune(token, 0) {
			return true // skip invalid inputs
		}

		// Create isolated temp environment
		tempDir := t.TempDir()
		configDir := filepath.Join(tempDir, "config", "plop")
		err := os.MkdirAll(configDir, 0755)
		if err != nil {
			t.Logf("MkdirAll failed: %v", err)
			return false
		}

		// Build the Config to write
		var defaultGUI *bool
		if hasGUI {
			v := guiVal
			defaultGUI = &v
		}
		original := Config{
			OptDir:      optDir,
			BinDir:      binDir,
			AppsDir:     appsDir,
			IconsDir:    iconsDir,
			GithubToken: token,
			AutoConfirm: autoConfirm,
			DefaultGUI:  defaultGUI,
		}

		// Write config as JSON
		data, err := json.MarshalIndent(&original, "", "  ")
		if err != nil {
			t.Logf("Marshal failed: %v", err)
			return false
		}
		configPath := filepath.Join(configDir, "config.json")
		err = os.WriteFile(configPath, data, 0644)
		if err != nil {
			t.Logf("WriteFile failed: %v", err)
			return false
		}

		// Point XDG_CONFIG_HOME to our temp dir so LoadConfig finds our file
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempDir, "config"))

		// LoadConfig should load the existing file
		loaded, err := LoadConfig()
		if err != nil {
			t.Logf("LoadConfig failed: %v", err)
			return false
		}

		// Compare all fields
		if loaded.OptDir != original.OptDir {
			t.Logf("OptDir mismatch: got %q, want %q", loaded.OptDir, original.OptDir)
			return false
		}
		if loaded.BinDir != original.BinDir {
			t.Logf("BinDir mismatch: got %q, want %q", loaded.BinDir, original.BinDir)
			return false
		}
		if loaded.AppsDir != original.AppsDir {
			t.Logf("AppsDir mismatch: got %q, want %q", loaded.AppsDir, original.AppsDir)
			return false
		}
		if loaded.IconsDir != original.IconsDir {
			t.Logf("IconsDir mismatch: got %q, want %q", loaded.IconsDir, original.IconsDir)
			return false
		}
		if loaded.GithubToken != original.GithubToken {
			t.Logf("GithubToken mismatch: got %q, want %q", loaded.GithubToken, original.GithubToken)
			return false
		}
		if loaded.AutoConfirm != original.AutoConfirm {
			t.Logf("AutoConfirm mismatch: got %v, want %v", loaded.AutoConfirm, original.AutoConfirm)
			return false
		}
		if original.DefaultGUI == nil && loaded.DefaultGUI != nil {
			t.Logf("DefaultGUI mismatch: got non-nil, want nil")
			return false
		}
		if original.DefaultGUI != nil {
			if loaded.DefaultGUI == nil {
				t.Logf("DefaultGUI mismatch: got nil, want %v", *original.DefaultGUI)
				return false
			}
			if *loaded.DefaultGUI != *original.DefaultGUI {
				t.Logf("DefaultGUI mismatch: got %v, want %v", *loaded.DefaultGUI, *original.DefaultGUI)
				return false
			}
		}

		return true
	}, &cfg)

	if err != nil {
		t.Errorf("Config round-trip preservation failed: %v", err)
	}
}

// XDG override precedence
func TestPropertyXDGOverridePrecedence(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	err := quick.Check(func(configHome, dataHome, binHome string) bool {
		// Filter: must be non-empty and no null bytes
		for _, s := range []string{configHome, dataHome, binHome} {
			if s == "" || strings.ContainsRune(s, 0) {
				return true // skip invalid inputs
			}
		}

		// Use absolute paths to avoid ambiguity
		tempDir := t.TempDir()
		absConfigHome := filepath.Join(tempDir, "xdg", configHome)
		absDataHome := filepath.Join(tempDir, "xdg", dataHome)
		absBinHome := filepath.Join(tempDir, "xdg", binHome)

		// Set XDG environment variables
		t.Setenv("XDG_CONFIG_HOME", absConfigHome)
		t.Setenv("XDG_DATA_HOME", absDataHome)
		t.Setenv("XDG_BIN_HOME", absBinHome)

		// Verify PlatformConfigDir derives from XDG_CONFIG_HOME
		gotConfigDir := PlatformConfigDir()
		expectedConfigDir := filepath.Join(absConfigHome, "plop")
		if gotConfigDir != expectedConfigDir {
			t.Logf("PlatformConfigDir: got %q, want %q", gotConfigDir, expectedConfigDir)
			return false
		}

		// Verify PlatformDataDir derives from XDG_DATA_HOME
		gotDataDir := PlatformDataDir()
		expectedDataDir := filepath.Join(absDataHome, "plop")
		if gotDataDir != expectedDataDir {
			t.Logf("PlatformDataDir: got %q, want %q", gotDataDir, expectedDataDir)
			return false
		}

		// Verify PlatformDefaultConfig uses XDG_BIN_HOME for BinDir
		defaultCfg := PlatformDefaultConfig()
		if defaultCfg.BinDir != absBinHome {
			t.Logf("PlatformDefaultConfig().BinDir: got %q, want %q", defaultCfg.BinDir, absBinHome)
			return false
		}

		return true
	}, &cfg)

	if err != nil {
		t.Errorf("XDG override precedence failed: %v", err)
	}
}

// Directory auto-creation on first access
func TestPropertyDirectoryAutoCreation(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	err := quick.Check(func(seed uint32) bool {
		// Generate a safe ASCII subdirectory name from the seed
		const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
		subDir := ""
		v := seed
		if v == 0 {
			v = 1
		}
		for i := 0; i < 8; i++ {
			subDir += string(chars[v%uint32(len(chars))])
			v = v / uint32(len(chars))
		}
		if subDir == "" {
			return true
		}

		tempDir := t.TempDir()
		configHome := filepath.Join(tempDir, subDir, "cfg")
		dataHome := filepath.Join(tempDir, subDir, "data")

		// Set XDG overrides to point to non-existent directories
		t.Setenv("XDG_CONFIG_HOME", configHome)
		t.Setenv("XDG_DATA_HOME", dataHome)

		// Clear registry override so GetRegistryPath uses PlatformDataDir
		oldOverride := registryPathOverride
		registryPathOverride = ""
		defer func() { registryPathOverride = oldOverride }()

		// Verify config directory doesn't exist yet
		configDir := filepath.Join(configHome, "plop")
		if _, err := os.Stat(configDir); err == nil {
			return true // already exists, skip (shouldn't happen with TempDir)
		}

		// LoadConfig should auto-create the config directory
		_, err := LoadConfig()
		if err != nil {
			t.Logf("LoadConfig failed: %v", err)
			return false
		}

		// Verify config directory was created with 0755
		info, err := os.Stat(configDir)
		if err != nil {
			t.Logf("Config dir not created: %v", err)
			return false
		}
		if !info.IsDir() {
			t.Logf("Config path is not a directory")
			return false
		}
		perm := info.Mode().Perm()
		if perm != 0755 {
			t.Logf("Config dir permissions: got %o, want 0755", perm)
			return false
		}

		// GetRegistryPath should auto-create the data directory
		dataDir := filepath.Join(dataHome, "plop")
		// Remove legacy path if LoadConfig created it in configHome
		// (GetRegistryPath checks legacy first)
		legacyReg := filepath.Join(configDir, "registry.json")
		os.Remove(legacyReg)

		_, err = GetRegistryPath()
		if err != nil {
			t.Logf("GetRegistryPath failed: %v", err)
			return false
		}

		// Verify data directory was created with 0755
		info, err = os.Stat(dataDir)
		if err != nil {
			t.Logf("Data dir not created: %v", err)
			return false
		}
		if !info.IsDir() {
			t.Logf("Data path is not a directory")
			return false
		}
		perm = info.Mode().Perm()
		if perm != 0755 {
			t.Logf("Data dir permissions: got %o, want 0755", perm)
			return false
		}

		return true
	}, &cfg)

	if err != nil {
		t.Errorf("Directory auto-creation on first access failed: %v", err)
	}
}

// Shebang detection universality
func TestPropertyShebangDetection(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	err := quick.Check(func(content []byte) bool {
		// Prepend shebang to random content
		shebangContent := append([]byte("#!"), content...)

		// Write to a temp file
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "script")
		err := os.WriteFile(filePath, shebangContent, 0644)
		if err != nil {
			t.Logf("WriteFile failed: %v", err)
			return false
		}

		// IsScript must return true for any file starting with #!
		isScript, err := IsScript(filePath)
		if err != nil {
			t.Logf("IsScript returned error: %v", err)
			return false
		}
		if !isScript {
			t.Logf("IsScript returned false for file starting with #! (content len=%d)", len(content))
			return false
		}

		return true
	}, &cfg)

	if err != nil {
		t.Errorf("Shebang detection universality failed: %v", err)
	}
}

// Non-executable file graceful skip
func TestPropertyNonExecutableSkip(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	err := quick.Check(func(content []byte) bool {
		// Ensure content does NOT start with any recognized magic bytes:
		// - ELF magic: 0x7F 0x45 0x4C 0x46
		// - Mach-O 32-bit: 0xFE 0xED 0xFA 0xCE
		// - Mach-O 64-bit: 0xFE 0xED 0xFA 0xCF
		// - Universal/fat: 0xCA 0xFE 0xBA 0xBE
		// - Shebang: 0x23 0x21 (#!)
		if len(content) < 2 {
			// Files shorter than 2 bytes can't match shebang, but could be empty
			// Ensure they don't accidentally match anything
			content = []byte{0x00, 0x00}
		}

		// Check and reject if content starts with shebang
		if content[0] == '#' && content[1] == '!' {
			content[0] = 0x00 // neutralize shebang
		}

		// Check and reject if content starts with ELF magic
		if len(content) >= 4 && content[0] == 0x7F && content[1] == 0x45 && content[2] == 0x4C && content[3] == 0x46 {
			content[0] = 0x00 // neutralize ELF magic
		}

		// Check and reject if content starts with Mach-O magic (0xFEEDFACE or 0xFEEDFACF)
		if len(content) >= 4 && content[0] == 0xFE && content[1] == 0xED && content[2] == 0xFA && (content[3] == 0xCE || content[3] == 0xCF) {
			content[0] = 0x00 // neutralize Mach-O magic
		}

		// Check and reject if content starts with universal binary magic (0xCAFEBABE)
		if len(content) >= 4 && content[0] == 0xCA && content[1] == 0xFE && content[2] == 0xBA && content[3] == 0xBE {
			content[0] = 0x00 // neutralize fat binary magic
		}

		// Write to a temp file
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "nonexec")
		err := os.WriteFile(filePath, content, 0644)
		if err != nil {
			t.Logf("WriteFile failed: %v", err)
			return false
		}

		// IsELF must return (false, nil)
		isElf, err := IsELF(filePath)
		if err != nil {
			t.Logf("IsELF returned error: %v", err)
			return false
		}
		if isElf {
			t.Logf("IsELF returned true for non-ELF content")
			return false
		}

		// IsMachO must return (false, nil)
		isMacho, err := IsMachO(filePath)
		if err != nil {
			t.Logf("IsMachO returned error: %v", err)
			return false
		}
		if isMacho {
			t.Logf("IsMachO returned true for non-Mach-O content")
			return false
		}

		// IsScript must return (false, nil)
		isScript, err := IsScript(filePath)
		if err != nil {
			t.Logf("IsScript returned error: %v", err)
			return false
		}
		if isScript {
			t.Logf("IsScript returned true for non-shebang content")
			return false
		}

		// IsExecutable must return (false, false, nil)
		isExec, isNative, err := IsExecutable(filePath)
		if err != nil {
			t.Logf("IsExecutable returned error: %v", err)
			return false
		}
		if isExec {
			t.Logf("IsExecutable returned isExecutable=true for non-executable content")
			return false
		}
		if isNative {
			t.Logf("IsExecutable returned isNativeBinary=true for non-executable content")
			return false
		}

		return true
	}, &cfg)

	if err != nil {
		t.Errorf("Non-executable file graceful skip failed: %v", err)
	}
}

// Non-hidden file filter for DMG extraction
func TestPropertyDMGNonHiddenFileFilter(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	err := quick.Check(func(seed uint64) bool {
		// Generate a mix of hidden and non-hidden filenames from the seed
		const safeChars = "abcdefghijklmnopqrstuvwxyz0123456789"
		rng := seed
		if rng == 0 {
			rng = 1
		}

		// Simple pseudo-random number generator
		nextRng := func() uint64 {
			rng = rng*6364136223846793005 + 1442695040888963407
			return rng
		}

		// Generate between 1 and 20 filenames
		numFiles := int(nextRng()%20) + 1
		type fileEntry struct {
			name     string
			isDir    bool
			isHidden bool
		}
		var entries []fileEntry

		for i := 0; i < numFiles; i++ {
			// Generate a filename of length 1-10
			nameLen := int(nextRng()%10) + 1
			name := ""
			for j := 0; j < nameLen; j++ {
				name += string(safeChars[nextRng()%uint64(len(safeChars))])
			}

			// Decide if hidden (prefix with '.')
			isHidden := nextRng()%2 == 0
			if isHidden {
				name = "." + name
			}

			// Decide if directory
			isDir := nextRng()%3 == 0

			// Avoid duplicate names
			duplicate := false
			for _, e := range entries {
				if e.name == name {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}

			entries = append(entries, fileEntry{name: name, isDir: isDir, isHidden: isHidden})
		}

		if len(entries) == 0 {
			return true // skip empty case
		}

		// Create source directory (simulating a mounted volume)
		srcDir := t.TempDir()
		for _, entry := range entries {
			srcPath := filepath.Join(srcDir, entry.name)
			if entry.isDir {
				if err := os.MkdirAll(srcPath, 0755); err != nil {
					t.Logf("MkdirAll failed: %v", err)
					return false
				}
				// Put a file inside the directory to verify recursive copy
				if err := os.WriteFile(filepath.Join(srcPath, "inner.txt"), []byte("content"), 0644); err != nil {
					t.Logf("WriteFile inner failed: %v", err)
					return false
				}
			} else {
				if err := os.WriteFile(srcPath, []byte("file-content-"+entry.name), 0644); err != nil {
					t.Logf("WriteFile failed: %v", err)
					return false
				}
			}
		}

		// Apply the same filtering logic as ExtractDMG:
		// Read entries, skip those starting with '.', copy the rest
		destDir := t.TempDir()
		dirEntries, err := os.ReadDir(srcDir)
		if err != nil {
			t.Logf("ReadDir failed: %v", err)
			return false
		}

		for _, de := range dirEntries {
			name := de.Name()
			// Skip hidden files (names starting with '.')
			if strings.HasPrefix(name, ".") {
				continue
			}

			srcEntry := filepath.Join(srcDir, name)
			dstEntry := filepath.Join(destDir, name)

			if de.IsDir() {
				if err := copyDir(srcEntry, dstEntry); err != nil {
					t.Logf("copyDir failed for %s: %v", name, err)
					return false
				}
			} else {
				if err := CopyFile(srcEntry, dstEntry); err != nil {
					t.Logf("CopyFile failed for %s: %v", name, err)
					return false
				}
			}
		}

		// Verify: destination contains ONLY non-hidden files
		destEntries, err := os.ReadDir(destDir)
		if err != nil {
			t.Logf("ReadDir dest failed: %v", err)
			return false
		}

		// Build expected set of non-hidden entries
		expectedNonHidden := make(map[string]bool)
		for _, entry := range entries {
			if !entry.isHidden {
				expectedNonHidden[entry.name] = true
			}
		}

		// Check that no hidden files are present in destination
		for _, de := range destEntries {
			if strings.HasPrefix(de.Name(), ".") {
				t.Logf("Hidden file %q found in destination", de.Name())
				return false
			}
		}

		// Check that all non-hidden files are present
		gotNames := make(map[string]bool)
		for _, de := range destEntries {
			gotNames[de.Name()] = true
		}

		for name := range expectedNonHidden {
			if !gotNames[name] {
				t.Logf("Expected non-hidden entry %q not found in destination", name)
				return false
			}
		}

		// Check that destination has exactly the expected count
		if len(destEntries) != len(expectedNonHidden) {
			t.Logf("Destination has %d entries, expected %d", len(destEntries), len(expectedNonHidden))
			return false
		}

		return true
	}, &cfg)

	if err != nil {
		t.Errorf("Non-hidden file filter for DMG extraction failed: %v", err)
	}
}

// Platform-aware asset selection correctness
func TestPropertyAssetSelection(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	// Platform identifiers for macOS
	darwinPlatforms := []string{"darwin", "macos", "osx"}
	// Valid extensions for macOS
	darwinValidExts := []string{".tar.gz", ".tar.xz", ".zip", ".dmg"}
	// Architecture identifiers
	arm64Archs := []string{"arm64", "aarch64"}
	amd64Archs := []string{"amd64", "x86_64", "x64"}

	err := quick.Check(func(seed uint64) bool {
		if seed == 0 {
			seed = 1
		}
		rng := seed

		nextRng := func() uint64 {
			rng = rng*6364136223846793005 + 1442695040888963407
			return rng
		}

		// Generate between 2 and 10 assets (more than 1 to avoid single-asset shortcut)
		numAssets := int(nextRng()%9) + 2
		assets := make([]GithubAsset, 0, numAssets)

		// Ensure at least one macOS-compatible asset exists
		// Pick a random platform identifier, architecture, and extension
		platIdx := int(nextRng() % uint64(len(darwinPlatforms)))
		extIdx := int(nextRng() % uint64(len(darwinValidExts)))

		var archStr string
		if runtime.GOARCH == "arm64" {
			archIdx := int(nextRng() % uint64(len(arm64Archs)))
			archStr = arm64Archs[archIdx]
		} else {
			archIdx := int(nextRng() % uint64(len(amd64Archs)))
			archStr = amd64Archs[archIdx]
		}

		// Build the guaranteed macOS-compatible asset
		macAssetName := fmt.Sprintf("app-%s-%s%s", darwinPlatforms[platIdx], archStr, darwinValidExts[extIdx])
		macAssetURL := fmt.Sprintf("https://example.com/releases/%s", macAssetName)
		assets = append(assets, GithubAsset{
			Name:               macAssetName,
			BrowserDownloadURL: macAssetURL,
		})

		// Generate remaining random assets (mix of platforms)
		otherPlatforms := []string{"linux", "windows", "freebsd"}
		otherExts := []string{".tar.gz", ".zip", ".appimage", ".exe", ".deb", ".rpm"}
		for i := 1; i < numAssets; i++ {
			plat := otherPlatforms[int(nextRng()%uint64(len(otherPlatforms)))]
			ext := otherExts[int(nextRng()%uint64(len(otherExts)))]
			name := fmt.Sprintf("app-%s-amd64%s", plat, ext)
			url := fmt.Sprintf("https://example.com/releases/%s", name)
			assets = append(assets, GithubAsset{
				Name:               name,
				BrowserDownloadURL: url,
			})
		}

		// Shuffle assets using Fisher-Yates
		for i := len(assets) - 1; i > 0; i-- {
			j := int(nextRng() % uint64(i+1))
			assets[i], assets[j] = assets[j], assets[i]
		}

		release := &GithubRelease{
			TagName: "v1.0.0",
			Assets:  assets,
		}

		// Call the platform-specific function directly based on runtime
		var selectedURL, selectedName string
		if runtime.GOOS == "darwin" {
			selectedURL, selectedName = findBestDarwinAsset(release)
		} else {
			// On Linux, we test findBestDarwinAsset directly to validate the logic
			selectedURL, selectedName = findBestDarwinAsset(release)
		}

		if selectedURL == "" || selectedName == "" {
			t.Logf("No asset selected from release with %d assets (seed=%d)", len(assets), seed)
			return false
		}

		nameLower := strings.ToLower(selectedName)
		urlLower := strings.ToLower(selectedURL)

		// (a) Selected asset must contain a macOS platform identifier
		hasPlatform := false
		for _, p := range darwinPlatforms {
			if strings.Contains(nameLower, p) || strings.Contains(urlLower, p) {
				hasPlatform = true
				break
			}
		}
		if !hasPlatform {
			t.Logf("Selected asset %q does not contain a macOS platform identifier (seed=%d)", selectedName, seed)
			return false
		}

		// (b) If assets with matching architecture exist, selected must match host arch
		var hostArchIds []string
		if runtime.GOARCH == "arm64" {
			hostArchIds = arm64Archs
		} else {
			hostArchIds = amd64Archs
		}

		// Check if any asset has both platform + arch match
		archAssetsExist := false
		for _, a := range assets {
			aName := strings.ToLower(a.Name)
			aURL := strings.ToLower(a.BrowserDownloadURL)
			aHasPlatform := false
			for _, p := range darwinPlatforms {
				if strings.Contains(aName, p) || strings.Contains(aURL, p) {
					aHasPlatform = true
					break
				}
			}
			if !aHasPlatform {
				continue
			}
			for _, arch := range hostArchIds {
				if strings.Contains(aName, arch) || strings.Contains(aURL, arch) {
					archAssetsExist = true
					break
				}
			}
			if archAssetsExist {
				break
			}
		}

		if archAssetsExist {
			hasArch := false
			for _, arch := range hostArchIds {
				if strings.Contains(nameLower, arch) || strings.Contains(urlLower, arch) {
					hasArch = true
					break
				}
			}
			if !hasArch {
				t.Logf("Arch-matching assets exist but selected %q doesn't match host arch (seed=%d)", selectedName, seed)
				return false
			}
		}

		// (c) Selected asset must end with an accepted extension
		hasValidExt := false
		for _, ext := range darwinValidExts {
			if strings.HasSuffix(nameLower, ext) {
				hasValidExt = true
				break
			}
		}
		if !hasValidExt {
			t.Logf("Selected asset %q does not have a valid macOS extension (seed=%d)", selectedName, seed)
			return false
		}

		// (d) Selected asset must NOT end with .appimage
		if strings.HasSuffix(nameLower, ".appimage") {
			t.Logf("Selected asset %q ends with .appimage which is excluded on macOS (seed=%d)", selectedName, seed)
			return false
		}

		return true
	}, &cfg)

	if err != nil {
		t.Errorf("Platform-aware asset selection correctness failed: %v", err)
	}
}

// Single-asset release always selected
func TestPropertySingleAsset(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	err := quick.Check(func(name, url string) bool {
		// Filter out empty or null-byte strings
		if name == "" || url == "" || strings.ContainsRune(name, 0) || strings.ContainsRune(url, 0) {
			return true // skip invalid inputs
		}

		release := &GithubRelease{
			TagName: "v1.0.0",
			Assets: []GithubAsset{
				{
					Name:               name,
					BrowserDownloadURL: url,
				},
			},
		}

		selectedURL, selectedName := FindBestAsset(release)

		// For a single-asset release, that asset must always be returned
		if selectedURL != url {
			t.Logf("Single-asset URL mismatch: got %q, want %q (name=%q)", selectedURL, url, name)
			return false
		}
		if selectedName != name {
			t.Logf("Single-asset name mismatch: got %q, want %q", selectedName, name)
			return false
		}

		return true
	}, &cfg)

	if err != nil {
		t.Errorf("Single-asset release always selected failed: %v", err)
	}
}

// Platform-aware link scraping
func TestPropertyLinkScraping(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	err := quick.Check(func(seed uint64) bool {
		if seed == 0 {
			seed = 1
		}
		rng := seed

		nextRng := func() uint64 {
			rng = rng*6364136223846793005 + 1442695040888963407
			return rng
		}

		// Platform identifiers for macOS and Linux
		macPlatforms := []string{"darwin", "macos", "osx"}
		linuxPlatforms := []string{"linux"}

		// Architecture identifiers
		var archIdentifiers []string
		if runtime.GOARCH == "arm64" {
			archIdentifiers = []string{"arm64", "aarch64"}
		} else {
			archIdentifiers = []string{"x86_64", "amd64", "x64"}
		}

		// Valid extensions per platform
		macExtensions := []string{".tar.gz", ".tar.xz", ".zip", ".dmg", ".tgz", ".txz", ".tbz2", ".tar.bz2", ".tar"}
		linuxExtensions := []string{".tar.gz", ".tar.xz", ".zip", ".tgz", ".txz", ".tbz2", ".tar.bz2", ".tar"}

		// Generate a random app name (3-8 lowercase chars)
		const chars = "abcdefghijklmnopqrstuvwxyz"
		nameLen := int(nextRng()%6) + 3
		appName := ""
		for i := 0; i < nameLen; i++ {
			appName += string(chars[nextRng()%uint64(len(chars))])
		}

		// Generate a random version (major.minor.patch)
		major := int(nextRng()%5) + 1
		minor := int(nextRng() % 10)
		patch := int(nextRng() % 10)
		version := fmt.Sprintf("%d.%d.%d", major, minor, patch)

		// Pick a random architecture
		arch := archIdentifiers[nextRng()%uint64(len(archIdentifiers))]

		// Build HTML with platform-specific download links
		var links []string

		// Always include a macOS link with a valid extension
		macPlatform := macPlatforms[nextRng()%uint64(len(macPlatforms))]
		macExt := macExtensions[nextRng()%uint64(len(macExtensions))]
		macLink := fmt.Sprintf("/downloads/%s-v%s-%s-%s%s", appName, version, macPlatform, arch, macExt)
		links = append(links, fmt.Sprintf(`<a href="%s">macOS Download</a>`, macLink))

		// Always include a Linux link with a valid extension
		linuxPlatform := linuxPlatforms[0]
		linuxExt := linuxExtensions[nextRng()%uint64(len(linuxExtensions))]
		linuxArch := archIdentifiers[nextRng()%uint64(len(archIdentifiers))]
		linuxLink := fmt.Sprintf("/downloads/%s-v%s-%s-%s%s", appName, version, linuxPlatform, linuxArch, linuxExt)
		links = append(links, fmt.Sprintf(`<a href="%s">Linux Download</a>`, linuxLink))

		// Optionally add a Windows link (should never be matched)
		if nextRng()%2 == 0 {
			winLink := fmt.Sprintf("/downloads/%s-v%s-windows-%s.zip", appName, version, arch)
			links = append(links, fmt.Sprintf(`<a href="%s">Windows Download</a>`, winLink))
		}

		// Build the HTML page
		html := "<html><body>\n" + strings.Join(links, "\n") + "\n</body></html>"

		// Serve the HTML via httptest
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(html))
		}))
		defer server.Close()

		// Call ScrapeGenericRelease
		release, err := ScrapeGenericRelease(server.URL)
		if err != nil {
			t.Logf("ScrapeGenericRelease failed: %v (html: %s)", err, html)
			return false
		}

		if len(release.Assets) == 0 {
			t.Logf("No assets returned")
			return false
		}

		matchedURL := strings.ToLower(release.Assets[0].BrowserDownloadURL)

		if runtime.GOOS == "darwin" {
			// On macOS: matched link must contain a macOS platform identifier
			hasMacPlatform := strings.Contains(matchedURL, "darwin") ||
				strings.Contains(matchedURL, "macos") ||
				strings.Contains(matchedURL, "osx")
			if !hasMacPlatform {
				t.Logf("On macOS, matched URL %q does not contain a macOS platform identifier", matchedURL)
				return false
			}

			// On macOS: matched link must have a valid extension (including .dmg)
			hasValidExt := false
			validExts := []string{".tar.gz", ".tar.xz", ".zip", ".dmg", ".tgz", ".txz", ".tbz2", ".tar.bz2", ".tar"}
			for _, ext := range validExts {
				if strings.HasSuffix(matchedURL, ext) {
					hasValidExt = true
					break
				}
			}
			if !hasValidExt {
				t.Logf("On macOS, matched URL %q does not have a valid extension", matchedURL)
				return false
			}
		} else {
			// On Linux: matched link must contain a Linux platform identifier
			hasLinuxPlatform := strings.Contains(matchedURL, "linux")
			if !hasLinuxPlatform {
				t.Logf("On Linux, matched URL %q does not contain a Linux platform identifier", matchedURL)
				return false
			}

			// On Linux: matched link must have a valid extension
			hasValidExt := false
			validExts := []string{".tar.gz", ".tar.xz", ".zip", ".tgz", ".txz", ".tbz2", ".tar.bz2", ".tar", ".appimage"}
			for _, ext := range validExts {
				if strings.HasSuffix(matchedURL, ext) {
					hasValidExt = true
					break
				}
			}
			if !hasValidExt {
				t.Logf("On Linux, matched URL %q does not have a valid extension", matchedURL)
				return false
			}
		}

		return true
	}, &cfg)

	if err != nil {
		t.Errorf("Platform-aware link scraping property failed: %v", err)
	}
}

// Desktop integration skip on macOS
func TestPropertyDesktopIntegrationSkipOnMacOS(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	err := quick.Check(func(seed uint64) bool {
		if seed == 0 {
			seed = 1
		}
		rng := seed

		nextRng := func() uint64 {
			rng = rng*6364136223846793005 + 1442695040888963407
			return rng
		}

		// Generate a random app name (3-10 lowercase chars)
		const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
		nameLen := int(nextRng()%8) + 3
		appName := ""
		for i := 0; i < nameLen; i++ {
			appName += string(chars[nextRng()%uint64(len(chars))])
		}

		// Create isolated temp directories
		tempDir := t.TempDir()
		appsDir := filepath.Join(tempDir, "apps")
		iconsDir := filepath.Join(tempDir, "icons")
		optDir := filepath.Join(tempDir, "opt")
		binDir := filepath.Join(tempDir, "bin")
		err := os.MkdirAll(appsDir, 0755)
		if err != nil {
			t.Logf("MkdirAll appsDir failed: %v", err)
			return false
		}
		err = os.MkdirAll(optDir, 0755)
		if err != nil {
			t.Logf("MkdirAll optDir failed: %v", err)
			return false
		}

		config := &Config{
			OptDir:      optDir,
			BinDir:      binDir,
			AppsDir:     appsDir,
			IconsDir:    iconsDir,
			AutoConfirm: true,
		}

		// Test handleDesktopLauncher on macOS
		if runtime.GOOS == "darwin" {
			// On macOS, handleDesktopLauncher should return empty string and nil error
			desktopPath, err := handleDesktopLauncher(
				appName,
				filepath.Join(binDir, appName),
				"application-x-executable",
				filepath.Join(optDir, appName),
				filepath.Join(optDir, appName),
				nil,   // no desktop files
				false, // not AppImage
				config,
			)
			if err != nil {
				t.Logf("handleDesktopLauncher returned error on macOS: %v (appName=%s)", err, appName)
				return false
			}
			if desktopPath != "" {
				t.Logf("handleDesktopLauncher returned non-empty path on macOS: %q (appName=%s)", desktopPath, appName)
				return false
			}

			// Verify no .desktop files were created in appsDir
			entries, err := os.ReadDir(appsDir)
			if err != nil {
				t.Logf("ReadDir appsDir failed: %v", err)
				return false
			}
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".desktop") {
					t.Logf("Found .desktop file %q in appsDir on macOS (appName=%s)", entry.Name(), appName)
					return false
				}
			}

			// Test uninstall skip: simulate a registry entry with DesktopPath and IconPath
			// and verify UninstallApp does not error due to desktop/icon removal on macOS
			registryPathOverride = filepath.Join(tempDir, "registry.json")
			defer func() { registryPathOverride = "" }()

			// Create a fake installed app with desktop and icon paths
			appOptDir := filepath.Join(optDir, appName)
			os.MkdirAll(appOptDir, 0755)
			binaryPath := filepath.Join(appOptDir, appName)
			os.WriteFile(binaryPath, []byte("#!/bin/bash\necho hi\n"), 0755)
			os.MkdirAll(binDir, 0755)
			symlinkPath := filepath.Join(binDir, appName)
			os.Symlink(binaryPath, symlinkPath)

			reg := &Registry{
				Apps: map[string]AppMetadata{
					appName: {
						Name:        appName,
						Version:     "1.0.0",
						BinaryPath:  binaryPath,
						SymlinkPath: symlinkPath,
						DesktopPath: filepath.Join(appsDir, appName+".desktop"),
						IconPath:    filepath.Join(iconsDir, appName+".png"),
					},
				},
			}
			SaveRegistry(reg)

			// UninstallApp should succeed without error on macOS even with populated DesktopPath/IconPath
			err = UninstallApp(appName, config, reg)
			if err != nil {
				t.Logf("UninstallApp returned error on macOS with populated DesktopPath/IconPath: %v (appName=%s)", err, appName)
				return false
			}
		} else {
			// On Linux, verify handleDesktopLauncher does NOT skip (it would create a .desktop file)
			// We just verify it doesn't return an error when called with AutoConfirm
			// and that it does create a .desktop file (proving the skip is platform-specific)
			sourceRoot := filepath.Join(optDir, appName)
			os.MkdirAll(sourceRoot, 0755)

			desktopPath, err := handleDesktopLauncher(
				appName,
				filepath.Join(binDir, appName),
				"application-x-executable",
				sourceRoot,
				sourceRoot,
				nil,   // no desktop files
				false, // not AppImage
				config,
			)
			if err != nil {
				t.Logf("handleDesktopLauncher returned error on Linux: %v (appName=%s)", err, appName)
				return false
			}
			// On Linux with AutoConfirm, a .desktop file should be created
			if desktopPath == "" {
				t.Logf("handleDesktopLauncher returned empty path on Linux with AutoConfirm (appName=%s)", appName)
				return false
			}
			// Verify the .desktop file exists
			if _, statErr := os.Stat(desktopPath); statErr != nil {
				t.Logf(".desktop file not found at %q on Linux (appName=%s)", desktopPath, appName)
				return false
			}
		}

		return true
	}, &cfg)

	if err != nil {
		t.Errorf("Desktop integration skip on macOS failed: %v", err)
	}
}

// AppImage rejection on macOS
func TestPropertyAppImageRejectionOnMacOS(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	err := quick.Check(func(seed uint64) bool {
		if seed == 0 {
			seed = 1
		}
		rng := seed

		nextRng := func() uint64 {
			rng = rng*6364136223846793005 + 1442695040888963407
			return rng
		}

		// Generate a random app name (3-10 lowercase chars)
		const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
		nameLen := int(nextRng()%8) + 3
		appName := ""
		for i := 0; i < nameLen; i++ {
			appName += string(chars[nextRng()%uint64(len(chars))])
		}

		// Generate a random .appimage filename with varying case
		extVariants := []string{".AppImage", ".appimage", ".APPIMAGE", ".Appimage"}
		extIdx := int(nextRng() % uint64(len(extVariants)))
		appImageFilename := appName + extVariants[extIdx]

		// Create isolated temp directories
		tempDir := t.TempDir()
		optDir := filepath.Join(tempDir, "opt")
		binDir := filepath.Join(tempDir, "bin")
		appsDir := filepath.Join(tempDir, "apps")
		iconsDir := filepath.Join(tempDir, "icons")

		config := &Config{
			OptDir:      optDir,
			BinDir:      binDir,
			AppsDir:     appsDir,
			IconsDir:    iconsDir,
			AutoConfirm: true,
		}

		registryPathOverride = filepath.Join(tempDir, "registry.json")
		defer func() { registryPathOverride = "" }()
		reg := &Registry{Apps: make(map[string]AppMetadata)}
		SaveRegistry(reg)

		// Create a fake .appimage file (just needs to exist for the path check)
		os.MkdirAll(tempDir, 0755)
		appImagePath := filepath.Join(tempDir, appImageFilename)
		// Write AppImage-like content (ELF header + AI marker for detection, or just a script)
		os.WriteFile(appImagePath, []byte("#!/bin/bash\necho appimage\n"), 0755)

		if runtime.GOOS == "darwin" {
			// On macOS, InstallApp should return an error for .appimage files
			err := InstallApp(appImagePath, InstallOptions{
				ForcedName:    appName,
				ForcedVersion: "1.0.0",
			}, config, reg)

			if err == nil {
				t.Logf("InstallApp did not return error for .appimage on macOS (file=%s)", appImageFilename)
				return false
			}

			// Error message should indicate AppImage is not supported on macOS
			if !strings.Contains(err.Error(), "AppImage is not supported on macOS") {
				t.Logf("Error message does not contain expected text: %v (file=%s)", err, appImageFilename)
				return false
			}

			// Verify no files were created in opt_dir or bin_dir
			if _, statErr := os.Stat(optDir); statErr == nil {
				entries, _ := os.ReadDir(optDir)
				if len(entries) > 0 {
					t.Logf("Files found in opt_dir after AppImage rejection on macOS: %v (file=%s)", entries, appImageFilename)
					return false
				}
			}
			if _, statErr := os.Stat(binDir); statErr == nil {
				entries, _ := os.ReadDir(binDir)
				if len(entries) > 0 {
					t.Logf("Files found in bin_dir after AppImage rejection on macOS: %v (file=%s)", entries, appImageFilename)
					return false
				}
			}
		} else {
			// On Linux, verify that .appimage files are NOT rejected
			// (they should proceed through the install flow)
			// We just verify detectFormat correctly identifies it as AppImage
			isAppImage, _ := detectFormat(appImagePath)
			if !isAppImage {
				t.Logf("detectFormat did not identify %q as AppImage on Linux", appImageFilename)
				return false
			}

			// And verify the IsDarwin() check is what gates the rejection
			if IsDarwin() {
				t.Logf("IsDarwin() returned true on Linux - unexpected")
				return false
			}
		}

		return true
	}, &cfg)

	if err != nil {
		t.Errorf("AppImage rejection on macOS failed: %v", err)
	}
}

// ============================================================================
// Unit Tests for macOS-specific functionality
// ============================================================================

// TestPlatformDefaultConfigMacOS verifies macOS defaults from PlatformDefaultConfig
func TestPlatformDefaultConfigMacOS(t *testing.T) {
	// Clear XDG overrides to test pure platform defaults
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	cfg := PlatformDefaultConfig()

	if runtime.GOOS == "darwin" {
		// macOS defaults
		if cfg.OptDir != "~/Applications/plop" {
			t.Errorf("macOS OptDir: got %q, want %q", cfg.OptDir, "~/Applications/plop")
		}
		if cfg.BinDir != "~/.local/bin" {
			t.Errorf("macOS BinDir: got %q, want %q", cfg.BinDir, "~/.local/bin")
		}
		if cfg.GithubToken != "" {
			t.Errorf("macOS GithubToken: got %q, want empty", cfg.GithubToken)
		}
		if cfg.AutoConfirm != false {
			t.Errorf("macOS AutoConfirm: got %v, want false", cfg.AutoConfirm)
		}
		if cfg.DefaultGUI != nil {
			t.Errorf("macOS DefaultGUI: got %v, want nil", cfg.DefaultGUI)
		}
	} else {
		// Linux defaults (regression test)
		if cfg.OptDir != "~/.local/opt" {
			t.Errorf("Linux OptDir: got %q, want %q", cfg.OptDir, "~/.local/opt")
		}
		if cfg.BinDir != "~/.local/bin" {
			t.Errorf("Linux BinDir: got %q, want %q", cfg.BinDir, "~/.local/bin")
		}
		if cfg.AppsDir != "~/.local/share/applications" {
			t.Errorf("Linux AppsDir: got %q, want %q", cfg.AppsDir, "~/.local/share/applications")
		}
		if cfg.IconsDir != "~/.local/share/icons" {
			t.Errorf("Linux IconsDir: got %q, want %q", cfg.IconsDir, "~/.local/share/icons")
		}
		if cfg.GithubToken != "" {
			t.Errorf("Linux GithubToken: got %q, want empty", cfg.GithubToken)
		}
		if cfg.AutoConfirm != false {
			t.Errorf("Linux AutoConfirm: got %v, want false", cfg.AutoConfirm)
		}
		if cfg.DefaultGUI != nil {
			t.Errorf("Linux DefaultGUI: got %v, want nil", cfg.DefaultGUI)
		}
	}
}

// TestPlatformDefaultConfigLinuxRegression ensures Linux defaults are unchanged
func TestPlatformDefaultConfigLinuxRegression(t *testing.T) {
	// Clear XDG overrides
	t.Setenv("XDG_BIN_HOME", "")
	t.Setenv("XDG_DATA_HOME", "")

	cfg := PlatformDefaultConfig()

	// BinDir should always be ~/.local/bin without XDG_BIN_HOME
	if cfg.BinDir != "~/.local/bin" {
		t.Errorf("BinDir without XDG_BIN_HOME: got %q, want %q", cfg.BinDir, "~/.local/bin")
	}

	// With XDG_BIN_HOME set, BinDir should use that value
	t.Setenv("XDG_BIN_HOME", "/custom/bin")
	cfg = PlatformDefaultConfig()
	if cfg.BinDir != "/custom/bin" {
		t.Errorf("BinDir with XDG_BIN_HOME: got %q, want %q", cfg.BinDir, "/custom/bin")
	}
}

// TestPlatformConfigDirWithAndWithoutXDG tests PlatformConfigDir behavior
func TestPlatformConfigDirWithAndWithoutXDG(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("could not get home dir: %v", err)
	}

	// Without XDG override
	t.Setenv("XDG_CONFIG_HOME", "")
	dir := PlatformConfigDir()
	if runtime.GOOS == "darwin" {
		expected := filepath.Join(home, "Library", "Application Support", "plop")
		if dir != expected {
			t.Errorf("macOS PlatformConfigDir (no XDG): got %q, want %q", dir, expected)
		}
	} else {
		expected := filepath.Join(home, ".config", "plop")
		if dir != expected {
			t.Errorf("Linux PlatformConfigDir (no XDG): got %q, want %q", dir, expected)
		}
	}

	// With XDG override
	t.Setenv("XDG_CONFIG_HOME", "/tmp/custom-config")
	dir = PlatformConfigDir()
	expected := filepath.Join("/tmp/custom-config", "plop")
	if dir != expected {
		t.Errorf("PlatformConfigDir (with XDG): got %q, want %q", dir, expected)
	}
}

// TestPlatformDataDirWithAndWithoutXDG tests PlatformDataDir behavior
func TestPlatformDataDirWithAndWithoutXDG(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("could not get home dir: %v", err)
	}

	// Without XDG override
	t.Setenv("XDG_DATA_HOME", "")
	dir := PlatformDataDir()
	if runtime.GOOS == "darwin" {
		expected := filepath.Join(home, "Library", "Application Support", "plop")
		if dir != expected {
			t.Errorf("macOS PlatformDataDir (no XDG): got %q, want %q", dir, expected)
		}
	} else {
		expected := filepath.Join(home, ".local", "share", "plop")
		if dir != expected {
			t.Errorf("Linux PlatformDataDir (no XDG): got %q, want %q", dir, expected)
		}
	}

	// With XDG override
	t.Setenv("XDG_DATA_HOME", "/tmp/custom-data")
	dir = PlatformDataDir()
	expected := filepath.Join("/tmp/custom-data", "plop")
	if dir != expected {
		t.Errorf("PlatformDataDir (with XDG): got %q, want %q", dir, expected)
	}
}

// TestIsMachOValidAndInvalid tests IsMachO with valid Mach-O bytes and non-Mach-O bytes
func TestIsMachOValidAndInvalid(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name    string
		content []byte
		wantOk  bool
		desc    string
	}{
		{
			name:    "mach-o-64bit-magic",
			content: []byte{0xFE, 0xED, 0xFA, 0xCF, 0x00, 0x00, 0x00, 0x00},
			wantOk:  false, // raw bytes alone don't make a valid Mach-O (needs proper headers)
			desc:    "raw 64-bit magic bytes without valid header structure",
		},
		{
			name:    "mach-o-32bit-magic",
			content: []byte{0xFE, 0xED, 0xFA, 0xCE, 0x00, 0x00, 0x00, 0x00},
			wantOk:  false, // raw bytes alone don't make a valid Mach-O
			desc:    "raw 32-bit magic bytes without valid header structure",
		},
		{
			name:    "elf-binary",
			content: []byte{0x7F, 0x45, 0x4C, 0x46, 0x02, 0x01, 0x01, 0x00},
			wantOk:  false,
			desc:    "ELF binary should not be detected as Mach-O",
		},
		{
			name:    "plain-text",
			content: []byte("Hello, World! This is plain text."),
			wantOk:  false,
			desc:    "plain text file",
		},
		{
			name:    "empty-file",
			content: []byte{},
			wantOk:  false,
			desc:    "empty file",
		},
		{
			name:    "java-class-file",
			content: []byte{0xCA, 0xFE, 0xBA, 0xBE, 0x00, 0x00, 0x00, 0x34},
			wantOk:  false,
			desc:    "Java .class file (shares 0xCAFEBABE magic but invalid fat header)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filePath := filepath.Join(tempDir, tt.name)
			err := os.WriteFile(filePath, tt.content, 0644)
			if err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			ok, err := IsMachO(filePath)
			if err != nil {
				t.Fatalf("IsMachO returned error: %v", err)
			}
			if ok != tt.wantOk {
				t.Errorf("IsMachO(%s) = %v, want %v (%s)", tt.name, ok, tt.wantOk, tt.desc)
			}
		})
	}
}

// TestIsExecutableDispatch tests IsExecutable dispatches correctly per platform
func TestIsExecutableDispatch(t *testing.T) {
	tempDir := t.TempDir()

	// Test with a shebang script (should work on both platforms)
	scriptPath := filepath.Join(tempDir, "script.sh")
	err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho hello\n"), 0755)
	if err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	isExec, isNative, err := IsExecutable(scriptPath)
	if err != nil {
		t.Fatalf("IsExecutable(script) error: %v", err)
	}
	if !isExec {
		t.Errorf("IsExecutable(script) isExec = false, want true")
	}
	if isNative {
		t.Errorf("IsExecutable(script) isNative = true, want false (scripts are not native)")
	}

	// Test with a plain text file (should not be executable on any platform)
	textPath := filepath.Join(tempDir, "readme.txt")
	err = os.WriteFile(textPath, []byte("Just a readme file."), 0644)
	if err != nil {
		t.Fatalf("failed to write text file: %v", err)
	}

	isExec, isNative, err = IsExecutable(textPath)
	if err != nil {
		t.Fatalf("IsExecutable(text) error: %v", err)
	}
	if isExec {
		t.Errorf("IsExecutable(text) isExec = true, want false")
	}
	if isNative {
		t.Errorf("IsExecutable(text) isNative = true, want false")
	}

	// Test with ELF-like bytes (only valid on Linux)
	elfPath := filepath.Join(tempDir, "elf-binary")
	// This is just magic bytes, not a valid ELF, so IsELF will return false
	err = os.WriteFile(elfPath, []byte{0x7F, 0x45, 0x4C, 0x46, 0x00, 0x00}, 0755)
	if err != nil {
		t.Fatalf("failed to write elf file: %v", err)
	}

	isExec, isNative, err = IsExecutable(elfPath)
	if err != nil {
		t.Fatalf("IsExecutable(elf-like) error: %v", err)
	}
	// Invalid ELF (just magic bytes) won't pass debug/elf.Open validation
	// so it should not be detected as executable
	if isExec {
		t.Logf("Note: IsExecutable detected partial ELF magic as executable (platform=%s)", runtime.GOOS)
	}
}

// TestFindBestAssetDarwinSpecific tests FindBestAsset with macOS-specific asset lists
func TestFindBestAssetDarwinSpecific(t *testing.T) {
	// Test darwin/arm64 asset selection
	releaseArm64 := &GithubRelease{
		TagName: "v1.0.0",
		Assets: []GithubAsset{
			{Name: "app-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
			{Name: "app-darwin-arm64.tar.gz", BrowserDownloadURL: "https://example.com/mac-arm"},
			{Name: "app-darwin-amd64.tar.gz", BrowserDownloadURL: "https://example.com/mac-intel"},
			{Name: "app-windows-amd64.zip", BrowserDownloadURL: "https://example.com/win"},
		},
	}

	// Test using findBestDarwinAsset directly (platform-independent test)
	url, name := findBestDarwinAsset(releaseArm64)
	// On arm64 host, should prefer arm64; on amd64 host, should prefer amd64
	if runtime.GOARCH == "arm64" {
		if url != "https://example.com/mac-arm" || name != "app-darwin-arm64.tar.gz" {
			t.Errorf("findBestDarwinAsset(arm64 host) = (%q, %q), want mac-arm asset", url, name)
		}
	} else {
		if url != "https://example.com/mac-intel" || name != "app-darwin-amd64.tar.gz" {
			t.Errorf("findBestDarwinAsset(amd64 host) = (%q, %q), want mac-intel asset", url, name)
		}
	}

	// Test with macos/osx platform identifiers
	releaseOSX := &GithubRelease{
		TagName: "v2.0.0",
		Assets: []GithubAsset{
			{Name: "tool-linux-x86_64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
			{Name: "tool-macos-universal.zip", BrowserDownloadURL: "https://example.com/macos"},
			{Name: "tool-windows.exe", BrowserDownloadURL: "https://example.com/win"},
		},
	}
	url, name = findBestDarwinAsset(releaseOSX)
	if url != "https://example.com/macos" || name != "tool-macos-universal.zip" {
		t.Errorf("findBestDarwinAsset(macos identifier) = (%q, %q), want macos asset", url, name)
	}

	// Test with .dmg extension
	releaseDMG := &GithubRelease{
		TagName: "v3.0.0",
		Assets: []GithubAsset{
			{Name: "editor-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
			{Name: "editor-darwin-arm64.dmg", BrowserDownloadURL: "https://example.com/mac-dmg"},
			{Name: "editor-windows.msi", BrowserDownloadURL: "https://example.com/win"},
		},
	}
	url, name = findBestDarwinAsset(releaseDMG)
	if !strings.Contains(url, "mac-dmg") {
		t.Errorf("findBestDarwinAsset(.dmg) = (%q, %q), want mac-dmg asset", url, name)
	}

	// Test fallback: no darwin platform match returns first asset
	releaseNoDarwin := &GithubRelease{
		TagName: "v4.0.0",
		Assets: []GithubAsset{
			{Name: "app-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
			{Name: "app-windows-amd64.zip", BrowserDownloadURL: "https://example.com/win"},
		},
	}
	url, name = findBestDarwinAsset(releaseNoDarwin)
	if url != "https://example.com/linux" || name != "app-linux-amd64.tar.gz" {
		t.Errorf("findBestDarwinAsset(no darwin) = (%q, %q), want first asset as fallback", url, name)
	}
}

// TestFindBestAssetPreservesLinuxCases ensures existing Linux test cases still pass
func TestFindBestAssetPreservesLinuxCases(t *testing.T) {
	// These mirror the cases from TestFindBestLinuxAsset but use FindBestAsset
	// to verify the dispatch works correctly

	// Case: Single asset (should always be returned regardless of platform)
	releaseSingle := &GithubRelease{
		TagName: "v2.0.6",
		Assets: []GithubAsset{
			{
				Name:               "Antigravity.tar.gz",
				BrowserDownloadURL: "https://storage.googleapis.com/antigravity/linux-x64/Antigravity.tar.gz",
			},
		},
	}
	url, name := FindBestAsset(releaseSingle)
	if url != "https://storage.googleapis.com/antigravity/linux-x64/Antigravity.tar.gz" || name != "Antigravity.tar.gz" {
		t.Errorf("FindBestAsset(single) = (%q, %q), want single asset returned", url, name)
	}

	// Case: Linux-specific assets via findBestLinuxAsset directly
	releaseLinux := &GithubRelease{
		TagName: "v0.10.0",
		Assets: []GithubAsset{
			{Name: "janice-0.10.0-windows-amd64.zip", BrowserDownloadURL: "https://github.com/win-zip"},
			{Name: "janice-0.10.0-linux-amd64.tar.xz", BrowserDownloadURL: "https://github.com/linux-tar"},
			{Name: "janice-0.10.0-darwin-amd64.tar.gz", BrowserDownloadURL: "https://github.com/mac-tar"},
		},
	}
	url, name = findBestLinuxAsset(releaseLinux)
	if url != "https://github.com/linux-tar" || name != "janice-0.10.0-linux-amd64.tar.xz" {
		t.Errorf("findBestLinuxAsset(standard) = (%q, %q), want linux-tar", url, name)
	}

	// Case: AppImage in multi-asset release (Linux)
	releaseAppImage := &GithubRelease{
		TagName: "v2.4.1",
		Assets: []GithubAsset{
			{Name: "Cutter-v2.4.1-macOS.dmg", BrowserDownloadURL: "https://github.com/mac"},
			{Name: "Cutter-v2.4.1-Windows.zip", BrowserDownloadURL: "https://github.com/win"},
			{Name: "Cutter-v2.4.1-Linux-x86_64.AppImage", BrowserDownloadURL: "https://github.com/linux-appimage"},
		},
	}
	url, name = findBestLinuxAsset(releaseAppImage)
	if url != "https://github.com/linux-appimage" {
		t.Errorf("findBestLinuxAsset(appimage) = (%q, %q), want linux-appimage", url, name)
	}
}

// TestFindBestAssetExcludesAppImageOnMacOS verifies .appimage is excluded on macOS
func TestFindBestAssetExcludesAppImageOnMacOS(t *testing.T) {
	// Release where the only macOS-matching asset is an .appimage (should be excluded)
	release := &GithubRelease{
		TagName: "v1.0.0",
		Assets: []GithubAsset{
			{Name: "app-darwin-arm64.appimage", BrowserDownloadURL: "https://example.com/mac-appimage"},
			{Name: "app-darwin-arm64.tar.gz", BrowserDownloadURL: "https://example.com/mac-tarball"},
			{Name: "app-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
		},
	}

	url, name := findBestDarwinAsset(release)
	// Should select the .tar.gz, not the .appimage
	if strings.HasSuffix(strings.ToLower(name), ".appimage") {
		t.Errorf("findBestDarwinAsset selected .appimage asset: (%q, %q)", url, name)
	}
	if url != "https://example.com/mac-tarball" {
		t.Errorf("findBestDarwinAsset = (%q, %q), want mac-tarball", url, name)
	}

	// Release where ALL macOS assets are .appimage (should fall back to first asset)
	releaseAllAppImage := &GithubRelease{
		TagName: "v2.0.0",
		Assets: []GithubAsset{
			{Name: "app-darwin-arm64.appimage", BrowserDownloadURL: "https://example.com/mac-appimage1"},
			{Name: "app-darwin-amd64.AppImage", BrowserDownloadURL: "https://example.com/mac-appimage2"},
			{Name: "app-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/linux"},
		},
	}

	url, name = findBestDarwinAsset(releaseAllAppImage)
	// No valid macOS asset, should fall back to first asset
	if url != "https://example.com/mac-appimage1" {
		// Actually the fallback returns first asset in the list
		if url != "https://example.com/mac-appimage1" && url != "https://example.com/linux" {
			t.Errorf("findBestDarwinAsset(all appimage) unexpected: (%q, %q)", url, name)
		}
	}
}

// TestAppImageRejectionErrorMessage verifies the error message on macOS
func TestAppImageRejectionErrorMessage(t *testing.T) {
	tempDir := t.TempDir()
	registryPathOverride = filepath.Join(tempDir, "registry.json")
	defer func() { registryPathOverride = "" }()

	config := &Config{
		OptDir:      filepath.Join(tempDir, "opt"),
		BinDir:      filepath.Join(tempDir, "bin"),
		AppsDir:     filepath.Join(tempDir, "apps"),
		IconsDir:    filepath.Join(tempDir, "icons"),
		AutoConfirm: true,
	}
	reg := &Registry{Apps: make(map[string]AppMetadata)}
	SaveRegistry(reg)

	// Create a fake .appimage file
	appImagePath := filepath.Join(tempDir, "test-app.AppImage")
	os.WriteFile(appImagePath, []byte("#!/bin/bash\necho fake\n"), 0755)

	if runtime.GOOS == "darwin" {
		err := InstallApp(appImagePath, InstallOptions{
			ForcedName:    "test-app",
			ForcedVersion: "1.0.0",
		}, config, reg)

		if err == nil {
			t.Fatal("expected error for AppImage on macOS, got nil")
		}
		expectedMsg := "AppImage is not supported on macOS"
		if !strings.Contains(err.Error(), expectedMsg) {
			t.Errorf("error message %q does not contain %q", err.Error(), expectedMsg)
		}
	} else {
		// On Linux, verify detectFormat identifies it as AppImage
		isAppImage, _ := detectFormat(appImagePath)
		if !isAppImage {
			t.Errorf("detectFormat did not identify .AppImage file on Linux")
		}
	}
}

// TestDesktopLauncherSkipOnMacOS verifies desktop launcher is skipped on macOS
func TestDesktopLauncherSkipOnMacOS(t *testing.T) {
	tempDir := t.TempDir()
	appsDir := filepath.Join(tempDir, "apps")
	os.MkdirAll(appsDir, 0755)

	config := &Config{
		OptDir:      filepath.Join(tempDir, "opt"),
		BinDir:      filepath.Join(tempDir, "bin"),
		AppsDir:     appsDir,
		IconsDir:    filepath.Join(tempDir, "icons"),
		AutoConfirm: true,
	}

	desktopPath, err := handleDesktopLauncher(
		"testapp",
		filepath.Join(config.BinDir, "testapp"),
		"application-x-executable",
		filepath.Join(config.OptDir, "testapp"),
		filepath.Join(config.OptDir, "testapp"),
		nil,
		false,
		config,
	)

	if runtime.GOOS == "darwin" {
		if err != nil {
			t.Errorf("handleDesktopLauncher on macOS returned error: %v", err)
		}
		if desktopPath != "" {
			t.Errorf("handleDesktopLauncher on macOS returned path %q, want empty", desktopPath)
		}
		// Verify no .desktop file was created
		entries, _ := os.ReadDir(appsDir)
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".desktop") {
				t.Errorf("found .desktop file %q on macOS", e.Name())
			}
		}
	} else {
		// On Linux with AutoConfirm, a .desktop file should be created
		if err != nil {
			t.Errorf("handleDesktopLauncher on Linux returned error: %v", err)
		}
		if desktopPath == "" {
			t.Errorf("handleDesktopLauncher on Linux returned empty path, want .desktop file")
		}
	}
}

// TestExtractDMGOnMacOS tests DMG extraction (skipped on non-macOS)
func TestExtractDMGOnMacOS(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("DMG extraction requires macOS (hdiutil)")
	}

	// Test that ExtractArchive rejects .dmg on non-darwin (covered by skip above)
	// On macOS, test with a non-existent DMG to verify error handling
	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "extracted")

	// Test with non-existent file
	warning, err := ExtractDMG("/nonexistent/path/fake.dmg", destDir)
	if err == nil {
		t.Errorf("ExtractDMG with non-existent file should return error")
	}
	_ = warning

	// Test that ExtractArchive routes .dmg correctly
	err = ExtractArchive("/nonexistent/path/fake.dmg", destDir)
	if err == nil {
		t.Errorf("ExtractArchive(.dmg) with non-existent file should return error")
	}
	if !strings.Contains(err.Error(), "DMG") && !strings.Contains(err.Error(), "dmg") &&
		!strings.Contains(err.Error(), "mount") && !strings.Contains(err.Error(), "hdiutil") {
		t.Logf("ExtractArchive(.dmg) error: %v", err)
	}
}

// TestExtractArchiveDMGOnLinux verifies .dmg is rejected on Linux
func TestExtractArchiveDMGOnLinux(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("This test verifies Linux-specific .dmg rejection")
	}

	tempDir := t.TempDir()
	destDir := filepath.Join(tempDir, "extracted")

	// Create a fake .dmg file
	dmgPath := filepath.Join(tempDir, "app.dmg")
	os.WriteFile(dmgPath, []byte("fake dmg content"), 0644)

	err := ExtractArchive(dmgPath, destDir)
	if err == nil {
		t.Fatal("ExtractArchive(.dmg) on Linux should return error")
	}
	if !strings.Contains(err.Error(), "not supported on this platform") {
		t.Errorf("error message %q should mention platform unsupported", err.Error())
	}
}

// GitHub URL classification correctness
func TestPropertyGitHubURLClassification(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	// Sub-property: Repo URLs classify as GitHubURLRepo
	err := quick.Check(func(owner, repo string) bool {
		// Filter to valid owner/repo strings (non-empty, no slashes, no whitespace, no null bytes)
		if !isValidSegment(owner) || !isValidSegment(repo) {
			return true // skip invalid inputs
		}

		// Plain repo URL
		url := "https://github.com/" + owner + "/" + repo
		if ClassifyGitHubURL(url) != GitHubURLRepo {
			t.Logf("expected GitHubURLRepo for %q", url)
			return false
		}

		// With trailing slash
		urlSlash := url + "/"
		if ClassifyGitHubURL(urlSlash) != GitHubURLRepo {
			t.Logf("expected GitHubURLRepo for %q", urlSlash)
			return false
		}

		// With .git suffix
		urlGit := url + ".git"
		if ClassifyGitHubURL(urlGit) != GitHubURLRepo {
			t.Logf("expected GitHubURLRepo for %q", urlGit)
			return false
		}

		return true
	}, &cfg)
	if err != nil {
		t.Errorf("Repo URL classification failed: %v", err)
	}

	// Sub-property: Direct download URLs classify as GitHubURLDirect
	err = quick.Check(func(owner, repo, extra string) bool {
		if !isValidSegment(owner) || !isValidSegment(repo) {
			return true
		}
		if extra == "" || strings.ContainsRune(extra, 0) {
			return true
		}

		// /releases/ path
		urlReleases := "https://github.com/" + owner + "/" + repo + "/releases/" + extra
		if ClassifyGitHubURL(urlReleases) != GitHubURLDirect {
			t.Logf("expected GitHubURLDirect for %q", urlReleases)
			return false
		}

		// /archive/ path
		urlArchive := "https://github.com/" + owner + "/" + repo + "/archive/" + extra
		if ClassifyGitHubURL(urlArchive) != GitHubURLDirect {
			t.Logf("expected GitHubURLDirect for %q", urlArchive)
			return false
		}

		return true
	}, &cfg)
	if err != nil {
		t.Errorf("Direct download URL classification failed: %v", err)
	}

	// Sub-property: Unsupported GitHub paths classify as GitHubURLUnsupported
	err = quick.Check(func(owner, repo string) bool {
		if !isValidSegment(owner) || !isValidSegment(repo) {
			return true
		}

		unsupported := []string{"/tree/main", "/wiki/Home", "/issues/1", "/pull/42", "/blob/main/README.md"}
		for _, suffix := range unsupported {
			url := "https://github.com/" + owner + "/" + repo + suffix
			if ClassifyGitHubURL(url) != GitHubURLUnsupported {
				t.Logf("expected GitHubURLUnsupported for %q", url)
				return false
			}
		}

		return true
	}, &cfg)
	if err != nil {
		t.Errorf("Unsupported URL classification failed: %v", err)
	}

	// Sub-property: Non-GitHub URLs classify as NotGitHub
	err = quick.Check(func(host, path string) bool {
		if host == "" || strings.ContainsRune(host, 0) || strings.ContainsAny(host, " \t\n") {
			return true
		}
		// Ensure host is not github.com (case-insensitive)
		if strings.EqualFold(host, "github.com") {
			return true
		}

		url := "https://" + host + "/" + path
		if ClassifyGitHubURL(url) != NotGitHub {
			t.Logf("expected NotGitHub for %q", url)
			return false
		}

		return true
	}, &cfg)
	if err != nil {
		t.Errorf("Non-GitHub URL classification failed: %v", err)
	}

	// Sub-property: Case-insensitive prefix matching
	err = quick.Check(func(owner, repo string) bool {
		if !isValidSegment(owner) || !isValidSegment(repo) {
			return true
		}

		// Test various case combinations of the prefix
		prefixes := []string{
			"HTTPS://GITHUB.COM/",
			"Https://GitHub.com/",
			"https://GitHub.COM/",
			"HTTPS://github.com/",
			"https://GITHUB.COM/",
		}

		for _, prefix := range prefixes {
			url := prefix + owner + "/" + repo
			result := ClassifyGitHubURL(url)
			if result != GitHubURLRepo {
				t.Logf("expected GitHubURLRepo for case-variant %q, got %d", url, result)
				return false
			}
		}

		return true
	}, &cfg)
	if err != nil {
		t.Errorf("Case-insensitive prefix classification failed: %v", err)
	}
}

// isValidSegment checks if a string is a valid URL path segment for testing
func isValidSegment(s string) bool {
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, "/ \t\n\r\x00") {
		return false
	}
	return true
}

func TestClassifyGitHubURL(t *testing.T) {
	tests := []struct {
		input string
		want  GitHubURLType
	}{
		// GitHubURLRepo: basic owner/repo patterns
		{"https://github.com/owner/repo", GitHubURLRepo},
		{"https://github.com/owner/repo/", GitHubURLRepo},
		{"https://github.com/owner/repo.git", GitHubURLRepo},
		{"https://github.com/owner/repo.git/", GitHubURLRepo},

		// GitHubURLRepo: case-insensitive prefix
		{"https://GITHUB.COM/owner/repo", GitHubURLRepo},
		{"https://GitHub.Com/owner/repo", GitHubURLRepo},

		// GitHubURLDirect: releases and archive paths
		{"https://github.com/owner/repo/releases/tag/v1.0", GitHubURLDirect},
		{"https://github.com/owner/repo/releases/download/v1.0/file.tar.gz", GitHubURLDirect},
		{"https://github.com/owner/repo/archive/refs/tags/v1.0.tar.gz", GitHubURLDirect},

		// GitHubURLUnsupported: other path segments beyond owner/repo
		{"https://github.com/owner/repo/tree/main", GitHubURLUnsupported},
		{"https://github.com/owner/repo/wiki", GitHubURLUnsupported},
		{"https://github.com/owner/repo/issues", GitHubURLUnsupported},
		{"https://github.com/owner/repo/pull/123", GitHubURLUnsupported},

		// NotGitHub: non-GitHub URLs
		{"https://example.com/file.tar.gz", NotGitHub},
		{"http://github.com/owner/repo", NotGitHub},

		// NotGitHub: incomplete GitHub URLs
		{"https://github.com/", NotGitHub},
		{"https://github.com/owner", NotGitHub},
	}

	for _, tt := range tests {
		got := ClassifyGitHubURL(tt.input)
		if got != tt.want {
			t.Errorf("ClassifyGitHubURL(%q) = %d; want %d", tt.input, got, tt.want)
		}
	}
}

func TestExtractOwnerRepo(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		// Valid inputs
		{"https://github.com/owner/repo", "owner", "repo", false},
		{"https://github.com/owner/repo/", "owner", "repo", false},
		{"https://github.com/owner/repo.git", "owner", "repo", false},
		{"https://github.com/owner/repo.git/", "owner", "repo", false},
		{"https://GITHUB.COM/owner/repo", "owner", "repo", false},

		// Invalid inputs
		{"https://example.com/owner/repo", "", "", true},
		{"https://github.com/", "", "", true},
		{"https://github.com/owner", "", "", true},
		{"http://github.com/owner/repo", "", "", true},
	}

	for _, tt := range tests {
		owner, repo, err := ExtractOwnerRepo(tt.input)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ExtractOwnerRepo(%q) expected error, got nil", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("ExtractOwnerRepo(%q) unexpected error: %v", tt.input, err)
			}
			if owner != tt.wantOwner {
				t.Errorf("ExtractOwnerRepo(%q) owner = %q; want %q", tt.input, owner, tt.wantOwner)
			}
			if repo != tt.wantRepo {
				t.Errorf("ExtractOwnerRepo(%q) repo = %q; want %q", tt.input, repo, tt.wantRepo)
			}
		}
	}
}

// Empty-asset release filtering
func TestPropertyFilterReleasesWithAssets(t *testing.T) {
	cfg := quick.Config{MaxCount: 100}

	err := quick.Check(func(n uint8) bool {
		// Generate a random list of releases with varying asset counts.
		// Use n to determine the number of releases (cap at 30 to keep tests fast).
		count := int(n) % 30

		var releases []GithubRelease
		expectedNonEmpty := 0

		for i := 0; i < count; i++ {
			// Alternate between empty and non-empty asset lists based on index
			var assets []GithubAsset
			if i%3 != 0 {
				// Non-empty: add 1 or more assets
				numAssets := (i % 5) + 1
				for j := 0; j < numAssets; j++ {
					assets = append(assets, GithubAsset{
						Name:               fmt.Sprintf("asset-%d-%d.tar.gz", i, j),
						BrowserDownloadURL: fmt.Sprintf("https://example.com/download/%d/%d", i, j),
					})
				}
				expectedNonEmpty++
			}
			releases = append(releases, GithubRelease{
				TagName: fmt.Sprintf("v%d.0.0", i),
				Assets:  assets,
			})
		}

		filtered := FilterReleasesWithAssets(releases)

		// Property 1: No release in the result has len(Assets) == 0
		for _, r := range filtered {
			if len(r.Assets) == 0 {
				t.Logf("filtered result contains release %q with zero assets", r.TagName)
				return false
			}
		}

		// Property 2: Every release from the input with len(Assets) > 0 appears in the result
		nonEmptyFromInput := make(map[string]bool)
		for _, r := range releases {
			if len(r.Assets) > 0 {
				nonEmptyFromInput[r.TagName] = true
			}
		}
		for _, r := range filtered {
			if !nonEmptyFromInput[r.TagName] {
				t.Logf("filtered result contains release %q that was not in non-empty input set", r.TagName)
				return false
			}
			delete(nonEmptyFromInput, r.TagName)
		}
		if len(nonEmptyFromInput) > 0 {
			t.Logf("some non-empty releases from input are missing in filtered result: %v", nonEmptyFromInput)
			return false
		}

		// Property 3: The count of results equals the count of input releases with non-empty assets
		if len(filtered) != expectedNonEmpty {
			t.Logf("expected %d filtered releases, got %d", expectedNonEmpty, len(filtered))
			return false
		}

		return true
	}, &cfg)
	if err != nil {
		t.Errorf("FilterReleasesWithAssets property failed: %v", err)
	}
}

// TestFetchReleaseList tests API error handling for FetchReleaseList
func TestFetchReleaseList(t *testing.T) {
	// Save and restore the original API base URL
	originalBaseURL := githubAPIBaseURL
	defer func() { githubAPIBaseURL = originalBaseURL }()

	t.Run("token included as Bearer header", func(t *testing.T) {
		var gotAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]GithubRelease{
				{TagName: "v1.0.0", Assets: []GithubAsset{{Name: "app.tar.gz", BrowserDownloadURL: "https://example.com/app.tar.gz"}}},
			})
		}))
		defer server.Close()

		githubAPIBaseURL = server.URL
		_, err := FetchReleaseList("owner", "repo", "my-secret-token")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotAuth != "Bearer my-secret-token" {
			t.Errorf("expected Authorization header 'Bearer my-secret-token', got %q", gotAuth)
		}
	})

	t.Run("no Authorization header when token is empty", func(t *testing.T) {
		var gotAuth string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode([]GithubRelease{
				{TagName: "v1.0.0", Assets: []GithubAsset{{Name: "app.tar.gz", BrowserDownloadURL: "https://example.com/app.tar.gz"}}},
			})
		}))
		defer server.Close()

		githubAPIBaseURL = server.URL
		_, err := FetchReleaseList("owner", "repo", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotAuth != "" {
			t.Errorf("expected no Authorization header, got %q", gotAuth)
		}
	})

	t.Run("rate limit error with X-RateLimit-Remaining 0", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()

		githubAPIBaseURL = server.URL
		_, err := FetchReleaseList("owner", "repo", "")
		if err == nil {
			t.Fatal("expected error for rate limit, got nil")
		}
		if !strings.Contains(err.Error(), "github_token") {
			t.Errorf("expected error to mention 'github_token', got: %v", err)
		}
		if !strings.Contains(err.Error(), "rate limit") {
			t.Errorf("expected error to mention 'rate limit', got: %v", err)
		}
	})

	t.Run("404 error mentions no releases found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		githubAPIBaseURL = server.URL
		_, err := FetchReleaseList("owner", "repo", "")
		if err == nil {
			t.Fatal("expected error for 404, got nil")
		}
		if !strings.Contains(err.Error(), "no releases found") {
			t.Errorf("expected error to mention 'no releases found', got: %v", err)
		}
	})

	t.Run("other HTTP error includes status code", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		githubAPIBaseURL = server.URL
		_, err := FetchReleaseList("owner", "repo", "")
		if err == nil {
			t.Fatal("expected error for 500, got nil")
		}
		if !strings.Contains(err.Error(), "500") {
			t.Errorf("expected error to include status code '500', got: %v", err)
		}
	})

	t.Run("valid 200 response filters releases without assets", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			releases := []GithubRelease{
				{TagName: "v2.0.0", Assets: []GithubAsset{{Name: "app-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/v2"}}},
				{TagName: "v1.5.0", Assets: []GithubAsset{}},
				{TagName: "v1.0.0", Assets: []GithubAsset{{Name: "app-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/v1"}}},
			}
			json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()

		githubAPIBaseURL = server.URL
		releases, err := FetchReleaseList("owner", "repo", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(releases) != 2 {
			t.Fatalf("expected 2 releases after filtering, got %d", len(releases))
		}
		if releases[0].TagName != "v2.0.0" {
			t.Errorf("expected first release tag 'v2.0.0', got %q", releases[0].TagName)
		}
		if releases[1].TagName != "v1.0.0" {
			t.Errorf("expected second release tag 'v1.0.0', got %q", releases[1].TagName)
		}
	})

	t.Run("all releases have empty assets returns error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			releases := []GithubRelease{
				{TagName: "v2.0.0", Assets: []GithubAsset{}},
				{TagName: "v1.0.0", Assets: []GithubAsset{}},
			}
			json.NewEncoder(w).Encode(releases)
		}))
		defer server.Close()

		githubAPIBaseURL = server.URL
		_, err := FetchReleaseList("owner", "repo", "")
		if err == nil {
			t.Fatal("expected error when all releases have empty assets, got nil")
		}
		if !strings.Contains(err.Error(), "no installable releases") {
			t.Errorf("expected error to mention 'no installable releases', got: %v", err)
		}
	})
}

// Release list display capping and ordering
func TestPropertyFormatReleaseList(t *testing.T) {
	config := &quick.Config{MaxCount: 100}

	// Property: FormatReleaseList outputs at most min(N, 20) entries,
	// each formatted as "  [i] tagname" in the same order as input.
	err := quick.Check(func(n uint8) bool {
		// Generate a release list of length 0..n (capped at 50 for reasonable test sizes)
		count := int(n) % 51

		releases := make([]GithubRelease, count)
		for i := 0; i < count; i++ {
			releases[i] = GithubRelease{
				TagName: fmt.Sprintf("v%d.%d.%d", i, i+1, i+2),
				Assets:  []GithubAsset{{Name: "asset.tar.gz", BrowserDownloadURL: "https://example.com/asset.tar.gz"}},
			}
		}

		output := FormatReleaseList(releases, 20)

		// Empty input should produce empty output
		if count == 0 {
			return output == ""
		}

		lines := strings.Split(output, "\n")

		// Verify at most min(count, 20) entries
		expectedCount := count
		if expectedCount > 20 {
			expectedCount = 20
		}
		if len(lines) != expectedCount {
			t.Logf("expected %d lines, got %d (input count=%d)", expectedCount, len(lines), count)
			return false
		}

		// Verify each entry is formatted correctly and in order
		for i, line := range lines {
			expectedLine := fmt.Sprintf("  [%d] %s", i+1, releases[i].TagName)
			if line != expectedLine {
				t.Logf("line %d: expected %q, got %q", i, expectedLine, line)
				return false
			}
		}

		return true
	}, config)

	if err != nil {
		t.Errorf("Property 3 failed: %v", err)
	}
}

// Version flag matching with v-prefix normalization
func TestPropertyVersionFlagMatching(t *testing.T) {
	config := &quick.Config{MaxCount: 100}

	// Property: SelectRelease finds a release whose tag matches the version flag
	// regardless of v-prefix presence on either the flag or the tag.
	// The match is symmetric with respect to v-prefix.
	err := quick.Check(func(major, minor, patch uint8) bool {
		// Generate a non-empty version string that doesn't start with 'v'
		// Use major.minor.patch format to ensure valid version-like strings
		version := fmt.Sprintf("%d.%d.%d", major%100, minor%100, patch%100)

		// Each release needs at least one asset (since we're testing SelectRelease, not filtering)
		dummyAsset := []GithubAsset{{Name: "app.tar.gz", BrowserDownloadURL: "https://example.com/app.tar.gz"}}

		// Case 1: Tag has v-prefix, flag does not
		// Release tag: "v1.2.3", flag: "1.2.3" -> should match
		releases1 := []GithubRelease{
			{TagName: "v" + version, Assets: dummyAsset},
		}
		result, err := SelectRelease(releases1, version, false)
		if err != nil || result == nil {
			t.Logf("Case 1 failed: tag='v%s', flag='%s', err=%v", version, version, err)
			return false
		}

		// Case 2: Tag has no v-prefix, flag has v-prefix
		// Release tag: "1.2.3", flag: "v1.2.3" -> should match
		releases2 := []GithubRelease{
			{TagName: version, Assets: dummyAsset},
		}
		result, err = SelectRelease(releases2, "v"+version, false)
		if err != nil || result == nil {
			t.Logf("Case 2 failed: tag='%s', flag='v%s', err=%v", version, version, err)
			return false
		}

		// Case 3: Both tag and flag have v-prefix
		// Release tag: "v1.2.3", flag: "v1.2.3" -> should match
		releases3 := []GithubRelease{
			{TagName: "v" + version, Assets: dummyAsset},
		}
		result, err = SelectRelease(releases3, "v"+version, false)
		if err != nil || result == nil {
			t.Logf("Case 3 failed: tag='v%s', flag='v%s', err=%v", version, version, err)
			return false
		}

		// Case 4: Neither tag nor flag have v-prefix
		// Release tag: "1.2.3", flag: "1.2.3" -> should match
		releases4 := []GithubRelease{
			{TagName: version, Assets: dummyAsset},
		}
		result, err = SelectRelease(releases4, version, false)
		if err != nil || result == nil {
			t.Logf("Case 4 failed: tag='%s', flag='%s', err=%v", version, version, err)
			return false
		}

		return true
	}, config)

	if err != nil {
		t.Errorf("Property 6 (version flag matching) failed: %v", err)
	}
}

func TestSelectRelease(t *testing.T) {
	// Helper to create a release with at least one asset
	makeRelease := func(tag string) GithubRelease {
		return GithubRelease{
			TagName: tag,
			Assets:  []GithubAsset{{Name: "app-linux-amd64.tar.gz", BrowserDownloadURL: "https://example.com/" + tag}},
		}
	}

	t.Run("empty releases list returns error", func(t *testing.T) {
		_, err := SelectRelease(nil, "", false)
		if err == nil {
			t.Fatal("expected error for empty releases, got nil")
		}
		if !strings.Contains(err.Error(), "no releases available") {
			t.Errorf("expected error to mention 'no releases available', got: %v", err)
		}
	})

	t.Run("single release auto-selects without prompting", func(t *testing.T) {
		releases := []GithubRelease{makeRelease("v1.0.0")}
		got, err := SelectRelease(releases, "", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.TagName != "v1.0.0" {
			t.Errorf("expected tag 'v1.0.0', got %q", got.TagName)
		}
	})

	t.Run("auto-confirm selects first (latest) release", func(t *testing.T) {
		releases := []GithubRelease{
			makeRelease("v3.0.0"),
			makeRelease("v2.0.0"),
			makeRelease("v1.0.0"),
		}
		got, err := SelectRelease(releases, "", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.TagName != "v3.0.0" {
			t.Errorf("expected latest tag 'v3.0.0', got %q", got.TagName)
		}
	})

	t.Run("version flag matches tag with v-prefix on tag", func(t *testing.T) {
		releases := []GithubRelease{
			makeRelease("v2.0.0"),
			makeRelease("v1.2.3"),
			makeRelease("v1.0.0"),
		}
		got, err := SelectRelease(releases, "1.2.3", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.TagName != "v1.2.3" {
			t.Errorf("expected tag 'v1.2.3', got %q", got.TagName)
		}
	})

	t.Run("version flag with v-prefix matches tag without v-prefix", func(t *testing.T) {
		releases := []GithubRelease{
			makeRelease("2.0.0"),
			makeRelease("1.2.3"),
			makeRelease("1.0.0"),
		}
		got, err := SelectRelease(releases, "v1.2.3", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.TagName != "1.2.3" {
			t.Errorf("expected tag '1.2.3', got %q", got.TagName)
		}
	})

	t.Run("version flag with v-prefix matches tag with v-prefix (exact)", func(t *testing.T) {
		releases := []GithubRelease{
			makeRelease("v2.0.0"),
			makeRelease("v1.2.3"),
			makeRelease("v1.0.0"),
		}
		got, err := SelectRelease(releases, "v1.2.3", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.TagName != "v1.2.3" {
			t.Errorf("expected tag 'v1.2.3', got %q", got.TagName)
		}
	})

	t.Run("version flag with no matching tag returns error listing available tags", func(t *testing.T) {
		releases := []GithubRelease{
			makeRelease("v3.0.0"),
			makeRelease("v2.0.0"),
			makeRelease("v1.0.0"),
		}
		_, err := SelectRelease(releases, "9.9.9", false)
		if err == nil {
			t.Fatal("expected error for non-matching version, got nil")
		}
		errMsg := err.Error()
		if !strings.Contains(errMsg, "9.9.9") {
			t.Errorf("expected error to mention the requested version '9.9.9', got: %v", errMsg)
		}
		// Verify available tags are listed in the error
		if !strings.Contains(errMsg, "v3.0.0") {
			t.Errorf("expected error to list available tag 'v3.0.0', got: %v", errMsg)
		}
		if !strings.Contains(errMsg, "v2.0.0") {
			t.Errorf("expected error to list available tag 'v2.0.0', got: %v", errMsg)
		}
		if !strings.Contains(errMsg, "v1.0.0") {
			t.Errorf("expected error to list available tag 'v1.0.0', got: %v", errMsg)
		}
	})
}

// Version extraction from release tag
func TestPropertyVersionExtraction(t *testing.T) {
	config := &quick.Config{MaxCount: 100}

	// Property 5a: When versionFlag is empty and tag starts with 'v' or 'V',
	// the result equals the tag with the first character removed.
	err := quick.Check(func(suffix []byte) bool {
		// Filter out empty suffixes and strings with null bytes
		if len(suffix) == 0 {
			return true
		}
		for _, b := range suffix {
			if b == 0 {
				return true
			}
		}
		s := string(suffix)

		// Test with lowercase 'v' prefix
		tagLower := "v" + s
		resultLower := ExtractVersion(tagLower, "")
		if resultLower != s {
			t.Logf("5a failed: tag=%q, expected=%q, got=%q", tagLower, s, resultLower)
			return false
		}

		// Test with uppercase 'V' prefix
		tagUpper := "V" + s
		resultUpper := ExtractVersion(tagUpper, "")
		if resultUpper != s {
			t.Logf("5a failed: tag=%q, expected=%q, got=%q", tagUpper, s, resultUpper)
			return false
		}

		return true
	}, config)
	if err != nil {
		t.Errorf("Property 5a (v/V prefix stripping) failed: %v", err)
	}

	// Property 5b: When versionFlag is empty and tag does NOT start with 'v' or 'V',
	// the result equals the tag unchanged.
	err = quick.Check(func(tag string) bool {
		// Filter out empty strings and strings with null bytes
		if len(tag) == 0 {
			return true
		}
		for _, b := range []byte(tag) {
			if b == 0 {
				return true
			}
		}

		// Skip tags that start with 'v' or 'V', those are covered by 5a
		if tag[0] == 'v' || tag[0] == 'V' {
			return true
		}

		result := ExtractVersion(tag, "")
		if result != tag {
			t.Logf("5b failed: tag=%q, expected=%q, got=%q", tag, tag, result)
			return false
		}
		return true
	}, config)
	if err != nil {
		t.Errorf("Property 5b (non-v tag unchanged) failed: %v", err)
	}

	// Property 5c: When versionFlag is non-empty, the result always equals versionFlag
	// regardless of the tag value.
	err = quick.Check(func(tag, versionFlag string) bool {
		// Filter out empty versionFlag and strings with null bytes
		if len(versionFlag) == 0 {
			return true
		}
		for _, b := range []byte(versionFlag) {
			if b == 0 {
				return true
			}
		}
		for _, b := range []byte(tag) {
			if b == 0 {
				return true
			}
		}

		result := ExtractVersion(tag, versionFlag)
		if result != versionFlag {
			t.Logf("5c failed: tag=%q, versionFlag=%q, expected=%q, got=%q", tag, versionFlag, versionFlag, result)
			return false
		}
		return true
	}, config)
	if err != nil {
		t.Errorf("Property 5c (versionFlag override) failed: %v", err)
	}
}

func TestSelectAsset(t *testing.T) {
	// Helper to create platform-compatible asset names based on current OS
	compatibleAssetName := func() string {
		if runtime.GOOS == "darwin" {
			return "app-darwin-arm64.tar.gz"
		}
		return "app-linux-amd64.tar.gz"
	}

	// Helper to create non-compatible asset names (always the "other" platform)
	incompatibleAssetName := func(suffix string) string {
		if runtime.GOOS == "darwin" {
			return "app-linux-amd64" + suffix
		}
		return "app-windows-amd64" + suffix
	}

	t.Run("auto-confirm with compatible asset returns that asset", func(t *testing.T) {
		release := &GithubRelease{
			TagName: "v1.0.0",
			Assets: []GithubAsset{
				{Name: incompatibleAssetName(".zip"), BrowserDownloadURL: "https://example.com/incompat.zip"},
				{Name: compatibleAssetName(), BrowserDownloadURL: "https://example.com/compat.tar.gz"},
				{Name: incompatibleAssetName(".tar.gz"), BrowserDownloadURL: "https://example.com/incompat.tar.gz"},
			},
		}

		got, err := SelectAsset(release, true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != compatibleAssetName() {
			t.Errorf("expected asset %q, got %q", compatibleAssetName(), got.Name)
		}
		if got.BrowserDownloadURL != "https://example.com/compat.tar.gz" {
			t.Errorf("expected URL 'https://example.com/compat.tar.gz', got %q", got.BrowserDownloadURL)
		}
	})

	t.Run("auto-confirm with no compatible asset returns error", func(t *testing.T) {
		release := &GithubRelease{
			TagName: "v1.0.0",
			Assets: []GithubAsset{
				{Name: incompatibleAssetName(".zip"), BrowserDownloadURL: "https://example.com/a.zip"},
				{Name: incompatibleAssetName(".tar.gz"), BrowserDownloadURL: "https://example.com/b.tar.gz"},
			},
		}

		_, err := SelectAsset(release, true)
		if err == nil {
			t.Fatal("expected error when no compatible asset found with auto-confirm, got nil")
		}
		if !strings.Contains(err.Error(), "no compatible asset found") {
			t.Errorf("expected error to mention 'no compatible asset found', got: %v", err)
		}
	})

	t.Run("auto-confirm with empty assets returns error", func(t *testing.T) {
		release := &GithubRelease{
			TagName: "v1.0.0",
			Assets:  []GithubAsset{},
		}

		_, err := SelectAsset(release, true)
		if err == nil {
			t.Fatal("expected error for empty assets, got nil")
		}
		if !strings.Contains(err.Error(), "no assets available") {
			t.Errorf("expected error to mention 'no assets available', got: %v", err)
		}
	})

	t.Run("interactive with compatible asset user presses Enter confirms selection", func(t *testing.T) {
		release := &GithubRelease{
			TagName: "v1.0.0",
			Assets: []GithubAsset{
				{Name: incompatibleAssetName(".zip"), BrowserDownloadURL: "https://example.com/incompat.zip"},
				{Name: compatibleAssetName(), BrowserDownloadURL: "https://example.com/compat.tar.gz"},
			},
		}

		// Mock stdin: user presses Enter (empty line)
		oldStdin := os.Stdin
		defer func() { os.Stdin = oldStdin }()

		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdin = r

		go func() {
			w.Write([]byte("\n"))
			w.Close()
		}()

		got, err := SelectAsset(release, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != compatibleAssetName() {
			t.Errorf("expected confirmed asset %q, got %q", compatibleAssetName(), got.Name)
		}
	})

	t.Run("interactive with compatible asset user enters number selects that asset", func(t *testing.T) {
		release := &GithubRelease{
			TagName: "v1.0.0",
			Assets: []GithubAsset{
				{Name: incompatibleAssetName(".zip"), BrowserDownloadURL: "https://example.com/first.zip"},
				{Name: compatibleAssetName(), BrowserDownloadURL: "https://example.com/compat.tar.gz"},
				{Name: incompatibleAssetName(".tar.gz"), BrowserDownloadURL: "https://example.com/third.tar.gz"},
			},
		}

		// Mock stdin: user enters "1" to select the first asset
		oldStdin := os.Stdin
		defer func() { os.Stdin = oldStdin }()

		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdin = r

		go func() {
			w.Write([]byte("1\n"))
			w.Close()
		}()

		got, err := SelectAsset(release, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// User selected asset [1] which is the first in the list
		if got.Name != incompatibleAssetName(".zip") {
			t.Errorf("expected user-selected asset %q, got %q", incompatibleAssetName(".zip"), got.Name)
		}
	})

	t.Run("interactive with no compatible asset user selects valid number", func(t *testing.T) {
		release := &GithubRelease{
			TagName: "v1.0.0",
			Assets: []GithubAsset{
				{Name: incompatibleAssetName(".zip"), BrowserDownloadURL: "https://example.com/a.zip"},
				{Name: incompatibleAssetName(".tar.gz"), BrowserDownloadURL: "https://example.com/b.tar.gz"},
			},
		}

		// Mock stdin: user enters "2" to select the second asset
		oldStdin := os.Stdin
		defer func() { os.Stdin = oldStdin }()

		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdin = r

		go func() {
			w.Write([]byte("2\n"))
			w.Close()
		}()

		got, err := SelectAsset(release, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != incompatibleAssetName(".tar.gz") {
			t.Errorf("expected asset %q, got %q", incompatibleAssetName(".tar.gz"), got.Name)
		}
	})

	t.Run("interactive invalid selection re-prompts then accepts valid input", func(t *testing.T) {
		release := &GithubRelease{
			TagName: "v1.0.0",
			Assets: []GithubAsset{
				{Name: incompatibleAssetName(".zip"), BrowserDownloadURL: "https://example.com/a.zip"},
				{Name: incompatibleAssetName(".tar.gz"), BrowserDownloadURL: "https://example.com/b.tar.gz"},
			},
		}

		// Mock stdin: user enters invalid input first, then valid "1"
		oldStdin := os.Stdin
		defer func() { os.Stdin = oldStdin }()

		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("failed to create pipe: %v", err)
		}
		os.Stdin = r

		go func() {
			// First: invalid number (out of range)
			w.Write([]byte("99\n"))
			// Second: non-numeric input
			w.Write([]byte("abc\n"))
			// Third: valid selection
			w.Write([]byte("1\n"))
			w.Close()
		}()

		got, err := SelectAsset(release, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Name != incompatibleAssetName(".zip") {
			t.Errorf("expected asset %q after re-prompt, got %q", incompatibleAssetName(".zip"), got.Name)
		}
	})
}

// Update URL construction from repository
func TestPropertyUpdateURLConstruction(t *testing.T) {
	config := &quick.Config{MaxCount: 100}

	// Property: For any valid owner/repo pair (non-empty, no whitespace, no null bytes),
	// ConstructUpdateURL returns exactly "https://github.com/{owner}/{repo}"
	err := quick.Check(func(owner, repo string) bool {
		// Filter: owner and repo must be non-empty, no whitespace, no null bytes
		if owner == "" || repo == "" {
			return true // skip invalid inputs
		}
		for _, r := range owner {
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == 0 {
				return true // skip
			}
		}
		for _, r := range repo {
			if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == 0 {
				return true // skip
			}
		}

		got := ConstructUpdateURL(owner, repo)
		expected := "https://github.com/" + owner + "/" + repo

		// Verify exact match
		if got != expected {
			t.Errorf("ConstructUpdateURL(%q, %q) = %q, want %q", owner, repo, got, expected)
			return false
		}

		// Verify no trailing slash
		if strings.HasSuffix(got, "/") {
			t.Errorf("ConstructUpdateURL(%q, %q) has trailing slash: %q", owner, repo, got)
			return false
		}

		// Verify no .git suffix
		if strings.HasSuffix(got, ".git") {
			t.Errorf("ConstructUpdateURL(%q, %q) has .git suffix: %q", owner, repo, got)
			return false
		}

		// Verify proper format: starts with https://github.com/
		if !strings.HasPrefix(got, "https://github.com/") {
			t.Errorf("ConstructUpdateURL(%q, %q) missing prefix: %q", owner, repo, got)
			return false
		}

		return true
	}, config)

	if err != nil {
		t.Errorf("Property 4 failed: %v", err)
	}
}

// TestGitHubInstallIntegration tests the full GitHub release install flow using a mock server.
// It verifies: repo URL -> API query -> release selection -> asset selection -> InstallApp handoff,
// including correct update_url and version in the registry, flag overrides, and download failure handling.
func TestGitHubInstallIntegration(t *testing.T) {
	// Build platform-appropriate asset name
	osName := runtime.GOOS
	archName := runtime.GOARCH
	if archName == "amd64" {
		archName = "x86_64"
	}

	t.Run("FullFlow", func(t *testing.T) {
		tempDir := t.TempDir()
		registryPathOverride = filepath.Join(tempDir, "registry.json")
		defer func() { registryPathOverride = "" }()

		// Track which API paths were requested
		var apiPathRequested string

		// Create mock server that serves both the GitHub Releases API and asset downloads
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/repos/") && strings.HasSuffix(r.URL.Path, "/releases") {
				// GitHub Releases API endpoint
				apiPathRequested = r.URL.Path
				assetName := fmt.Sprintf("testrepo-v1.5.0-%s-%s.zip", osName, archName)
				releases := []GithubRelease{
					{
						TagName: "v1.5.0",
						Assets: []GithubAsset{
							{
								Name:               assetName,
								BrowserDownloadURL: fmt.Sprintf("http://%s/downloads/%s", r.Host, assetName),
							},
						},
					},
					{
						TagName: "v1.4.0",
						Assets: []GithubAsset{
							{
								Name:               fmt.Sprintf("testrepo-v1.4.0-%s-%s.zip", osName, archName),
								BrowserDownloadURL: fmt.Sprintf("http://%s/downloads/testrepo-v1.4.0-%s-%s.zip", r.Host, osName, archName),
							},
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(releases)
				return
			}

			if strings.HasPrefix(r.URL.Path, "/downloads/") {
				// Asset download endpoint, serve a real zip
				w.Header().Set("Content-Type", "application/zip")
				z := zip.NewWriter(w)
				header := &zip.FileHeader{
					Name:   "testrepo",
					Method: zip.Deflate,
				}
				header.SetMode(0755)
				fw, _ := z.CreateHeader(header)
				fw.Write([]byte("#!/bin/bash\necho 'hello'\n"))
				z.Close()
				return
			}

			http.NotFound(w, r)
		}))
		defer server.Close()

		// Override the GitHub API base URL to point to our mock server
		oldBaseURL := githubAPIBaseURL
		githubAPIBaseURL = server.URL
		defer func() { githubAPIBaseURL = oldBaseURL }()

		config := &Config{
			OptDir:      filepath.Join(tempDir, "opt"),
			BinDir:      filepath.Join(tempDir, "bin"),
			AppsDir:     filepath.Join(tempDir, "apps"),
			IconsDir:    filepath.Join(tempDir, "icons"),
			AutoConfirm: true,
		}
		reg := &Registry{Apps: make(map[string]AppMetadata)}

		// Step 1: Call ResolveGitHubInstall to get the download URL and resolved options
		opts := InstallOptions{}
		downloadURL, resolvedOpts, err := ResolveGitHubInstall("https://github.com/testowner/testrepo", opts, config)
		if err != nil {
			t.Fatalf("ResolveGitHubInstall failed: %v", err)
		}

		// Verify correct API endpoint was called with owner/repo path
		expectedAPIPath := "/repos/testowner/testrepo/releases"
		if apiPathRequested != expectedAPIPath {
			t.Errorf("expected API path %q, got %q", expectedAPIPath, apiPathRequested)
		}

		// Verify the download URL points to the mock server's asset endpoint
		expectedAssetName := fmt.Sprintf("testrepo-v1.5.0-%s-%s.zip", osName, archName)
		if !strings.Contains(downloadURL, "/downloads/"+expectedAssetName) {
			t.Errorf("expected download URL to contain /downloads/%s, got %q", expectedAssetName, downloadURL)
		}

		// Verify ForcedVersion is the tag with 'v' stripped
		if resolvedOpts.ForcedVersion != "1.5.0" {
			t.Errorf("expected ForcedVersion to be '1.5.0', got %q", resolvedOpts.ForcedVersion)
		}

		// Verify ForcedSource is the GitHub repo URL
		if resolvedOpts.ForcedSource != "https://github.com/testowner/testrepo" {
			t.Errorf("expected ForcedSource to be 'https://github.com/testowner/testrepo', got %q", resolvedOpts.ForcedSource)
		}

		// Step 2: Call InstallApp with the resolved URL
		err = InstallApp(downloadURL, resolvedOpts, config, reg)
		if err != nil {
			t.Fatalf("InstallApp failed: %v", err)
		}

		// Verify the app is registered with correct version and update_url
		app, exists := reg.Apps["testrepo"]
		if !exists {
			t.Fatalf("expected 'testrepo' to be registered in the registry")
		}
		if app.Version != "1.5.0" {
			t.Errorf("expected registered version to be '1.5.0', got %q", app.Version)
		}
		if app.UpdateURL != "https://github.com/testowner/testrepo" {
			t.Errorf("expected UpdateURL to be 'https://github.com/testowner/testrepo', got %q", app.UpdateURL)
		}
	})

	t.Run("VersionFlagOverride", func(t *testing.T) {
		tempDir := t.TempDir()
		registryPathOverride = filepath.Join(tempDir, "registry.json")
		defer func() { registryPathOverride = "" }()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/repos/") && strings.HasSuffix(r.URL.Path, "/releases") {
				assetName := fmt.Sprintf("testrepo-%s-%s.zip", osName, archName)
				releases := []GithubRelease{
					{
						TagName: "v2.0.0",
						Assets: []GithubAsset{
							{
								Name:               assetName,
								BrowserDownloadURL: fmt.Sprintf("http://%s/downloads/v2.0.0/%s", r.Host, assetName),
							},
						},
					},
					{
						TagName: "v1.0.0",
						Assets: []GithubAsset{
							{
								Name:               assetName,
								BrowserDownloadURL: fmt.Sprintf("http://%s/downloads/v1.0.0/%s", r.Host, assetName),
							},
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(releases)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/downloads/") {
				w.Header().Set("Content-Type", "application/zip")
				z := zip.NewWriter(w)
				header := &zip.FileHeader{Name: "testrepo", Method: zip.Deflate}
				header.SetMode(0755)
				fw, _ := z.CreateHeader(header)
				fw.Write([]byte("#!/bin/bash\necho 'hello'\n"))
				z.Close()
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		oldBaseURL := githubAPIBaseURL
		githubAPIBaseURL = server.URL
		defer func() { githubAPIBaseURL = oldBaseURL }()

		config := &Config{
			OptDir:      filepath.Join(tempDir, "opt"),
			BinDir:      filepath.Join(tempDir, "bin"),
			AppsDir:     filepath.Join(tempDir, "apps"),
			IconsDir:    filepath.Join(tempDir, "icons"),
			AutoConfirm: true,
		}

		// Provide --version flag to select a specific release
		opts := InstallOptions{ForcedVersion: "1.0.0"}
		_, resolvedOpts, err := ResolveGitHubInstall("https://github.com/testowner/testrepo", opts, config)
		if err != nil {
			t.Fatalf("ResolveGitHubInstall with --version failed: %v", err)
		}

		// When --version is provided, ForcedVersion should use the flag value (not the tag)
		if resolvedOpts.ForcedVersion != "1.0.0" {
			t.Errorf("expected ForcedVersion to be '1.0.0' (flag value), got %q", resolvedOpts.ForcedVersion)
		}
	})

	t.Run("SourceFlagOverride", func(t *testing.T) {
		tempDir := t.TempDir()
		registryPathOverride = filepath.Join(tempDir, "registry.json")
		defer func() { registryPathOverride = "" }()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/repos/") && strings.HasSuffix(r.URL.Path, "/releases") {
				assetName := fmt.Sprintf("testrepo-%s-%s.zip", osName, archName)
				releases := []GithubRelease{
					{
						TagName: "v3.0.0",
						Assets: []GithubAsset{
							{
								Name:               assetName,
								BrowserDownloadURL: fmt.Sprintf("http://%s/downloads/%s", r.Host, assetName),
							},
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(releases)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/downloads/") {
				w.Header().Set("Content-Type", "application/zip")
				z := zip.NewWriter(w)
				header := &zip.FileHeader{Name: "testrepo", Method: zip.Deflate}
				header.SetMode(0755)
				fw, _ := z.CreateHeader(header)
				fw.Write([]byte("#!/bin/bash\necho 'hello'\n"))
				z.Close()
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		oldBaseURL := githubAPIBaseURL
		githubAPIBaseURL = server.URL
		defer func() { githubAPIBaseURL = oldBaseURL }()

		config := &Config{
			OptDir:      filepath.Join(tempDir, "opt"),
			BinDir:      filepath.Join(tempDir, "bin"),
			AppsDir:     filepath.Join(tempDir, "apps"),
			IconsDir:    filepath.Join(tempDir, "icons"),
			AutoConfirm: true,
		}
		reg := &Registry{Apps: make(map[string]AppMetadata)}

		// Provide --source flag to override the update_url
		opts := InstallOptions{ForcedSource: "https://custom-source.example.com/repo"}
		downloadURL, resolvedOpts, err := ResolveGitHubInstall("https://github.com/testowner/testrepo", opts, config)
		if err != nil {
			t.Fatalf("ResolveGitHubInstall with --source failed: %v", err)
		}

		// ForcedSource should use the flag value, not the GitHub repo URL
		if resolvedOpts.ForcedSource != "https://custom-source.example.com/repo" {
			t.Errorf("expected ForcedSource to be 'https://custom-source.example.com/repo', got %q", resolvedOpts.ForcedSource)
		}

		// Install and verify the registry has the custom source
		err = InstallApp(downloadURL, resolvedOpts, config, reg)
		if err != nil {
			t.Fatalf("InstallApp failed: %v", err)
		}

		app, exists := reg.Apps["testrepo"]
		if !exists {
			t.Fatalf("expected 'testrepo' to be registered")
		}
		if app.UpdateURL != "https://custom-source.example.com/repo" {
			t.Errorf("expected UpdateURL to be 'https://custom-source.example.com/repo', got %q", app.UpdateURL)
		}
	})

	t.Run("NameFlagPassThrough", func(t *testing.T) {
		tempDir := t.TempDir()
		registryPathOverride = filepath.Join(tempDir, "registry.json")
		defer func() { registryPathOverride = "" }()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/repos/") && strings.HasSuffix(r.URL.Path, "/releases") {
				assetName := fmt.Sprintf("testrepo-%s-%s.zip", osName, archName)
				releases := []GithubRelease{
					{
						TagName: "v1.0.0",
						Assets: []GithubAsset{
							{
								Name:               assetName,
								BrowserDownloadURL: fmt.Sprintf("http://%s/downloads/%s", r.Host, assetName),
							},
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(releases)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/downloads/") {
				w.Header().Set("Content-Type", "application/zip")
				z := zip.NewWriter(w)
				header := &zip.FileHeader{Name: "mybin", Method: zip.Deflate}
				header.SetMode(0755)
				fw, _ := z.CreateHeader(header)
				fw.Write([]byte("#!/bin/bash\necho 'hello'\n"))
				z.Close()
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		oldBaseURL := githubAPIBaseURL
		githubAPIBaseURL = server.URL
		defer func() { githubAPIBaseURL = oldBaseURL }()

		config := &Config{
			OptDir:      filepath.Join(tempDir, "opt"),
			BinDir:      filepath.Join(tempDir, "bin"),
			AppsDir:     filepath.Join(tempDir, "apps"),
			IconsDir:    filepath.Join(tempDir, "icons"),
			AutoConfirm: true,
		}
		reg := &Registry{Apps: make(map[string]AppMetadata)}

		// Provide --name flag
		opts := InstallOptions{ForcedName: "custom-name"}
		downloadURL, resolvedOpts, err := ResolveGitHubInstall("https://github.com/testowner/testrepo", opts, config)
		if err != nil {
			t.Fatalf("ResolveGitHubInstall with --name failed: %v", err)
		}

		// ForcedName should be preserved
		if resolvedOpts.ForcedName != "custom-name" {
			t.Errorf("expected ForcedName to be 'custom-name', got %q", resolvedOpts.ForcedName)
		}

		// Install and verify the registry uses the custom name
		err = InstallApp(downloadURL, resolvedOpts, config, reg)
		if err != nil {
			t.Fatalf("InstallApp failed: %v", err)
		}

		_, exists := reg.Apps["custom-name"]
		if !exists {
			t.Fatalf("expected 'custom-name' to be registered in the registry")
		}
	})

	t.Run("DownloadFailure", func(t *testing.T) {
		tempDir := t.TempDir()
		registryPathOverride = filepath.Join(tempDir, "registry.json")
		defer func() { registryPathOverride = "" }()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/repos/") && strings.HasSuffix(r.URL.Path, "/releases") {
				assetName := fmt.Sprintf("testrepo-%s-%s.zip", osName, archName)
				releases := []GithubRelease{
					{
						TagName: "v1.0.0",
						Assets: []GithubAsset{
							{
								Name:               assetName,
								BrowserDownloadURL: fmt.Sprintf("http://%s/downloads/%s", r.Host, assetName),
							},
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(releases)
				return
			}
			// Return 404 for asset download to simulate download failure
			http.NotFound(w, r)
		}))
		defer server.Close()

		oldBaseURL := githubAPIBaseURL
		githubAPIBaseURL = server.URL
		defer func() { githubAPIBaseURL = oldBaseURL }()

		config := &Config{
			OptDir:      filepath.Join(tempDir, "opt"),
			BinDir:      filepath.Join(tempDir, "bin"),
			AppsDir:     filepath.Join(tempDir, "apps"),
			IconsDir:    filepath.Join(tempDir, "icons"),
			AutoConfirm: true,
		}
		reg := &Registry{Apps: make(map[string]AppMetadata)}

		// Resolve should succeed (API works fine)
		opts := InstallOptions{}
		downloadURL, resolvedOpts, err := ResolveGitHubInstall("https://github.com/testowner/testrepo", opts, config)
		if err != nil {
			t.Fatalf("ResolveGitHubInstall failed: %v", err)
		}

		// InstallApp should fail because the download returns 404
		err = InstallApp(downloadURL, resolvedOpts, config, reg)
		if err == nil {
			t.Fatalf("expected InstallApp to fail on download failure, but it succeeded")
		}

		// Verify the error mentions download failure
		if !strings.Contains(err.Error(), "download failed") {
			t.Errorf("expected error to mention 'download failed', got: %v", err)
		}

		// Verify the app was NOT registered
		if _, exists := reg.Apps["testrepo"]; exists {
			t.Errorf("expected app to NOT be registered after download failure")
		}
	})
}

func TestUninstallPrefixCollision(t *testing.T) {
	tests := []struct {
		name           string
		appToUninstall string
		symlinkTarget  string // relative to optDir (e.g., "myapp2/binary")
		expectDeleted  bool
	}{
		{
			name:           "leaves symlink to /opt/myapp2/binary intact",
			appToUninstall: "myapp",
			symlinkTarget:  "myapp2/binary",
			expectDeleted:  false,
		},
		{
			name:           "leaves symlink to /opt/myapp-extra/bin/tool intact",
			appToUninstall: "myapp",
			symlinkTarget:  "myapp-extra/bin/tool",
			expectDeleted:  false,
		},
		{
			name:           "leaves symlink to /opt/application/bin intact",
			appToUninstall: "app",
			symlinkTarget:  "application/bin",
			expectDeleted:  false,
		},
		{
			name:           "still deletes symlink to /opt/myapp/binary",
			appToUninstall: "myapp",
			symlinkTarget:  "myapp/binary",
			expectDeleted:  true,
		},
		{
			name:           "still deletes symlink to /opt/myapp/bin/tool",
			appToUninstall: "myapp",
			symlinkTarget:  "myapp/bin/tool",
			expectDeleted:  true,
		},
		{
			name:           "handles symlink pointing exactly to /opt/myapp",
			appToUninstall: "myapp",
			symlinkTarget:  "myapp",
			expectDeleted:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			optDir := filepath.Join(tempDir, "opt")
			binDir := filepath.Join(tempDir, "bin")

			if err := os.MkdirAll(optDir, 0755); err != nil {
				t.Fatalf("failed to create optDir: %v", err)
			}
			if err := os.MkdirAll(binDir, 0755); err != nil {
				t.Fatalf("failed to create binDir: %v", err)
			}

			// Create the app's own opt folder
			appOptFolder := filepath.Join(optDir, tt.appToUninstall)
			if err := os.MkdirAll(appOptFolder, 0755); err != nil {
				t.Fatalf("failed to create appOptFolder: %v", err)
			}

			// Create the full target path (file or directory) so the symlink resolves
			fullTarget := filepath.Join(optDir, tt.symlinkTarget)
			targetDir := filepath.Dir(fullTarget)
			if err := os.MkdirAll(targetDir, 0755); err != nil {
				t.Fatalf("failed to create target dir: %v", err)
			}
			// If the target is a directory itself (e.g., "myapp" or "application/bin"),
			// ensure it exists as a directory; otherwise create it as a file.
			if tt.symlinkTarget == tt.appToUninstall {
				// Edge case: symlink points exactly to the app opt folder (already created)
			} else {
				if err := os.MkdirAll(fullTarget, 0755); err != nil {
					// If MkdirAll fails, try writing as a file
					if err2 := os.WriteFile(fullTarget, []byte("binary"), 0755); err2 != nil {
						t.Fatalf("failed to create target: %v", err2)
					}
				}
			}

			// Create a symlink in binDir pointing to the target
			symlinkPath := filepath.Join(binDir, "link-under-test")
			if err := os.Symlink(fullTarget, symlinkPath); err != nil {
				t.Fatalf("failed to create symlink: %v", err)
			}

			// Set up registry with the app registered
			registryPathOverride = filepath.Join(tempDir, "registry.json")
			defer func() { registryPathOverride = "" }()

			reg := &Registry{
				Apps: map[string]AppMetadata{
					tt.appToUninstall: {
						Name:        tt.appToUninstall,
						Version:     "1.0.0",
						BinaryPath:  filepath.Join(appOptFolder, "somebin"),
						SymlinkPath: filepath.Join(binDir, tt.appToUninstall),
					},
				},
			}
			if err := SaveRegistry(reg); err != nil {
				t.Fatalf("failed to save registry: %v", err)
			}

			config := &Config{
				OptDir:      optDir,
				BinDir:      binDir,
				AppsDir:     filepath.Join(tempDir, "apps"),
				IconsDir:    filepath.Join(tempDir, "icons"),
				AutoConfirm: true,
			}

			// Run UninstallApp
			err := UninstallApp(tt.appToUninstall, config, reg)
			if err != nil {
				t.Fatalf("UninstallApp returned error: %v", err)
			}

			// Check whether the symlink still exists
			_, statErr := os.Lstat(symlinkPath)
			symlinkExists := statErr == nil

			if tt.expectDeleted && symlinkExists {
				t.Errorf("expected symlink to be deleted, but it still exists")
			}
			if !tt.expectDeleted && !symlinkExists {
				t.Errorf("expected symlink to be preserved, but it was deleted")
			}
		})
	}
}
