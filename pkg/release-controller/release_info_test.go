package releasecontroller

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestBugList(t *testing.T) {
	testData := "[\n  {\n    \"id\": \"OCPBUGS-4128\",\n    \"status\": \"\",\n    \"priority\": \"\",\n    \"summary\": \"\",\n    \"source\": 1\n  },\n  {\n    \"id\": \"OCPBUGS-4129\",\n    \"status\": \"\",\n    \"priority\": \"\",\n    \"summary\": \"\",\n    \"source\": 1\n  },\n  {\n    \"id\": \"4129\",\n    \"status\": \"\",\n    \"priority\": \"\",\n    \"summary\": \"\",\n    \"source\": 0\n  }  \n]"
	expectedResult := []BugDetails{
		{
			ID:     "OCPBUGS-4128",
			Source: 1,
		},
		{
			ID:     "OCPBUGS-4129",
			Source: 1,
		},
		{
			ID:     "4129",
			Source: 0,
		},
	}
	result, err := bugList(testData)
	if err != nil {
		t.Fatalf("failed to unmarshall testData. Error: %s", err)
	}
	if diff := cmp.Diff(result, expectedResult); diff != "" {
		t.Fatalf("incorrect result %v", diff)
	}
}
