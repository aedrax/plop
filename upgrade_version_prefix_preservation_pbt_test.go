package main

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"testing/quick"
)

// TestPreservation_LowercaseAndNoPrefixBehavior_TableDriven verifies concrete
// examples of preservation behavior: lowercase 'v' prefix and no-prefix tags
// produce the same result with stripVPrefix as with strings.TrimPrefix(tag, "v").
func TestPreservation_LowercaseAndNoPrefixBehavior_TableDriven(t *testing.T) {
	// Table-driven cases for stripVPrefix equivalence with TrimPrefix for non-buggy inputs
	stripCases := []struct {
		input    string
		expected string
	}{
		{"v1.6.0", "1.6.0"},
		{"v2.0.0", "2.0.0"},
		{"v0.1.0-beta", "0.1.0-beta"},
		{"1.6.0", "1.6.0"},
		{"2.0.0", "2.0.0"},
		{"0.0.1", "0.0.1"},
		{"v10.20.30", "10.20.30"},
	}

	for _, tc := range stripCases {
		t.Run(fmt.Sprintf("strip_%s", tc.input), func(t *testing.T) {
			got := stripVPrefix(tc.input)
			trimGot := strings.TrimPrefix(tc.input, "v")
			if got != tc.expected {
				t.Errorf("stripVPrefix(%q) = %q, want %q", tc.input, got, tc.expected)
			}
			if got != trimGot {
				t.Errorf("stripVPrefix(%q) = %q, but TrimPrefix = %q, should be identical for lowercase/no-prefix", tc.input, got, trimGot)
			}
		})
	}

	// Table-driven cases for genuine upgrade detection preservation
	upgradeCases := []struct {
		tag        string
		appVersion string
		wantEqual  bool
	}{
		{"v1.6.0", "1.6.0", true},  // same version, lowercase prefix
		{"1.6.0", "1.6.0", true},   // same version, no prefix
		{"v2.0.0", "1.5.0", false}, // genuine upgrade
		{"v1.0.0", "0.9.0", false}, // genuine upgrade
		{"v3.1.4", "3.1.4", true},  // same version
		{"2.0.0", "1.0.0", false},  // genuine upgrade, no prefix
	}

	for _, tc := range upgradeCases {
		t.Run(fmt.Sprintf("compare_%s_vs_%s", tc.tag, tc.appVersion), func(t *testing.T) {
			// Simulate the current upgrade.go logic (unfixed)
			latestVersion := strings.TrimPrefix(tc.tag, "v")
			originalEqual := (latestVersion == tc.appVersion)

			// The fixed logic uses stripVPrefix
			fixedLatest := stripVPrefix(tc.tag)
			fixedEqual := (fixedLatest == stripVPrefix(tc.appVersion))

			// For non-buggy inputs (lowercase 'v' or no prefix), both should agree
			if originalEqual != fixedEqual {
				t.Errorf("tag=%q appVersion=%q: original says equal=%v, fixed says equal=%v, should agree for non-buggy inputs",
					tc.tag, tc.appVersion, originalEqual, fixedEqual)
			}
			if fixedEqual != tc.wantEqual {
				t.Errorf("tag=%q appVersion=%q: got equal=%v, want equal=%v",
					tc.tag, tc.appVersion, fixedEqual, tc.wantEqual)
			}
		})
	}
}

// generateRandomVersionSegments creates a random version string with 1-4 numeric segments.
func generateRandomVersionSegments(r *rand.Rand) string {
	numSegments := r.Intn(4) + 1
	segments := make([]string, numSegments)
	for i := range segments {
		segments[i] = fmt.Sprintf("%d", r.Intn(100))
	}
	return strings.Join(segments, ".")
}

// generateLowercaseOrNoPrefixTag generates a random version string with either
// a lowercase 'v' prefix or no prefix (never uppercase 'V').
func generateLowercaseOrNoPrefixTag(r *rand.Rand) string {
	version := generateRandomVersionSegments(r)
	if r.Intn(2) == 0 {
		return "v" + version
	}
	return version
}

