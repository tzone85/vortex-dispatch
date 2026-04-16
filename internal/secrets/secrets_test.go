package secrets

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestEnvProvider_Get(t *testing.T) {
	os.Setenv("TEST_SECRET_KEY", "secret-value")
	defer os.Unsetenv("TEST_SECRET_KEY")

	p := NewEnvProvider()
	val, err := p.Get("TEST_SECRET_KEY")
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
	_, err := p.Get("TEST_NONEXISTENT_KEY")
	if err == nil {
		t.Error("expected error for missing env var")
	}
}

func TestEnvProvider_GetEmptyButSet(t *testing.T) {
	os.Setenv("TEST_EMPTY_KEY", "")
	defer os.Unsetenv("TEST_EMPTY_KEY")

	p := NewEnvProvider()
	val, err := p.Get("TEST_EMPTY_KEY")
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

	val, err := p.Get("ANTHROPIC_API_KEY")
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
	_, err := p.Get("ANY")
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
	_, err := p.Get("MISSING_KEY")
	if err == nil {
		t.Error("expected error for missing key")
	}
}

func TestVaultProvider_Name(t *testing.T) {
	if NewVaultProvider(VaultConfig{}).Name() != "vault" {
		t.Error("Name should be vault")
	}
}
