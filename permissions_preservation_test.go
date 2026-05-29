package main

import (
	"encoding/json"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"testing/quick"
)

// randString generates a random alphanumeric string for test data.
func randString(r *rand.Rand, maxLen int) string {
	n := r.Intn(maxLen + 1)
	buf := make([]byte, n)
	for i := range buf {
		buf[i] = byte('a' + r.Intn(26))
	}
	return string(buf)
}

// randConfig generates a random Config struct for property testing.
func randConfig(r *rand.Rand) Config {
	cfg := Config{
		OptDir:      randString(r, 20),
		BinDir:      randString(r, 20),
		AppsDir:     randString(r, 20),
		IconsDir:    randString(r, 20),
		GithubToken: randString(r, 30),
		AutoConfirm: r.Intn(2) == 1,
	}
	if r.Intn(2) == 1 {
		val := r.Intn(2) == 1
		cfg.DefaultGUI = &val
	}
	return cfg
}

// randAppMetadata generates a random AppMetadata struct for property testing.
func randAppMetadata(r *rand.Rand) AppMetadata {
	numBins := r.Intn(4)
	bins := make([]string, numBins)
	for i := range bins {
		bins[i] = randString(r, 10)
	}
	return AppMetadata{
		Name:             randString(r, 15),
		Version:          randString(r, 10),
		InstalledAt:      randString(r, 20),
		ArchiveSource:    randString(r, 30),
		UpdateURL:        randString(r, 30),
		BinaryPath:       randString(r, 25),
		SymlinkPath:      randString(r, 25),
		DesktopPath:      randString(r, 25),
		IconPath:         randString(r, 25),
		InstallScript:    randString(r, 20),
		SelectedBinaries: bins,
	}
}

// randRegistry generates a random Registry struct for property testing.
func randRegistry(r *rand.Rand) Registry {
	numApps := r.Intn(5) + 1
	apps := make(map[string]AppMetadata, numApps)
	for i := 0; i < numApps; i++ {
		key := randString(r, 10)
		if key == "" {
			key = "app"
		}
		apps[key] = randAppMetadata(r)
	}
	return Registry{Apps: apps}
}

// TestPreservation_ConfigJSONRoundTrip verifies that Config structs serialize
// to JSON and can be read back with byte-identical content.
func TestPreservation_ConfigJSONRoundTrip(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		cfg := randConfig(r)

		// Serialize the same way LoadConfig does
		data, err := json.MarshalIndent(&cfg, "", "  ")
		if err != nil {
			t.Logf("MarshalIndent failed: %v", err)
			return false
		}

		// Write to a temp file
		tempDir := t.TempDir()
		filePath := filepath.Join(tempDir, "config.json")
		err = os.WriteFile(filePath, data, 0644)
		if err != nil {
			t.Logf("WriteFile failed: %v", err)
			return false
		}

		// Read back
		readData, err := os.ReadFile(filePath)
		if err != nil {
			t.Logf("ReadFile failed: %v", err)
			return false
		}

		// Verify byte-identical content
		if string(data) != string(readData) {
			t.Logf("Content mismatch: written %d bytes, read %d bytes", len(data), len(readData))
			return false
		}

		// Verify JSON can be unmarshaled back to equivalent struct
		var readCfg Config
		err = json.Unmarshal(readData, &readCfg)
		if err != nil {
			t.Logf("Unmarshal failed: %v", err)
			return false
		}

		// Re-serialize and compare to ensure round-trip fidelity
		reData, err := json.MarshalIndent(&readCfg, "", "  ")
		if err != nil {
			t.Logf("Re-MarshalIndent failed: %v", err)
			return false
		}

		if string(data) != string(reData) {
			t.Logf("Round-trip content mismatch")
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Config JSON round-trip property failed: %v", err)
	}
}

