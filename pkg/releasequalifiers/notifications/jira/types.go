// +k8s:deepcopy-gen=package

package jira

// Notification defines how notifications are sent via Jira
// It includes project configuration and escalation rules
// +k8s:deepcopy-gen=true
type Notification struct {
	// Project is the Jira project key where tickets will be created
	Project string `json:"project,omitempty" yaml:"project,omitempty"`

	// Component is the Jira component this qualifier relates to
	Component string `json:"component,omitempty" yaml:"component,omitempty"`

	// Assignee is the default assignee for tickets created by this qualifier
	Assignee string `json:"assignee,omitempty" yaml:"assignee,omitempty"`

	// Summary is the default summary text for Jira tickets
	Summary string `json:"summary,omitempty" yaml:"summary,omitempty"`

	// Description is the default description text for Jira tickets
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Thread identifier for separating notifications across jobs
	// When multiple jobs contribute to the same qualifier, different thread values
	// will result in separate Jira tickets being created
	Thread string `json:"thread,omitempty" yaml:"thread,omitempty"`

	// Escalations defines the escalation rules for Jira notifications
	// Each escalation specifies when and how to create tickets based on failure patterns
	Escalations []Escalation `json:"escalations,omitempty" yaml:"escalations,omitempty"`
}

// Escalation defines a single escalation rule for Jira notifications
// It specifies the conditions and actions for creating Jira tickets
// Multiple criteria can be combined to create sophisticated escalation rules:
//   - Simple: Failures=3 triggers after 3 consecutive failures
//   - Windowed: OverLastRuns=10, Failures=2 triggers if >=2 of last 10 runs failed
//   - Percentage: OverLastRuns=10, PassPercentage=60 triggers if <60% of last 10 runs passed
//   - Time-bounded: OverPeriod="2d", OverLastRuns=20, PassPercentage=80 considers last 20 runs
//     or runs from last 2 days (whichever provides more samples)
//
// +k8s:deepcopy-gen=true
type Escalation struct {
	// Name is the unique identifier for this escalation level
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// Failures is the number of failures required to trigger this escalation
	// When used alone, this counts consecutive failures
	// When combined with OverLastRuns, this counts total failures in the window
	Failures int `json:"failures,omitempty" yaml:"failures,omitempty"`

	// OverLastRuns defines the window of recent runs to consider
	// If omitted when Failures is set, defaults to Failures (consecutive mode)
	// Can be combined with Failures, PassPercentage, or OverPeriod
	// +kubebuilder:validation:Minimum=1
	OverLastRuns *int `json:"overLastRuns,omitempty" yaml:"overLastRuns,omitempty"`

	// PassPercentage defines the minimum pass rate required (0-100)
	// Escalates when the pass rate falls below this threshold
	// Must be used with OverLastRuns to define the evaluation window
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	PassPercentage *int `json:"passPercentage,omitempty" yaml:"passPercentage,omitempty"`

	// OverPeriod defines a time window for considering runs (e.g., "2d" for 2 days)
	// Used with OverLastRuns to expand the sample set: whichever provides more runs is used
	// Format examples: "1h", "24h", "2d", "1w"
	// +kubebuilder:validation:Pattern=`^[1-9]\d*(h|d|w)$`
	OverPeriod string `json:"overPeriod,omitempty" yaml:"overPeriod,omitempty"`

	// Priority is the Jira priority level for tickets created at this escalation
	Priority string `json:"priority,omitempty" yaml:"priority,omitempty"`

	// Mentions is a list of users to mention in the Jira ticket
	Mentions []string `json:"mentions,omitempty" yaml:"mentions,omitempty"`

	// NeedsInfo is a list of users to add as watchers or request information from
	NeedsInfo []string `json:"needsInfo,omitempty" yaml:"needsInfo,omitempty"`
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
