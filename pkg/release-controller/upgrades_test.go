package releasecontroller

import (
	"reflect"
	"sort"
	"testing"

	"k8s.io/apimachinery/pkg/util/diff"
)

func TestUpgradeGraph_UpgradesFrom(t *testing.T) {
	tests := []struct {
		name      string
		graph     func() *UpgradeGraph
		fromNames []string
		want      []UpgradeHistory
	}{
		{
			graph: func() *UpgradeGraph {
				g := NewUpgradeGraph("amd64")
				g.Add("1.0.0", "1.1.0", UpgradeResult{State: ReleaseVerificationStateSucceeded, URL: "http://1"})
				g.Add("1.0.0", "1.1.0", UpgradeResult{State: ReleaseVerificationStateSucceeded, URL: "http://2"})
				g.Add("1.0.1", "1.1.0", UpgradeResult{State: ReleaseVerificationStateSucceeded, URL: "http://3"})
				g.Add("0.0.1", "1.0.0", UpgradeResult{State: ReleaseVerificationStateSucceeded, URL: "http://4"})
				g.Add("1.0.0", "1.1.1", UpgradeResult{State: ReleaseVerificationStateSucceeded, URL: "http://5"})
				return g
			},
			fromNames: []string{"1.0.0"},
			want: []UpgradeHistory{
				{
					From:    "1.0.0",
					To:      "1.1.0",
					Success: 2,
					Total:   2,
					History: map[string]UpgradeResult{
						"http://1": {State: ReleaseVerificationStateSucceeded, URL: "http://1"},
						"http://2": {State: ReleaseVerificationStateSucceeded, URL: "http://2"},
					},
				},
				{
					From:    "1.0.0",
					To:      "1.1.1",
					Success: 1,
					Total:   1,
					History: map[string]UpgradeResult{
						"http://5": {State: ReleaseVerificationStateSucceeded, URL: "http://5"},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := tt.graph()
			got := g.UpgradesFrom(tt.fromNames...)
			sort.Sort(NewNewestSemVerFromSummaries(got))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UpgradeGraph.UpgradesFrom() = %s", diff.ObjectReflectDiff(tt.want, got))
			}
		})
	}
}

func TestUpgradeGraph_UpgradesTo(t *testing.T) {
	tests := []struct {
		name    string
		graph   func() *UpgradeGraph
		toNames []string
		want    []UpgradeHistory
	}{
		{
			graph: func() *UpgradeGraph {
				g := NewUpgradeGraph("amd64")
				g.Add("1.0.0", "1.1.0", UpgradeResult{State: ReleaseVerificationStateSucceeded, URL: "http://1"})
				g.Add("1.0.0", "1.1.0", UpgradeResult{State: ReleaseVerificationStateSucceeded, URL: "http://2"})
				g.Add("1.0.1", "1.1.0", UpgradeResult{State: ReleaseVerificationStateSucceeded, URL: "http://3"})
				g.Add("0.0.1", "1.0.0", UpgradeResult{State: ReleaseVerificationStateSucceeded, URL: "http://4"})
				g.Add("1.0.0", "1.1.1", UpgradeResult{State: ReleaseVerificationStateSucceeded, URL: "http://5"})
				return g
			},
			toNames: []string{"1.1.0"},
			want: []UpgradeHistory{
				{
					From:    "1.0.1",
					To:      "1.1.0",
					Success: 1,
					Total:   1,
					History: map[string]UpgradeResult{
						"http://3": {State: ReleaseVerificationStateSucceeded, URL: "http://3"},
					},
				},
				{
					From:    "1.0.0",
					To:      "1.1.0",
					Success: 2,
					Total:   2,
					History: map[string]UpgradeResult{
						"http://1": {State: ReleaseVerificationStateSucceeded, URL: "http://1"},
						"http://2": {State: ReleaseVerificationStateSucceeded, URL: "http://2"},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := tt.graph()
			got := g.UpgradesTo(tt.toNames...)
			sort.Sort(NewNewestSemVerFromSummaries(got))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("UpgradeGraph.UpgradesFrom() = %s", diff.ObjectReflectDiff(tt.want, got))
			}
		})
	}
}