// TestPreservation_RegistryJSONRoundTrip verifies that Registry structs saved
// via SaveRegistry can be read back with identical JSON content.
func TestPreservation_RegistryJSONRoundTrip(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		reg := randRegistry(r)

		// Use temp dir for registry isolation
		tempDir := t.TempDir()
		registryPathOverride = filepath.Join(tempDir, "registry.json")
		defer func() { registryPathOverride = "" }()

		// Save via SaveRegistry (uses json.MarshalIndent with same format)
		err := SaveRegistry(&reg)
		if err != nil {
			t.Logf("SaveRegistry failed: %v", err)
			return false
		}

		// Read back the file
		readData, err := os.ReadFile(registryPathOverride)
		if err != nil {
			t.Logf("ReadFile failed: %v", err)
			return false
		}

		// Verify JSON can be unmarshaled back
		var readReg Registry
		err = json.Unmarshal(readData, &readReg)
		if err != nil {
			t.Logf("Unmarshal failed: %v", err)
			return false
		}

		// Re-serialize and compare to verify round-trip fidelity
		expectedData, err := json.MarshalIndent(&reg, "", "  ")
		if err != nil {
			t.Logf("MarshalIndent failed: %v", err)
			return false
		}

		if string(expectedData) != string(readData) {
			t.Logf("Registry content mismatch:\nexpected: %s\ngot: %s", string(expectedData), string(readData))
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 100}); err != nil {
		t.Errorf("Registry JSON round-trip property failed: %v", err)
	}
}

// TestPreservation_DirectoryPermissions verifies that directories created by
// LoadConfig have 0755 permissions.
func TestPreservation_DirectoryPermissions(t *testing.T) {
	f := func(seed int64) bool {
		tempDir := t.TempDir()

		// Isolate config path
		configDir := filepath.Join(tempDir, "config")
		t.Setenv("XDG_CONFIG_HOME", configDir)

		// Isolate data path
		dataDir := filepath.Join(tempDir, "data")
		t.Setenv("XDG_DATA_HOME", dataDir)

		// Isolate registry
		registryPathOverride = filepath.Join(tempDir, "registry.json")
		defer func() { registryPathOverride = "" }()

		// Call LoadConfig which creates the config directory
		_, err := LoadConfig()
		if err != nil {
			t.Logf("LoadConfig failed: %v", err)
			return false
		}

		// Verify config directory has 0755 permissions
		plopConfigDir := filepath.Join(configDir, "plop")
		info, err := os.Stat(plopConfigDir)
		if err != nil {
			t.Logf("Stat config dir failed: %v", err)
			return false
		}
		if !info.IsDir() {
			t.Logf("Config path is not a directory")
			return false
		}
		perm := info.Mode().Perm()
		if perm != 0755 {
			t.Logf("Config directory has permissions %04o, expected 0755", perm)
			return false
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 20}); err != nil {
		t.Errorf("Directory permissions property failed: %v", err)
	}
}

// TestPreservation_OwnerAccess verifies that the file owner retains read/write
// access after config and registry operations.
func TestPreservation_OwnerAccess(t *testing.T) {
	f := func(seed int64) bool {
		r := rand.New(rand.NewSource(seed))
		tempDir := t.TempDir()

		// Isolate config path
		configDir := filepath.Join(tempDir, "config")
		t.Setenv("XDG_CONFIG_HOME", configDir)

		// Isolate data path
		dataDir := filepath.Join(tempDir, "data")
		t.Setenv("XDG_DATA_HOME", dataDir)

		// Isolate registry
		regPath := filepath.Join(tempDir, "registry.json")
		registryPathOverride = regPath
		defer func() { registryPathOverride = "" }()

		// Create config file via LoadConfig
		_, err := LoadConfig()
		if err != nil {
			t.Logf("LoadConfig failed: %v", err)
			return false
		}

		// Verify owner can open config file with read/write
		configPath := filepath.Join(configDir, "plop", "config.json")
		file, err := os.OpenFile(configPath, os.O_RDWR, 0)
		if err != nil {
			t.Logf("Owner cannot open config file for read/write: %v", err)
			return false
		}
		file.Close()

		// Save a random registry
		reg := randRegistry(r)
		err = SaveRegistry(&reg)
		if err != nil {
			t.Logf("SaveRegistry failed: %v", err)
			return false
		}

		// Verify owner can open registry file with read/write
		file, err = os.OpenFile(regPath, os.O_RDWR, 0)
		if err != nil {
			t.Logf("Owner cannot open registry file for read/write: %v", err)
			return false
		}
		file.Close()

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 50}); err != nil {
		t.Errorf("Owner access property failed: %v", err)
	}
}
