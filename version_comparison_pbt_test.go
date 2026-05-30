package main

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"testing/quick"
)

// preReleaseTypes defines the valid pre-release type identifiers in rank order.
var preReleaseTypes = []string{"alpha", "beta", "rc"}

// preReleaseRankTest returns the rank of a pre-release type for comparison.
// alpha=1, beta=2, rc=3
func preReleaseRankTest(preType string) int {
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

// generateBaseVersion creates a random version string with 1-4 segments, values 0-99.
func generateBaseVersion(rng *rand.Rand) string {
	numSegments := 1 + rng.Intn(4) // 1-4 segments
	segments := make([]string, numSegments)
	for i := range segments {
		segments[i] = fmt.Sprintf("%d", rng.Intn(100))
	}
	return strings.Join(segments, ".")
}

// generatePreReleaseSuffix creates a random pre-release suffix (e.g., "alpha", "beta2", "rc1").
func generatePreReleaseSuffix(rng *rand.Rand) string {
	preType := preReleaseTypes[rng.Intn(len(preReleaseTypes))]
	// 50% chance of adding a numeric suffix (1-9)
	if rng.Intn(2) == 0 {
		return preType
	}
	return fmt.Sprintf("%s%d", preType, 1+rng.Intn(9))
}

// parsePreReleaseSuffix extracts the type and numeric suffix from a pre-release string.
// e.g., "rc1" -> ("rc", 1), "alpha" -> ("alpha", 0)
func parsePreReleaseSuffix(suffix string) (string, int) {
	for _, t := range []string{"alpha", "beta", "rc"} {
		if strings.HasPrefix(suffix, t) {
			numStr := strings.TrimPrefix(suffix, t)
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

// expectedPreReleaseComparison computes the expected comparison result when both
// versions have pre-release suffixes.
func expectedPreReleaseComparison(suffix1, suffix2 string) int {
	type1, num1 := parsePreReleaseSuffix(suffix1)
	type2, num2 := parsePreReleaseSuffix(suffix2)

	rank1 := preReleaseRankTest(type1)
	rank2 := preReleaseRankTest(type2)

	if rank1 < rank2 {
		return -1
	}
	if rank1 > rank2 {
		return 1
	}
	// Same type, compare numeric suffix
	if num1 < num2 {
		return -1
	}
	if num1 > num2 {
		return 1
	}
	return 0
}

// TestBugCondition_PreReleaseVersionComparison is a property-based test that verifies
// pre-release versions are correctly ordered relative to stable versions and each other.
//
// Bug condition: fmt.Sscanf("%d", ...) silently discards hyphen-delimited pre-release
// suffixes, causing all pre-release versions to compare as equal to their stable counterparts.
//
// EXPECTED: This test FAILS on unfixed code, failure confirms the bug exists.
func TestBugCondition_PreReleaseVersionComparison(t *testing.T) {
	f := func(seed int64) bool {
		rng := rand.New(rand.NewSource(seed))

		// Generate a base version shared by both versions (bug condition requires same base)
		base := generateBaseVersion(rng)

		// Decide the scenario: pre-release vs stable, stable vs pre-release, or both pre-release
		scenario := rng.Intn(3)

		var v1, v2 string
		var expectedResult int

		switch scenario {
		case 0:
			// v1 has pre-release, v2 is stable then expected -1
			suffix := generatePreReleaseSuffix(rng)
			v1 = base + "-" + suffix
			v2 = base
			expectedResult = -1

		case 1:
			// v1 is stable, v2 has pre-release then expected 1
			suffix := generatePreReleaseSuffix(rng)
			v1 = base
			v2 = base + "-" + suffix
			expectedResult = 1

		case 2:
			// Both have pre-release then expected ordering based on type and numeric suffix
			suffix1 := generatePreReleaseSuffix(rng)
			suffix2 := generatePreReleaseSuffix(rng)
			v1 = base + "-" + suffix1
			v2 = base + "-" + suffix2
			expectedResult = expectedPreReleaseComparison(suffix1, suffix2)
			// Skip cases where both suffixes are identical (result would be 0, which
			// the buggy code also returns which wouldn't surface the bug)
			if expectedResult == 0 {
				return true
			}
		}

		result := CompareVersions(v1, v2)
		if result != expectedResult {
			t.Logf("COUNTEREXAMPLE: CompareVersions(%q, %q) = %d, expected %d",
				v1, v2, result, expectedResult)
			return false
		}
		return true
	}

	if err := quick.Check(f, &quick.Config{MaxCount: 200}); err != nil {
		t.Errorf("Bug confirmed - pre-release versions incorrectly compare as equal to stable: %v", err)
	}
}

// TestBugCondition_PreReleaseTableDriven provides concrete examples of the bug condition
// as table-driven test cases.
//
// EXPECTED: This test FAILS on unfixed code, failure confirms the bug exists.
func TestBugCondition_PreReleaseTableDriven(t *testing.T) {
	tests := []struct {
		name     string
		v1       string
		v2       string
		expected int
	}{
		{
			name:     "pre-release rc1 vs stable",
			v1:       "1.0.0-rc1",
			v2:       "1.0.0",
			expected: -1,
		},
		{
			name:     "stable vs pre-release alpha",
			v1:       "1.0.0",
			v2:       "1.0.0-alpha",
			expected: 1,
		},
		{
			name:     "alpha vs beta ordering",
			v1:       "1.0.0-alpha",
			v2:       "1.0.0-beta",
			expected: -1,
		},
		{
			name:     "rc1 vs rc2 numeric suffix ordering",
			v1:       "1.0.0-rc1",
			v2:       "1.0.0-rc2",
			expected: -1,
		},
		{
			name:     "beta vs rc1 type ordering",
			v1:       "1.0.0-beta",
			v2:       "1.0.0-rc1",
			expected: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CompareVersions(tt.v1, tt.v2)
			if result != tt.expected {
				t.Errorf("CompareVersions(%q, %q) = %d, expected %d (bug: fmt.Sscanf discards pre-release suffix)",
					tt.v1, tt.v2, result, tt.expected)
			}
		})
	}
}
