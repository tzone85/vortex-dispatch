// Package notify provides outbound webhook notifications for VXD events.
// Used to surface SLA breaches, story completions, and pipeline failures
// to Slack, Discord, or generic webhook endpoints.
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Notifier sends notifications to a webhook URL.
type Notifier interface {
	Notify(ctx context.Context, msg Message) error
	Name() string
}

// Message is the payload for a notification.
type Message struct {
	Title     string            // Short subject line (e.g., "SLA breach: s-001")
	Body      string            // Multi-line body
	Severity  string            // "info", "warn", "error"
	EventType string            // Event type (e.g., "STORY_COMPLETED")
	Fields    map[string]string // Key-value pairs for structured display
}

// NoopNotifier discards all notifications. Used as a default when
// notifications are disabled.
type NoopNotifier struct{}

// NewNoopNotifier returns a Notifier that discards messages.
func NewNoopNotifier() *NoopNotifier { return &NoopNotifier{} }

// Notify always returns nil.
func (n *NoopNotifier) Notify(_ context.Context, _ Message) error { return nil }

// Name returns the notifier name.
func (n *NoopNotifier) Name() string { return "noop" }

// SlackNotifier posts to a Slack incoming webhook URL.
// See: https://api.slack.com/messaging/webhooks
type SlackNotifier struct {
	webhookURL string
	client     *http.Client
}

// NewSlackNotifier creates a Slack webhook notifier.
func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Notify posts a message to the configured Slack webhook using mrkdwn.
// Returns an error if the webhook is unreachable or returns non-2xx.
func (n *SlackNotifier) Notify(ctx context.Context, msg Message) error {
	if n.webhookURL == "" {
		return fmt.Errorf("slack webhook URL not configured")
	}

	emoji := severityEmoji(msg.Severity)
	text := fmt.Sprintf("%s *%s*\n%s", emoji, msg.Title, msg.Body)

	// Add fields as a bullet list under the body
	for k, v := range msg.Fields {
		text += fmt.Sprintf("\n• *%s*: %s", k, v)
	}

	body, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return fmt.Errorf("marshal slack payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build slack request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("slack webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned %d", resp.StatusCode)
	}
	return nil
}

// Name returns the notifier name.
func (n *SlackNotifier) Name() string { return "slack" }

// severityEmoji maps severity to an emoji prefix for visual scanning.
func severityEmoji(sev string) string {
	switch sev {
	case "error":
		return ":rotating_light:"
	case "warn":
		return ":warning:"
	case "info":
		return ":information_source:"
	default:
		return ""
	}
}

// WebhookNotifier posts JSON to a generic webhook URL with optional
// HMAC-SHA256 signature verification.
type WebhookNotifier struct {
	url    string
	secret string
	client *http.Client
}

// NewWebhookNotifier creates a generic webhook notifier. If secret is
// non-empty, an HMAC-SHA256 signature is included in X-VXD-Signature.
func NewWebhookNotifier(url, secret string) *WebhookNotifier {
	return &WebhookNotifier{
		url:    url,
		secret: secret,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

// Notify posts the Message as JSON to the configured webhook URL.
func (n *WebhookNotifier) Notify(ctx context.Context, msg Message) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-VXD-Event", msg.EventType)

	if n.secret != "" {
		mac := hmac.New(sha256.New, []byte(n.secret))
		mac.Write(body)
		req.Header.Set("X-VXD-Signature", hex.EncodeToString(mac.Sum(nil)))
	}

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}
	return nil
}

// Name returns the notifier name.
func (n *WebhookNotifier) Name() string { return "webhook" }
