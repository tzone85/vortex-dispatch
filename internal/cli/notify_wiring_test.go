package cli

import (
	"reflect"
	"testing"

	"github.com/tzone85/vortex-dispatch/internal/config"
)

// TestNotificationAllowlist pins the notifier gating semantics (audit findings
// W-02/W-03): a configured webhook enables the notifier; PIPELINE_STALLED is
// always sent (it is the human-intervention signal); notify_on_sla gates SLA
// breaches; notify_on_complete gates terminal requirement outcomes
// (REQ_COMPLETED and REQ_BLOCKED). No webhook → no notifier at all.
func TestNotificationAllowlist(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.NotifyConfig
		want []string
	}{
		{
			name: "no webhook disables everything regardless of flags",
			cfg:  config.NotifyConfig{NotifyOnSLA: true, NotifyOnComplete: true},
			want: nil,
		},
		{
			name: "webhook alone still sends stall alerts",
			cfg:  config.NotifyConfig{SlackWebhookURL: "https://hooks.slack.example/x"},
			want: []string{"PIPELINE_STALLED"},
		},
		{
			name: "notify_on_sla adds SLA breaches",
			cfg:  config.NotifyConfig{SlackWebhookURL: "https://hooks.slack.example/x", NotifyOnSLA: true},
			want: []string{"PIPELINE_STALLED", "STORY_SLA_BREACHED"},
		},
		{
			name: "notify_on_complete adds terminal requirement outcomes",
			cfg:  config.NotifyConfig{SlackWebhookURL: "https://hooks.slack.example/x", NotifyOnComplete: true},
			want: []string{"PIPELINE_STALLED", "REQ_COMPLETED", "REQ_BLOCKED"},
		},
		{
			name: "both flags",
			cfg:  config.NotifyConfig{SlackWebhookURL: "https://hooks.slack.example/x", NotifyOnSLA: true, NotifyOnComplete: true},
			want: []string{"PIPELINE_STALLED", "STORY_SLA_BREACHED", "REQ_COMPLETED", "REQ_BLOCKED"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := notificationAllowlist(tc.cfg)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("notificationAllowlist(%+v) = %v, want %v", tc.cfg, got, tc.want)
			}
		})
	}
}
