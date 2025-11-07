package jira

// Notification defines how notifications are sent via Jira
// It includes project configuration and escalation rules
type Notification struct {
	// Project is the Jira project key where tickets will be created
	Project string `json:"project" yaml:"project"`

	// Component is the Jira component this qualifier relates to
	Component string `json:"component" yaml:"component"`

	// Assignee is the default assignee for tickets created by this qualifier
	Assignee string `json:"assignee" yaml:"assignee"`

	// Summary is the default summary text for Jira tickets
	Summary string `json:"summary" yaml:"summary"`

	// Description is the default description text for Jira tickets
	Description string `json:"description" yaml:"description"`

	// Escalations defines the escalation rules for Jira notifications
	// Each escalation specifies when and how to create tickets based on failure patterns
	Escalations []Escalation `json:"escalations,omitempty" yaml:"escalations,omitempty"`
}

// Escalation defines a single escalation rule for Jira notifications
// It specifies the conditions and actions for creating Jira tickets
type Escalation struct {
	// Name is the unique identifier for this escalation level
	Name string `json:"name" yaml:"name"`

	// Failures is the number of failures required to trigger this escalation
	Failures int `json:"failures" yaml:"failures"`

	// Priority is the Jira priority level for tickets created at this escalation
	Priority string `json:"priority" yaml:"priority"`

	// Mentions is a list of users to mention in the Jira ticket
	Mentions []string `json:"mentions,omitempty" yaml:"mentions,omitempty"`
}

// ByJiraEscalationName sorts a list of Escalations' by their Name
type ByJiraEscalationName []Escalation

func (in ByJiraEscalationName) Less(i, j int) bool {
	return in[i].Name < in[j].Name
}

func (in ByJiraEscalationName) Len() int {
	return len(in)
}

func (in ByJiraEscalationName) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}

func (j Notification) Send() {
	//TODO implement me
	panic("implement me")
}
