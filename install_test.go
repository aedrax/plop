package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	// Start a local mock HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`
			<html>
				<body>
					<a href="/downloads/app-v1.0.0-linux-amd64.tar.gz">Version 1.0.0</a>
					<a href="/downloads/app-v2.5.1-linux-x86_64.tar.gz">Version 2.5.1</a>
					<a href="/downloads/ignored-mac.zip">Mac Release</a>
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
	if asset.Name != "app-v2.5.1-linux-x86_64.tar.gz" {
		t.Errorf("expected asset name to be app-v2.5.1-linux-x86_64.tar.gz, got %s", asset.Name)
	}
	expectedURL := server.URL + "/downloads/app-v2.5.1-linux-x86_64.tar.gz"
	if asset.BrowserDownloadURL != expectedURL {
		t.Errorf("expected download URL to be %q, got %q", expectedURL, asset.BrowserDownloadURL)
	}
}

func TestScrapeObfuscatedJS(t *testing.T) {
	// Start a local mock HTTP server that simulates SQLite's JavaScript injection
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
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
	expectedURL := server.URL + "/2026/sqlite-tools-linux-x64-3530100.zip"
	if asset.BrowserDownloadURL != expectedURL {
		t.Errorf("expected download URL to be %q, got %q", expectedURL, asset.BrowserDownloadURL)
	}
}

func TestScrapeSPABundles(t *testing.T) {
	// Start a local mock HTTP server that simulates a client-side rendered SPA page
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)

		// If requesting the JS bundle, serve JS content containing the direct download URL
		if strings.HasSuffix(r.URL.Path, "/main.js") {
			w.Write([]byte(`
				const stableLinux = "https://example.com/awesome/2.0.6-123456789/linux-x64/Awesome.tar.gz";
				const otherLinux = "/downloads/2.0.3/Awesome IDE.tar.gz";
			`))
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
	expectedURL := "https://example.com/awesome/2.0.6-123456789/linux-x64/Awesome.tar.gz"
	if asset.BrowserDownloadURL != expectedURL {
		t.Errorf("expected download URL to be %q, got %q", expectedURL, asset.BrowserDownloadURL)
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
		fmt.Fprintf(w, `{
			"tag_name": "v1.6.0",
			"assets": [
				{
					"name": "custom-app-linux-x64-1.6.0.zip",
					"browser_download_url": "%s/custom-app-linux-x64-1.6.0.zip"
				}
			]
		}`, serverURLToHost(r))
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
	url, name := FindBestLinuxAsset(releaseSingle)
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
	url, name = FindBestLinuxAsset(releaseAppImage)
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
	url, name = FindBestLinuxAsset(releaseMakeself)
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
	url, name = FindBestLinuxAsset(releaseStandard)
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
