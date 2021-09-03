package main

import (
	"fmt"
	"reflect"
	"testing"
)

func TestRoundRobinDistribution(t *testing.T) {
	testCases := []struct {
		name        string
		items       []string
		expectedErr error
		wants       []string
	}{
		{
			name:        "NoItemsSpecified",
			items:       []string{},
			expectedErr: ErrEmptyItems,
			wants:       []string{},
		},
		{
			name: "OneItem",
			items: []string{
				"build01",
			},
			expectedErr: nil,
			wants: []string{
				"build01",
				"build01",
				"build01",
			},
		},
		{
			name: "TwoItems",
			items: []string{
				"build01",
				"build02",
			},
			expectedErr: nil,
			wants: []string{
				"build01",
				"build02",
				"build01",
				"build02",
			},
		},
		{
			name: "ThreeItems",
			items: []string{
				"build01",
				"build02",
				"build03",
			},
			expectedErr: nil,
			wants: []string{
				"build01",
				"build02",
				"build03",
				"build01",
				"build02",
				"build03",
			},
		},
	}

	for _, tc := range testCases {
		distribution, err := NewRoundRobinClusterDistribution(tc.items...)

		if err != nil && err != tc.expectedErr {
			t.Errorf("%s - expected error: %v, got: %v", tc.name, tc.expectedErr, err)
		}

		results := make([]string, 0, len(tc.wants))
		for j := 0; j < len(tc.wants); j++ {
			results = append(results, distribution.Get())
		}

		if !reflect.DeepEqual(results, tc.wants) {
			t.Errorf("%s - expected: %v, got: %v", tc.name, tc.wants, results)
		}
	}
}

func TestRandomDistribution(t *testing.T) {
	testCases := []struct {
		name        string
		items       []string
		expectedErr error
		wants       int
	}{
		{
			name:        "NoItemsSpecified",
			items:       []string{},
			expectedErr: ErrEmptyItems,
			wants:       0,
		},
		{
			name: "OneItemTwentyFiveTimes",
			items: []string{
				"build01",
			},
			expectedErr: nil,
			wants: 25,
		},
		{
			name: "TwoItemsFiftyTimes",
			items: []string{
				"build01",
				"build02",
			},
			expectedErr: nil,
			wants: 50,
		},
		{
			name: "ThreeItemsOneHundredTimes",
			items: []string{
				"build01",
				"build02",
				"build03",
			},
			expectedErr: nil,
			wants: 100,
		},
		{
			name: "FiveItemsOneThousandTimes",
			items: []string{
				"build01",
				"build02",
				"build03",
				"build04",
				"build05",
			},
			expectedErr: nil,
			wants: 1000,
		},
	}

	for _, tc := range testCases {
		distribution, err := NewRandomClusterDistribution(tc.items...)

		if err != nil && err != tc.expectedErr {
			t.Errorf("%s - expected error: %v, got: %v", tc.name, tc.expectedErr, err)
		} else if err != nil && err == tc.expectedErr{
			continue
		}

		results := make([]string, 0, tc.wants)
		for j := 0; j < tc.wants; j++ {
			results = append(results, distribution.Get())
		}

		if len(results) != tc.wants {
			t.Errorf("%s - expected: %v items, got: %v items", tc.name, tc.wants, results)
		}

		var data = make(map[string]int, len(tc.items))
		for _, item := range tc.items {
			for _, result := range results{
				if result == item {
					data[item]++
				}
			}
		}
		fmt.Printf("%s - Cluster distribution results:\n", tc.name)
		for key, value := range data {
			fmt.Printf("\t%s: %.2f%%\n", key, float32(value)/float32(tc.wants)*100)
		}
	}
}
