package main

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"testing/quick"
)

// compareVersionsOriginal is a copy of the original CompareVersions function
// (before any fix), used as a reference for preservation testing.
func compareVersionsOriginal(v1, v2 string) int {
	v1 = strings.TrimPrefix(strings.ToLower(v1), "v")
	v2 = strings.TrimPrefix(strings.ToLower(v2), "v")

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
	return 0
}

// hasPreRelease returns true if any segment of the version contains a hyphen
// followed by a known pre-release identifier (alpha, beta, rc).
func hasPreRelease(v string) bool {
	v = strings.TrimPrefix(strings.ToLower(v), "v")
	segments := strings.Split(v, ".")
	for _, seg := range segments {
		if idx := strings.Index(seg, "-"); idx >= 0 {
			suffix := seg[idx+1:]
			if strings.HasPrefix(suffix, "alpha") ||
				strings.HasPrefix(suffix, "beta") ||
				strings.HasPrefix(suffix, "rc") {
				return true
			}
		}
	}
	return false
}

// baseVersion extracts the numeric-only portion of a version string,
// stripping any pre-release suffix from each segment.
func baseVersion(v string) string {
	v = strings.TrimPrefix(strings.ToLower(v), "v")
	segments := strings.Split(v, ".")
	for i, seg := range segments {
		if idx := strings.Index(seg, "-"); idx >= 0 {
			segments[i] = seg[:idx]
		}
	}
	return strings.Join(segments, ".")
}

// isBugCondition returns true when at least one version has a pre-release suffix
// and both versions share the same base version numbers.
func isBugCondition(v1, v2 string) bool {
	return (hasPreRelease(v1) || hasPreRelease(v2)) &&
		baseVersion(v1) == baseVersion(v2)
}

// generateStableVersion generates a random stable version string (no hyphens)
// with 1-4 segments, each segment value 0-99.
func generateStableVersion(r *rand.Rand) string {
	numSegments := r.Intn(4) + 1 // 1 to 4 segments
	segments := make([]string, numSegments)
	for i := range segments {
		segments[i] = fmt.Sprintf("%d", r.Intn(100))
	}
	return strings.Join(segments, ".")
}

// generateVersionWithOptionalPrefix generates a stable version with an optional "v" prefix.
func generateVersionWithOptionalPrefix(r *rand.Rand) string {
	v := generateStableVersion(r)
	if r.Intn(2) == 0 {
		v = "v" + v
	}
	return v
}

// generateDifferingBaseVersionPair generates a pair of versions where the base
// versions differ numerically. One version may optionally have a pre-release suffix.
func generateDifferingBaseVersionPair(r *rand.Rand) (string, string) {
	// Generate two stable versions that are guaranteed to differ
	v1 := generateStableVersion(r)
	v2 := generateStableVersion(r)

	// Ensure they actually differ in base version
	for baseVersion(v1) == baseVersion(v2) {
		v2 = generateStableVersion(r)
	}

	// Optionally add a pre-release suffix to one of them
	if r.Intn(3) == 0 {
		preTypes := []string{"alpha", "beta", "rc"}
		pre := preTypes[r.Intn(len(preTypes))]
		if r.Intn(2) == 0 {
			pre += fmt.Sprintf("%d", r.Intn(10)+1)
		}
		if r.Intn(2) == 0 {
			v1 = v1 + "-" + pre
		} else {
			v2 = v2 + "-" + pre
		}
	}

	// Optionally add v prefix
	if r.Intn(2) == 0 {
		v1 = "v" + v1
	}
	if r.Intn(2) == 0 {
		v2 = "v" + v2
	}

	return v1, v2
}

// TestPreservationStableVersions tests that CompareVersions produces the same
// result as the original function for stable version pairs (no pre-release suffixes).
func TestPreservationStableVersions(t *testing.T) {
	// Table-driven concrete examples first
	examples := []struct {
		v1, v2   string
		expected int
	}{
		{"2.0.0", "1.9.0", 1},
		{"1.0.0", "1.0.0", 0},
		{"v2.0.0", "v1.0.0", 1},
		{"1.0", "1.0.0", 0},
		{"2.0.0-rc1", "1.9.0", 1}, // base version dominates
	}

	for _, ex := range examples {
		t.Run(fmt.Sprintf("%s_vs_%s", ex.v1, ex.v2), func(t *testing.T) {
			got := CompareVersions(ex.v1, ex.v2)
			if got != ex.expected {
				t.Errorf("CompareVersions(%q, %q) = %d, want %d", ex.v1, ex.v2, got, ex.expected)
			}
		})
	}
}

// TestPreservationPropertyStable uses property-based testing to verify that
// CompareVersions produces identical results to the original function for all
// generated stable version pairs (where isBugCondition is false).
func TestPreservationPropertyStable(t *testing.T) {
	cfg := &quick.Config{
		MaxCount: 1000,
	}

	// For stable version pairs, CompareVersions == compareVersionsOriginal
	err := quick.Check(func(seed uint64) bool {
		r := rand.New(rand.NewSource(int64(seed)))

		v1 := generateVersionWithOptionalPrefix(r)
		v2 := generateVersionWithOptionalPrefix(r)

		// Only test non-buggy inputs
		if isBugCondition(v1, v2) {
			return true // skip buggy inputs
		}

		got := CompareVersions(v1, v2)
		expected := compareVersionsOriginal(v1, v2)
		if got != expected {
			t.Logf("MISMATCH: CompareVersions(%q, %q) = %d, original = %d", v1, v2, got, expected)
			return false
		}
		return true
	}, cfg)

	if err != nil {
		t.Errorf("Preservation property failed for stable versions: %v", err)
	}
}

// TestPreservationPropertyDifferingBase uses property-based testing to verify that
// CompareVersions produces identical results to the original function for version
// pairs where the base versions differ (even if one has a pre-release suffix).
func TestPreservationPropertyDifferingBase(t *testing.T) {
	cfg := &quick.Config{
		MaxCount: 1000,
	}

	// For version pairs with differing base versions,
	// CompareVersions == compareVersionsOriginal
	err := quick.Check(func(seed uint64) bool {
		r := rand.New(rand.NewSource(int64(seed)))

		v1, v2 := generateDifferingBaseVersionPair(r)

		// Confirm this is NOT a bug condition (base versions differ)
		if isBugCondition(v1, v2) {
			return true // skip - shouldn't happen but be safe
		}

		got := CompareVersions(v1, v2)
		expected := compareVersionsOriginal(v1, v2)
		if got != expected {
			t.Logf("MISMATCH: CompareVersions(%q, %q) = %d, original = %d", v1, v2, got, expected)
			return false
		}
		return true
	}, cfg)

	if err != nil {
		t.Errorf("Preservation property failed for differing base versions: %v", err)
	}
}
