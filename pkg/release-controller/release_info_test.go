package releasecontroller

import (
	"github.com/google/go-cmp/cmp"
	"testing"
)

func TestBugList(t *testing.T) {
	testData := "[\n  {\n    \"id\": 2087213,\n    \"status\": \"VERIFIED\",\n    \"priority\": \"high\",\n    \"summary\": \"Spoke BMH stuck \\\"inspecting\\\" when deployed via ZTP in 4.11 OCP hub\",\n    \"source\": 0\n  },\n  {\n    \"id\": 2093126,\n    \"status\": \"ON_QA\",\n    \"priority\": \"urgent\",\n    \"summary\": \"Summary\",\n    \"source\": 0\n  },\n  {\n    \"id\": 2102639,\n    \"status\": \"ON_QA\",\n    \"priority\": \"unspecified\",\n    \"summary\": \"Summary\",\n    \"source\": 1\n  }\n]\n"
	expectedResult := []BugDetails{
		{
			ID:     2087213,
			Source: 0,
		},
		{
			ID:     2093126,
			Source: 0,
		},
		{
			ID:     2102639,
			Source: 1,
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
