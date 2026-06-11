package secrets

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestEnvProvider_Get(t *testing.T) {
	os.Setenv("TEST_SECRET_KEY", "secret-value")
	defer os.Unsetenv("TEST_SECRET_KEY")

	p := NewEnvProvider()
	val, err := p.Get(context.Background(), "TEST_SECRET_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "secret-value" {
		t.Errorf("Get = %q, want %q", val, "secret-value")
	}
}

func TestEnvProvider_GetMissing(t *testing.T) {
	os.Unsetenv("TEST_NONEXISTENT_KEY")
	p := NewEnvProvider()
	_, err := p.Get(context.Background(), "TEST_NONEXISTENT_KEY")
	if err == nil {
		t.Error("expected error for missing env var")
	}
}

func TestEnvProvider_GetEmptyButSet(t *testing.T) {
	os.Setenv("TEST_EMPTY_KEY", "")
	defer os.Unsetenv("TEST_EMPTY_KEY")

	p := NewEnvProvider()
	val, err := p.Get(context.Background(), "TEST_EMPTY_KEY")
	if err != nil {
		t.Fatalf("empty-but-set should not error: %v", err)
	}
	if val != "" {
		t.Errorf("Get = %q, want empty", val)
	}
}

func TestEnvProvider_Name(t *testing.T) {
	if NewEnvProvider().Name() != "env" {
		t.Error("Name should be env")
	}
}

func TestVaultProvider_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/data/vxd" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("X-Vault-Token") != "test-token" {
			t.Error("missing or wrong X-Vault-Token")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{"data":{"ANTHROPIC_API_KEY":"sk-ant-fake"}}}`))
	}))
	defer server.Close()

	p := NewVaultProvider(VaultConfig{
		Addr:  server.URL,
		Token: "test-token",
	})

	val, err := p.Get(context.Background(), "ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "sk-ant-fake" {
		t.Errorf("Get = %q, want sk-ant-fake", val)
	}
}

func TestVaultProvider_GetUnreachable(t *testing.T) {
	p := NewVaultProvider(VaultConfig{
		Addr:  "http://127.0.0.1:1", // unreachable
		Token: "token",
	})
	_, err := p.Get(context.Background(), "ANY")
	if err == nil {
		t.Error("expected error for unreachable Vault")
	}
}

func TestVaultProvider_GetMissingKey(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data":{"data":{"OTHER_KEY":"x"}}}`))
	}))
	defer server.Close()

	p := NewVaultProvider(VaultConfig{Addr: server.URL, Token: "t"})
	_, err := p.Get(context.Background(), "MISSING_KEY")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

// TestVaultProvider_GetRespectsCanceledContext verifies that a caller's
// ctx cancellation tears down the request promptly — the original
// hazard before this fix was a slow Vault stalling the goroutine until
// the client's 10 s timeout fired regardless of caller context.
func TestVaultProvider_GetRespectsCanceledContext(t *testing.T) {
	// Server blocks until its context cancels (i.e., forever for the test's
	// timeout). The Vault client will inherit the test's deadline via ctx.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	p := NewVaultProvider(VaultConfig{Addr: server.URL, Token: "t"})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := p.Get(ctx, "ANY")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from cancelled ctx, got nil")
	}
	// Must return before the 10 s client timeout (well under 1 s slack).
	if elapsed > time.Second {
		t.Errorf("Get did not honor ctx cancellation: elapsed %v", elapsed)
	}
}

func TestVaultProvider_Name(t *testing.T) {
	if NewVaultProvider(VaultConfig{}).Name() != "vault" {
		t.Error("Name should be vault")
	}
}
