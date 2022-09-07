package main

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestSortedUpgradesByReleaseMap(t *testing.T) {
	testCases := []struct {
		name              string
		supportedUpgrades []string
		expected          SortedVersionsMap
	}{
		{
			name: "SingleDigitVersionPadding",
			supportedUpgrades: []string{
				"4.10.10",
				"4.10.11",
				"4.10.12",
				"4.10.20",
				"4.10.21",
				"4.10.22",
				"4.10.30",
				"4.10.31",
				"4.10.32",
				"4.10.0",
				"4.10.1",
				"4.10.2",
				"4.9.20",
				"4.9.21",
				"4.9.22",
				"4.9.30",
				"4.9.31",
				"4.9.32",
				"4.9.40",
				"4.9.41",
				"4.9.42",
			},
			expected: SortedVersionsMap{
				SortedKeys: []string{
					"4.09",
					"4.10",
				},
				VersionMap: map[string]SemanticVersions{
					"4.09": {
						{
							Major: 4,
							Minor: 9,
							Patch: 42,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 41,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 40,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 32,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 31,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 30,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 22,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 21,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 20,
						},
					},
					"4.10": {
						{
							Major: 4,
							Minor: 10,
							Patch: 32,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 31,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 30,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 22,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 21,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 20,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 12,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 11,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 10,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 2,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 1,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 0,
						},
					},
				},
			},
		},
		{
			name: "MixedVersions",
			supportedUpgrades: []string{
				"4.10.1",
				"4.10.10",
				"4.10.11",
				"4.10.12",
				"4.10.2",
				"4.10.20",
				"4.10.21",
				"4.10.22",
				"4.10.3",
				"4.10.30",
				"4.10.31",
				"4.10.32",
				"4.9.2",
				"4.9.20",
				"4.9.21",
				"4.9.22",
				"4.9.3",
				"4.9.30",
				"4.9.31",
				"4.9.32",
				"4.9.4",
				"4.9.40",
				"4.9.41",
				"4.9.42",
			},
			expected: SortedVersionsMap{
				SortedKeys: []string{
					"4.09",
					"4.10",
				},
				VersionMap: map[string]SemanticVersions{
					"4.09": {
						{
							Major: 4,
							Minor: 9,
							Patch: 42,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 41,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 40,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 32,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 31,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 30,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 22,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 21,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 20,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 4,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 3,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 2,
						},
					},
					"4.10": {
						{
							Major: 4,
							Minor: 10,
							Patch: 32,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 31,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 30,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 22,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 21,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 20,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 12,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 11,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 10,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 3,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 2,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 1,
						},
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results := SortedUpgradesByReleaseMap(tc.supportedUpgrades)
			if !reflect.DeepEqual(results, tc.expected) {
				t.Errorf("%s: Expected %v, got %v", tc.name, tc.expected, results)
			}
		})
	}
}

func TestSortedVersionsMapSample(t *testing.T) {
	testCases := []struct {
		name                 string
		sampleSize           int
		randomSeed           int64
		supportedVersionsMap SortedVersionsMap
		expected             []string
	}{
		{
			name:       "NotEnoughPreviousReleaseEdges",
			sampleSize: 3,
			randomSeed: 1337,
			supportedVersionsMap: SortedVersionsMap{
				SortedKeys: []string{
					"4.10",
					"4.11",
				},
				VersionMap: map[string]SemanticVersions{
					"4.10": {
						{
							Major: 4,
							Minor: 10,
							Patch: 1,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 0,
						},
					},
					"4.11": {
						{
							Major: 4,
							Minor: 11,
							Patch: 10,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 9,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 8,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 7,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 6,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 5,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 4,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 3,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 2,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 1,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 0,
						},
					},
				},
			},
			expected: []string{
				// 4.Y-1.z: First N
				"4.10.1",
				"4.10.0",

				// 4.y.z: First N
				"4.11.10",
				"4.11.9",
				"4.11.8",

				// 4.y.z: Last N
				"4.11.2",
				"4.11.1",
				"4.11.0",

				// 4.y.z: Random N
				"4.11.4",
				"4.11.4",
				"4.11.3",
			},
		},
		{
			name:       "NotEnoughReleaseEdges",
			sampleSize: 3,
			randomSeed: 1337,
			supportedVersionsMap: SortedVersionsMap{
				SortedKeys: []string{
					"4.10",
					"4.11",
				},
				VersionMap: map[string]SemanticVersions{
					"4.10": {
						{
							Major: 4,
							Minor: 10,
							Patch: 10,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 9,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 8,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 7,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 6,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 5,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 4,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 3,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 2,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 1,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 0,
						},
					},
					"4.11": {
						{
							Major: 4,
							Minor: 11,
							Patch: 1,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 0,
						},
					},
				},
			},
			expected: []string{
				// 4.Y-1.z: First N
				"4.10.10",
				"4.10.9",
				"4.10.8",

				// 4.Y-1.z: Last N
				"4.10.2",
				"4.10.1",
				"4.10.0",

				// 4.Y-1.z: Random N
				"4.10.4",
				"4.10.4",
				"4.10.3",

				// 4.y.z: First N
				"4.11.1",
				"4.11.0",
			},
		},
		{
			name:       "NotEnoughEdges",
			sampleSize: 3,
			randomSeed: 1337,
			supportedVersionsMap: SortedVersionsMap{
				SortedKeys: []string{
					"4.10",
					"4.11",
				},
				VersionMap: map[string]SemanticVersions{
					"4.10": {
						{
							Major: 4,
							Minor: 10,
							Patch: 2,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 1,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 0,
						},
					},
					"4.11": {
						{
							Major: 4,
							Minor: 11,
							Patch: 1,
						},
						{
							Major: 4,
							Minor: 11,
							Patch: 0,
						},
					},
				},
			},
			expected: []string{
				// 4.Y-1.z: First N
				"4.10.2",
				"4.10.1",
				"4.10.0",

				// 4.y.z: First N
				"4.11.1",
				"4.11.0",
			},
		},
		{
			name:       "FullSampleSizeAvailable",
			sampleSize: 3,
			randomSeed: 1337,
			supportedVersionsMap: SortedVersionsMap{
				SortedKeys: []string{
					"4.09",
					"4.10",
				},
				VersionMap: map[string]SemanticVersions{
					"4.09": {
						{
							Major: 4,
							Minor: 9,
							Patch: 42,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 41,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 40,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 32,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 31,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 30,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 22,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 21,
						},
						{
							Major: 4,
							Minor: 9,
							Patch: 20,
						},
					},
					"4.10": {
						{
							Major: 4,
							Minor: 10,
							Patch: 32,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 31,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 30,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 22,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 21,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 20,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 12,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 11,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 10,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 2,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 1,
						},
						{
							Major: 4,
							Minor: 10,
							Patch: 0,
						},
					},
				},
			},
			expected: []string{
				// 4.Y-1.z: First N
				"4.9.42",
				"4.9.41",
				"4.9.40",

				// 4.Y-1.z: Last N
				"4.9.22",
				"4.9.21",
				"4.9.20",

				// 4.Y-1.z: Random N
				"4.9.31",
				"4.9.30",
				"4.9.30",

				// 4.y.z: First N
				"4.10.32",
				"4.10.31",
				"4.10.30",

				// 4.y.z: Last N
				"4.10.2",
				"4.10.1",
				"4.10.0",

				// 4.y.z: Random N
				"4.10.10",
				"4.10.20",
				"4.10.11",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Forcing static random seed for reproducibility
			random = rand.New(rand.NewSource(tc.randomSeed))
			results := tc.supportedVersionsMap.Sample(tc.sampleSize)
			if !reflect.DeepEqual(results, tc.expected) {
				t.Errorf("%s: Expected %v, got %v", tc.name, tc.expected, results)
			}
		})
	}
}
