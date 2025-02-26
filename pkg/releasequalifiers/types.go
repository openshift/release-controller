package releasequalifiers

import (
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications"
)

// QualifierId is a unique name that corresponds to a specific ReleaseQualifier definition
type QualifierId string

// PayloadBadgeStatus payload badge status
type PayloadBadgeStatus string

const (
	PayloadBadgeYes       PayloadBadgeStatus = "Yes"
	PayloadBadgeNo        PayloadBadgeStatus = "No"
	PayloadBadgeOnSuccess PayloadBadgeStatus = "OnSuccess"
	PayloadBadgeOnFailure PayloadBadgeStatus = "OnFailure"
)

// ReleaseQualifiers represents a collection of release qualifiers indexed by their unique names
// Each qualifier defines configuration for a specific component or feature
type ReleaseQualifiers map[QualifierId]ReleaseQualifier

// ReleaseQualifier defines the configuration for a single release qualifier
// It contains metadata about the qualifier and its notification settings
type ReleaseQualifier struct {
	// Enabled indicates whether this qualifier is currently active
	// Using a pointer to distinguish between "not set" and "set to false"
	Enabled *bool `json:"enabled,omitempty" yaml:"enabled,omitempty"`

	// BadgeName is the short display name used for this qualifier in UI badges
	BadgeName string `json:"badgeName,omitempty" yaml:"badgeName,omitempty"`

	// Summary provides a brief description of what this qualifier represents
	Summary string `json:"summary,omitempty" yaml:"summary,omitempty"`

	// Description contains detailed information about the qualifier for display in tooltips or detailed views
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// PayloadBadge indicates whether this qualifier should be included in payload badges
	PayloadBadge PayloadBadgeStatus `json:"payloadBadge,omitempty" yaml:"payloadBadge,omitempty"`

	// Labels the labels to apply when qualifying jobs fail
	Labels []string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// Notifications contains configuration for notification channels
	Notifications *notifications.Notifications `json:"notifications,omitempty" yaml:"notifications,omitempty"`
}

// Validate enforces any rules that all ReleaseQualifier objects must adhere to
func (rq ReleaseQualifier) Validate() error {
	return nil
}

// BoolPtr returns a pointer to the given bool value
// This is a utility function for creating pointers to bool values
func BoolPtr(b bool) *bool {
	return &b
}

// SortableQualifier represents a qualifier with its ID for sorting purposes
type SortableQualifier struct {
	ID        QualifierId
	Qualifier ReleaseQualifier
}

// SortableQualifiers is a slice of SortableQualifier for sorting
type SortableQualifiers []SortableQualifier

func (sq SortableQualifiers) Len() int {
	return len(sq)
}

func (sq SortableQualifiers) Less(i, j int) bool {
	return string(sq[i].ID) < string(sq[j].ID)
}

func (sq SortableQualifiers) Swap(i, j int) {
	sq[i], sq[j] = sq[j], sq[i]
}

// ToSortableSlice converts ReleaseQualifiers map to a sortable slice
func (rqs ReleaseQualifiers) ToSortableSlice() SortableQualifiers {
	slice := make(SortableQualifiers, 0, len(rqs))
	for id, qualifier := range rqs {
		slice = append(slice, SortableQualifier{
			ID:        id,
			Qualifier: qualifier,
		})
	}
	return slice
}

// ToMap converts a sorted slice back to a ReleaseQualifiers map
func (sq SortableQualifiers) ToMap() ReleaseQualifiers {
	result := make(ReleaseQualifiers)
	for _, item := range sq {
		result[item.ID] = item.Qualifier
	}
	return result
}
