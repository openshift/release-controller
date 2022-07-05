package bugzilla

import (
	"fmt"
	"testing"
)

type bugzillaRequestError struct {
	statusCode   int
	bugzillaCode int
	message      string
}

func (e bugzillaRequestError) Error() string {
	if e.bugzillaCode != 0 {
		return fmt.Sprintf("code %d: %s", e.bugzillaCode, e.message)
	}
	return e.message
}

func TestValidateBZRequestError(t *testing.T) {
	testCases := []struct {
		name     string
		id       int
		response error
		expected bool
	}{
		{
			name:     "Status Code 201",
			id:       123,
			response: &bugzillaRequestError{statusCode: 201, bugzillaCode: 201, message: "message"},
			expected: false,
		},
		{
			name:     "Status Code 401",
			id:       123,
			response: &bugzillaRequestError{statusCode: 401, bugzillaCode: 401, message: "message"},
			expected: false,
		},
		{
			name:     "Empty response",
			id:       124,
			response: nil,
			expected: false,
		},
		{
			name:     "Non whitelisted status code",
			id:       125,
			response: &bugzillaRequestError{statusCode: 301, bugzillaCode: 301, message: "message"},
			expected: true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			isItAnError := validateBZRequestError(tc.id, tc.response)
			if isItAnError != tc.expected {
				t.Errorf("expected a %t validation but got %t for this response: %v", tc.expected, isItAnError, tc.response)
			}
		})
	}
}
