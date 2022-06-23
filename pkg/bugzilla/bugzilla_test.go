package bugzilla

import "testing"

func TestValidateBZRequestError(t *testing.T) {
	goodResponse := &bugzillaRequestError{statusCode: 201, bugzillaCode: 201, message: "I am an error"}
	badResponse := &bugzillaRequestError{statusCode: 301, bugzillaCode: 301, message: "I am an error"}
	isItAnError := validateBZRequestError(goodResponse)
	if isItAnError {
		t.Fatalf("failed to validate whitelisted return codes")
	}
	isItAnError = validateBZRequestError(nil)
	if isItAnError {
		t.Errorf("failed to validate nil error")
	}
	isItAnError = validateBZRequestError(badResponse)
	if !isItAnError {
		t.Fatalf("wrong validataion of statusCode %d", badResponse.statusCode)
	}
}
