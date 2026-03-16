package helpers

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/andygrunwald/go-jira"
)

const (
	QAContactField       = "customfield_10470"
	SeverityField        = "customfield_10840"
	TargetVersionField   = "customfield_10855"
	ReleaseBlockerField  = "customfield_10847"
	ReleaseNoteTextField = "customfield_10783"
	SprintField          = "customfield_10020"
	ReleaseNoteTypeField = "customfield_10785"
	ContributorsField    = "customfield_10479"
)

// GetUnknownField will attempt to get the specified field from the Unknowns struct and unmarshal
// the value into the provided function. If the field is not set, the first return value of this
// function will return false.
func GetUnknownField(field string, issue *jira.Issue, fn func() any) (bool, error) {
	obj := fn()
	if issue.Fields == nil || issue.Fields.Unknowns == nil {
		return false, nil
	}
	unknownField, ok := issue.Fields.Unknowns[field]
	if !ok {
		return false, nil
	}
	if unknownField == nil {
		return false, nil
	}
	bytes, err := json.Marshal(unknownField)
	if err != nil {
		return true, fmt.Errorf("failed to process the custom field %s. Error : %v", field, err)
	}
	if err := json.Unmarshal(bytes, obj); err != nil {
		return true, fmt.Errorf("failed to unmarshal the json to struct for %s. Error: %v", field, err)
	}
	return true, nil
}

// GetSprintField returns a raw interface for the Sprint value of an issue if it exists. Currently, the value
// is only used during cloning, so no struct is currently needed for us to parse data from the interface.
func GetSprintField(issue *jira.Issue) any {
	if issue.Fields == nil || issue.Fields.Unknowns == nil {
		return nil
	}
	sprintObject, ok := issue.Fields.Unknowns[SprintField]
	if !ok {
		return nil
	}
	return sprintObject
}

// GetIssueSecurityLevel returns the security level of an issue. If no security level
// is set for the issue, the returned SecurityLevel and error will both be nil and
// the issue will follow the default project security level.
func GetIssueSecurityLevel(issue *jira.Issue) (*SecurityLevel, error) {
	// TODO: Add field to the upstream go-jira package; if a security level exists, it is returned
	// as part of the issue fields
	// See https://github.com/andygrunwald/go-jira/issues/456
	var obj *SecurityLevel
	isSet, err := GetUnknownField("security", issue, func() any {
		obj = &SecurityLevel{}
		return obj
	})
	if !isSet {
		return nil, err
	}
	return obj, err
}

type SecurityLevel struct {
	Self        string `json:"self"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

func GetIssueQaContact(issue *jira.Issue) (*jira.User, error) {
	var obj *jira.User
	isSet, err := GetUnknownField(QAContactField, issue, func() any {
		obj = &jira.User{}
		return obj
	})
	if !isSet {
		return nil, err
	}
	return obj, err
}

func GetIssueTargetVersion(issue *jira.Issue) ([]*jira.Version, error) {
	var obj *[]*jira.Version
	isSet, err := GetUnknownField(TargetVersionField, issue, func() any {
		obj = &[]*jira.Version{{}}
		return obj
	})
	if !isSet {
		return nil, err
	}
	return *obj, err
}

func GetIssueSeverity(issue *jira.Issue) (*CustomField, error) {
	var obj *CustomField
	isSet, err := GetUnknownField(SeverityField, issue, func() any {
		obj = &CustomField{}
		return obj
	})
	if !isSet {
		return nil, err
	}
	return obj, err
}

type CustomField struct {
	Self     string `json:"self"`
	ID       string `json:"id"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled"`
}

func GetIssueReleaseNoteText(issue *jira.Issue) (*string, error) {
	var obj *string
	isSet, err := GetUnknownField(ReleaseNoteTextField, issue, func() any {
		var field string
		obj = &field
		return obj
	})
	if !isSet {
		return nil, err
	}
	return obj, err
}

func GetIssueReleaseNoteType(issue *jira.Issue) (*CustomField, error) {
	var obj *CustomField
	isSet, err := GetUnknownField(ReleaseNoteTypeField, issue, func() any {
		obj = &CustomField{}
		return obj
	})
	if !isSet {
		return nil, err
	}
	return obj, err
}

var activeSprintReg = regexp.MustCompile(",state=ACTIVE,")
var sprintIDReg = regexp.MustCompile("id=([0-9]+)")

func GetActiveSprintID(sprintField any) (int, error) {
	if sprintField == nil {
		return -1, nil
	}
	sprintFieldSlice, ok := sprintField.([]any)
	if !ok {
		return -1, errors.New("failed to convert sprint field to slice of interfaces")
	}
	for _, sprint := range sprintFieldSlice {
		sprintString := sprint.(string)
		if activeSprintReg.MatchString(sprintString) {
			if submatch := sprintIDReg.FindStringSubmatch(sprintString); submatch != nil {
				sprintID, err := strconv.Atoi(submatch[1])
				if err != nil {
					// should be impossible based on the regex
					return -1, fmt.Errorf("failed to parse sprint ID. Err: %w", err)
				}
				return sprintID, nil
			}
		}
	}
	return -1, nil
}

type Contributor struct {
	Self string `json:"self"`
	Name string `json:"name"`
}

func GetIssueContributors(issue *jira.Issue) (*[]Contributor, error) {
	var obj *[]Contributor
	isSet, err := GetUnknownField(ContributorsField, issue, func() any {
		obj = &[]Contributor{}
		return obj
	})
	if !isSet {
		return nil, err
	}
	return obj, err
}
