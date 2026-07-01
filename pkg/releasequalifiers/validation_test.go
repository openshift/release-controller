package releasequalifiers

import (
	"testing"

	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications"
	"github.com/openshift/release-controller/pkg/releasequalifiers/notifications/jira"
)

func TestReleaseQualifier_Validate(t *testing.T) {
	tests := []struct {
		name      string
		qualifier ReleaseQualifier
		wantErr   bool
	}{
		{
			name:      "empty qualifier is valid",
			qualifier: ReleaseQualifier{},
			wantErr:   false,
		},
		{
			name: "non-approval qualifier with all fields is valid",
			qualifier: ReleaseQualifier{
				Enabled:            BoolPtr(true),
				BadgeName:          "TEST",
				Summary:            "Test Summary",
				Description:        "Test Description",
				PayloadBadgeStatus: BadgeStatusOnSuccess,
				FailureLabels:      []string{"label1"},
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{Project: "TEST"},
				},
			},
			wantErr: false,
		},
		{
			name: "approval=false with notifications is valid",
			qualifier: ReleaseQualifier{
				Approval: BoolPtr(false),
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{Project: "TEST"},
				},
			},
			wantErr: false,
		},
		{
			name: "approval qualifier with only allowed fields is valid",
			qualifier: ReleaseQualifier{
				Approval:           BoolPtr(true),
				Enabled:            BoolPtr(true),
				BadgeName:          "Approval Badge",
				Summary:            "Team Approval",
				Description:        "Earned via team approval",
				PayloadBadgeStatus: BadgeStatusOnSuccess,
			},
			wantErr: false,
		},
		{
			name: "approval qualifier with failureLabels is invalid",
			qualifier: ReleaseQualifier{
				Approval:      BoolPtr(true),
				Enabled:       BoolPtr(true),
				BadgeName:     "Approval Badge",
				FailureLabels: []string{"some-label"},
			},
			wantErr: true,
		},
		{
			name: "approval qualifier with notifications is invalid",
			qualifier: ReleaseQualifier{
				Approval:  BoolPtr(true),
				Enabled:   BoolPtr(true),
				BadgeName: "Approval Badge",
				Notifications: &notifications.Notifications{
					Jira: &jira.Notification{Project: "TEST"},
				},
			},
			wantErr: true,
		},
		{
			name: "approval qualifier with empty failureLabels is valid",
			qualifier: ReleaseQualifier{
				Approval:      BoolPtr(true),
				Enabled:       BoolPtr(true),
				FailureLabels: []string{},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.qualifier.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
