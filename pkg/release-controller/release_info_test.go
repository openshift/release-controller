package releasecontroller

import (
	"reflect"
	"testing"
)

func TestBugListToArr(t *testing.T) {
	testCases := []struct {
		name   string
		input  string
		output []int
	}{{
		name:   "Empty",
		input:  "",
		output: []int{},
	}, {
		name:   "Newline",
		input:  "\n",
		output: []int{},
	}, {
		name:   "Multiple cases",
		input:  "1\n2\n3",
		output: []int{1, 2, 3},
	}, {
		name:   "Multiple cases ending in newline",
		input:  "1\n2\n3\n",
		output: []int{1, 2, 3},
	}}
	for _, testCase := range testCases {
		output, err := bugListToArr(testCase.input)
		if err != nil {
			t.Errorf("%s: unexpected err: %v", testCase.name, err)
			continue
		}
		if !reflect.DeepEqual(output, testCase.output) {
			t.Errorf("%s: Expected %v, got %v", testCase.name, testCase.output, output)
		}
	}
}
