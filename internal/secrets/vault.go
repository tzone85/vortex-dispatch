package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VaultProvider reads secrets from HashiCorp Vault via the HTTP API.
// Uses KV v2 (the modern default). Path format: secret/data/<path>
type VaultProvider struct {
	addr       string // e.g., http://127.0.0.1:8200
	token      string
	mountPath  string // default "secret"
	secretPath string // default "vxd"
	client     *http.Client
}

// VaultConfig configures a VaultProvider.
type VaultConfig struct {
	Addr       string // e.g., "http://127.0.0.1:8200"
	Token      string // X-Vault-Token
	MountPath  string // KV v2 mount, default "secret"
	SecretPath string // path within mount, default "vxd"
}

// NewVaultProvider creates a Vault provider. Does NOT verify connectivity —
// errors surface on first Get() call.
func NewVaultProvider(cfg VaultConfig) *VaultProvider {
	mount := cfg.MountPath
	if mount == "" {
		mount = "secret"
	}
	path := cfg.SecretPath
	if path == "" {
		path = "vxd"
	}
	return &VaultProvider{
		addr:       strings.TrimRight(cfg.Addr, "/"),
		token:      cfg.Token,
		mountPath:  mount,
		secretPath: path,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Get fetches the secret value for key from the configured Vault path.
// Vault KV v2 stores secrets as a map at /v1/{mount}/data/{path}.
//
// The HTTP request is bound to ctx, so cancelling the caller's context
// tears down the in-flight request immediately rather than waiting out
// the client's 10 s safety timeout.
func (p *VaultProvider) Get(ctx context.Context, key string) (string, error) {
	url := fmt.Sprintf("%s/v1/%s/data/%s", p.addr, p.mountPath, p.secretPath)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Vault-Token", p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vault request: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("vault returned %d: %s", resp.StatusCode, string(body))
	}

	// KV v2 response: { "data": { "data": { "key1": "value1" } } }
	var parsed struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("parse vault response: %w", err)
	}

	val, ok := parsed.Data.Data[key]
	if !ok {
		return "", fmt.Errorf("key %q not found at vault path %s/data/%s", key, p.mountPath, p.secretPath)
	}
	return val, nil
}

// Name returns the provider name for logging.
func (p *VaultProvider) Name() string { return "vault" }
