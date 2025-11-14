// +k8s:deepcopy-gen=package
// +k8s:defaulter-gen=TypeMeta

package releasequalifiers

import (
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications"
)

// QualifierId is a unique name that corresponds to a specific ReleaseQualifier definition
type QualifierId string

// ReleaseQualifiers represents a collection of release qualifiers indexed by their unique names
// Each qualifier defines configuration for a specific component or feature
type ReleaseQualifiers map[QualifierId]ReleaseQualifier

// ReleaseQualifier defines the configuration for a single release qualifier
// It contains metadata about the qualifier and its notification settings
// +k8s:deepcopy-gen=true
type ReleaseQualifier struct {
	// Enabled indicates whether this qualifier is currently active
	// Using a pointer to distinguish between "not set" and "set to false"
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`

	// BadgeName short name displayed, as UI badges, in job level summaries
	BadgeName string `json:"badgeName,omitempty" yaml:"badgeName,omitempty"`

	// Summary provides a brief description of what this qualifier represents
	Summary string `json:"summary,omitempty" yaml:"summary,omitempty"`

	// Description contains detailed information about the qualifier for display in tooltips or detailed views
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// PayloadBadgeStatus indicates if/when the qualifier's BadgeName should be displayed at the ReleasePayload level
	PayloadBadgeStatus BadgeStatus `json:"payloadBadgeStatus,omitempty" yaml:"payloadBadgeStatus,omitempty"`

	// Labels the labels to apply when qualifying jobs fail
	Labels []string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// Notifications contains configuration for notification channels
	Notifications *notifications.Notifications `json:"notifications,omitempty" yaml:"notifications,omitempty"`
}

// BadgeStatus badge status used to indicate if/when a ReleaseQualifier badge should be displayed
type BadgeStatus string

const (
	BadgeStatusYes       BadgeStatus = "Yes"
	BadgeStatusNo        BadgeStatus = "No"
	BadgeStatusOnSuccess BadgeStatus = "OnSuccess"
	BadgeStatusOnFailure BadgeStatus = "OnFailure"
)

// BoolPtr returns a pointer to the given bool value
// This is a utility function for creating pointers to bool values
func BoolPtr(b bool) *bool {
	return &b
}