// TestPreservation_StripVPrefixEquivalence_Property verifies that for all tags
// with lowercase 'v' prefix or no prefix, stripVPrefix produces the same result
// as strings.TrimPrefix(tag, "v").
func TestPreservation_StripVPrefixEquivalence_Property(t *testing.T) {
	cfg := &quick.Config{
		MaxCount: 1000,
	}

	err := quick.Check(func(seed uint64) bool {
		r := rand.New(rand.NewSource(int64(seed)))
		tag := generateLowercaseOrNoPrefixTag(r)

		stripResult := stripVPrefix(tag)
		trimResult := strings.TrimPrefix(tag, "v")

		if stripResult != trimResult {
			t.Logf("MISMATCH: tag=%q stripVPrefix=%q TrimPrefix=%q", tag, stripResult, trimResult)
			return false
		}
		return true
	}, cfg)

	if err != nil {
		t.Errorf("Preservation property failed, stripVPrefix differs from TrimPrefix for non-buggy input: %v", err)
	}
}

// TestPreservation_ComparisonOutcome_Property verifies that for tag/appVersion pairs
// where both use lowercase 'v' prefix or no prefix, the comparison outcome (== or !=)
// is identical between the original logic (strings.TrimPrefix) and the fixed logic (stripVPrefix).
func TestPreservation_ComparisonOutcome_Property(t *testing.T) {
	cfg := &quick.Config{
		MaxCount: 1000,
	}

	err := quick.Check(func(seed uint64) bool {
		r := rand.New(rand.NewSource(int64(seed)))

		tag := generateLowercaseOrNoPrefixTag(r)
		// appVersion is stored without prefix (as it would be in the registry)
		appVersion := generateRandomVersionSegments(r)

		// Original logic from upgrade.go (unfixed)
		originalLatest := strings.TrimPrefix(tag, "v")
		originalEqual := (originalLatest == appVersion)

		// Fixed logic using stripVPrefix
		fixedLatest := stripVPrefix(tag)
		fixedEqual := (fixedLatest == stripVPrefix(appVersion))

		// For non-buggy inputs, both should produce the same comparison outcome
		if originalEqual != fixedEqual {
			t.Logf("COMPARISON MISMATCH: tag=%q appVersion=%q originalEqual=%v fixedEqual=%v",
				tag, appVersion, originalEqual, fixedEqual)
			return false
		}
		return true
	}, cfg)

	if err != nil {
		t.Errorf("Preservation property failed, comparison outcome differs for non-buggy input: %v", err)
	}
}

// TestPreservation_GenuineUpgradeDetection_Property verifies that genuine version
// differences (where stripped tag != stripped appVersion) are still correctly detected
// as needing an upgrade, regardless of whether the tag has a lowercase 'v' prefix or not.
func TestPreservation_GenuineUpgradeDetection_Property(t *testing.T) {
	cfg := &quick.Config{
		MaxCount: 1000,
	}

	err := quick.Check(func(seed uint64) bool {
		r := rand.New(rand.NewSource(int64(seed)))

		// Generate two different version strings
		version1 := generateRandomVersionSegments(r)
		version2 := generateRandomVersionSegments(r)

		// Ensure they are actually different
		for version1 == version2 {
			version2 = generateRandomVersionSegments(r)
		}

		// Tag uses lowercase 'v' or no prefix
		tag := version1
		if r.Intn(2) == 0 {
			tag = "v" + version1
		}
		appVersion := version2

		// Original logic
		originalLatest := strings.TrimPrefix(tag, "v")
		originalDifferent := (originalLatest != appVersion)

		// Fixed logic
		fixedLatest := stripVPrefix(tag)
		fixedDifferent := (fixedLatest != stripVPrefix(appVersion))

		// Both should detect the genuine difference
		if !originalDifferent {
			t.Logf("UNEXPECTED: original says equal for tag=%q appVersion=%q", tag, appVersion)
			return false
		}
		if !fixedDifferent {
			t.Logf("UNEXPECTED: fixed says equal for tag=%q appVersion=%q", tag, appVersion)
			return false
		}

		return true
	}, cfg)

	if err != nil {
		t.Errorf("Preservation property failed, genuine upgrade not detected: %v", err)
	}
}
