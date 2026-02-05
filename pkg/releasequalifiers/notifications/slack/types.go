// +k8s:deepcopy-gen=package

package slack

// Notification defines how notifications are sent via Slack
// It includes escalation rules and channel configuration
// +k8s:deepcopy-gen=true
type Notification struct {
	// Escalations defines the escalation rules for Slack notifications
	// Each escalation specifies when and how to notify based on failure patterns
	Escalations []Escalation `json:"escalations,omitempty" yaml:"escalations,omitempty"`
}

// Escalation defines a single escalation rule for Slack notifications
// It specifies the conditions and actions for escalating failures
// +k8s:deepcopy-gen=true
type Escalation struct {
	// Name is the unique identifier for this escalation level
	Name string `json:"name" yaml:"name"`

	// Period defines the time window over which failures are counted
	Period string `json:"period" yaml:"period"`

	// MinFailures is the minimum number of failures required to trigger this escalation
	MinFailures int `json:"minFailures" yaml:"minFailures"`

	// Channel is the Slack channel where notifications will be sent
	Channel string `json:"channel" yaml:"channel"`

	// Mentions is a list of users or groups to mention in the notification
	Mentions []string `json:"mentions,omitempty" yaml:"mentions,omitempty"`
}

// BySlackEscalationName sorts a list of Escalations' by their Name
type BySlackEscalationName []Escalation

func (in BySlackEscalationName) Less(i, j int) bool {
	return in[i].Name < in[j].Name
}

func (in BySlackEscalationName) Len() int {
	return len(in)
}

func (in BySlackEscalationName) Swap(i, j int) {
	in[i], in[j] = in[j], in[i]
}
