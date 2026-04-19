package notify

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNoopNotifier_NeverErrors(t *testing.T) {
	n := NewNoopNotifier()
	if err := n.Notify(context.Background(), Message{Title: "x"}); err != nil {
		t.Errorf("noop should not error: %v", err)
	}
	if n.Name() != "noop" {
		t.Errorf("Name() = %q, want noop", n.Name())
	}
}

func TestSlackNotifier_PostsCorrectPayload(t *testing.T) {
	var received string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing or wrong Content-Type")
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]string
		json.Unmarshal(body, &payload)
		received = payload["text"]
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewSlackNotifier(srv.URL)
	err := n.Notify(context.Background(), Message{
		Title:    "SLA breach: s-001",
		Body:     "Story exceeded 4hr limit",
		Severity: "warn",
		Fields:   map[string]string{"complexity": "3", "elapsed": "5h0m0s"},
	})
	if err != nil {
		t.Fatalf("Notify: %v", err)
	}

	if !strings.Contains(received, ":warning:") {
		t.Error("warn severity should include warning emoji")
	}
	if !strings.Contains(received, "SLA breach: s-001") {
		t.Error("title missing")
	}
	if !strings.Contains(received, "Story exceeded 4hr limit") {
		t.Error("body missing")
	}
	if !strings.Contains(received, "complexity") {
		t.Error("fields missing")
	}
}

func TestSlackNotifier_EmptyURL(t *testing.T) {
	n := NewSlackNotifier("")
	err := n.Notify(context.Background(), Message{Title: "x"})
	if err == nil {
		t.Error("empty URL should return error")
	}
}

func TestSlackNotifier_NonOKResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n := NewSlackNotifier(srv.URL)
	err := n.Notify(context.Background(), Message{Title: "x"})
	if err == nil {
		t.Error("non-2xx should return error")
	}
}

func TestSlackNotifier_Name(t *testing.T) {
	if NewSlackNotifier("").Name() != "slack" {
		t.Error("Name should be slack")
	}
}

func TestSeverityEmoji(t *testing.T) {
	tests := map[string]string{
		"error":   ":rotating_light:",
		"warn":    ":warning:",
		"info":    ":information_source:",
		"unknown": "",
	}
	for sev, want := range tests {
		if got := severityEmoji(sev); got != want {
			t.Errorf("severityEmoji(%q) = %q, want %q", sev, got, want)
		}
	}
}

func TestWebhookNotifier_HappyPath(t *testing.T) {
	var gotBody []byte
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotHeaders = r.Header
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, "")
	msg := Message{
		Title:     "Story done",
		Body:      "s-001 completed",
		EventType: "STORY_COMPLETED",
		Fields:    map[string]string{"story": "s-001"},
	}
	if err := n.Notify(context.Background(), msg); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	var received Message
	if err := json.Unmarshal(gotBody, &received); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if received.Title != "Story done" {
		t.Errorf("Title = %q, want %q", received.Title, "Story done")
	}
	if gotHeaders.Get("X-VXD-Event") != "STORY_COMPLETED" {
		t.Errorf("X-VXD-Event = %q, want STORY_COMPLETED", gotHeaders.Get("X-VXD-Event"))
	}
	if gotHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotHeaders.Get("Content-Type"))
	}
	if n.Name() != "webhook" {
		t.Errorf("Name() = %q, want webhook", n.Name())
	}
}

func TestWebhookNotifier_NonOKReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, "")
	err := n.Notify(context.Background(), Message{Title: "x"})
	if err == nil {
		t.Fatal("non-2xx should return error")
	}
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

func TestWebhookNotifier_HMACSignature(t *testing.T) {
	secret := "test-secret-key"
	var gotBody []byte
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-VXD-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, secret)
	if err := n.Notify(context.Background(), Message{Title: "signed", EventType: "TEST"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(gotBody)
	want := hex.EncodeToString(mac.Sum(nil))

	if gotSig != want {
		t.Errorf("signature mismatch:\n  got  %s\n  want %s", gotSig, want)
	}
}

func TestWebhookNotifier_NoSignatureWhenNoSecret(t *testing.T) {
	var gotSig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-VXD-Signature")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n := NewWebhookNotifier(srv.URL, "")
	if err := n.Notify(context.Background(), Message{Title: "unsigned"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if gotSig != "" {
		t.Errorf("X-VXD-Signature should be empty when no secret, got %q", gotSig)
	}
}
