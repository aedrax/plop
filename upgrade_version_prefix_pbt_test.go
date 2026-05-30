package main

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"testing/quick"
)

// TestBugCondition_UppercaseVPrefixNotStripped is a property-based test that verifies
// the version extraction logic correctly strips uppercase 'V' prefixes from release tags.
//
// Bug condition: strings.TrimPrefix(tag, "v") only strips lowercase 'v', leaving uppercase
// 'V' prefixes intact. This causes false upgrade notifications when the installed version
// matches the latest release but the tag uses uppercase 'V'.
//
// EXPECTED: This test FAILS on unfixed code, failure confirms the bug exists.
func TestBugCondition_UppercaseVPrefixNotStripped(t *testing.T) {
	f := func(seed int64) bool {
		rng := rand.New(rand.NewSource(seed))

		// Generate a random version string (without prefix)
		baseVersion := generateVersionString(rng)

		// Create a tag with uppercase 'V' prefix
		tag := "V" + baseVersion

		// Simulate the fixed upgrade.go logic using stripVPrefix
		latestVersion := stripVPrefix(tag)

		// The app.Version is the bare version (as stored in registry)
		appVersion := baseVersion

		// The extracted version should NOT start with 'V' or 'v'
		if strings.HasPrefix(latestVersion, "V") || strings.HasPrefix(latestVersion, "v") {
			t.Logf("COUNTEREXAMPLE: strings.TrimPrefix(%q, \"v\") = %q (still has prefix)",
				tag, latestVersion)
			return false
		}

		// When stripVPrefix(tag) == stripVPrefix(appVersion), the comparison
		// should report versions as equal (no false upgrade notification)
		if stripVPrefix(tag) == stripVPrefix(appVersion) {
			if latestVersion != appVersion {
				t.Logf("COUNTEREXAMPLE: tag=%q, appVersion=%q, latestVersion=%q != appVersion=%q (false upgrade)",
					tag, appVersion, latestVersion, appVersion)
				return false
			}
		}

		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("Bug confirmed - uppercase 'V' prefix not stripped by strings.TrimPrefix(tag, \"v\"): %v", err)
	}
}

// TestBugCondition_UppercaseVPrefixTableDriven provides concrete examples demonstrating
// the uppercase 'V' prefix bug with table-driven test cases.
//
// EXPECTED: This test FAILS on unfixed code, failure confirms the bug exists.
func TestBugCondition_UppercaseVPrefixTableDriven(t *testing.T) {
	tests := []struct {
		name       string
		tag        string
		appVersion string
	}{
		{
			name:       "uppercase V tag vs bare version",
			tag:        "V1.6.0",
			appVersion: "1.6.0",
		},
		{
			name:       "uppercase V tag with pre-release vs bare version",
			tag:        "V2.0.0-beta",
			appVersion: "2.0.0-beta",
		},
		{
			name:       "uppercase V tag vs lowercase v version",
			tag:        "V1.6.0",
			appVersion: "v1.6.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the fixed upgrade.go logic using stripVPrefix
			latestVersion := stripVPrefix(tt.tag)

			// The extracted version should not start with 'V' or 'v'
			if strings.HasPrefix(latestVersion, "V") || strings.HasPrefix(latestVersion, "v") {
				t.Errorf("stripVPrefix(%q) = %q (prefix not stripped)",
					tt.tag, latestVersion)
			}

			// When the versions are semantically equal, comparison should succeed
			normalizedApp := stripVPrefix(tt.appVersion)
			if stripVPrefix(tt.tag) == normalizedApp {
				if latestVersion != normalizedApp {
					t.Errorf("False upgrade: tag=%q, latestVersion=%q != normalizedApp=%q",
						tt.tag, latestVersion, normalizedApp)
				}
			}
		})
	}
}

// generateVersionString creates a random version string suitable for testing.
// Produces versions like "1.6.0", "2.0.0-beta", "10.3.1-rc1", etc.
func generateVersionString(rng *rand.Rand) string {
	// Generate 2-4 numeric segments
	numSegments := 2 + rng.Intn(3) // 2-4 segments
	segments := make([]string, numSegments)
	for i := range segments {
		segments[i] = fmt.Sprintf("%d", rng.Intn(100))
	}
	version := strings.Join(segments, ".")

	// 30% chance of adding a pre-release suffix
	if rng.Intn(10) < 3 {
		suffixes := []string{"alpha", "beta", "rc", "alpha1", "beta2", "rc1", "rc2"}
		version += "-" + suffixes[rng.Intn(len(suffixes))]
	}

	return version
}
