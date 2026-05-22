package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
)

// ClientConfig configures the Docker HTTP client.
// BaseURL defaults to the Unix-socket transport ("/var/run/docker.sock");
// tests pass an httptest URL instead.
type ClientConfig struct {
	BaseURL string
	Timeout time.Duration
}

// Client is a thin wrapper around the Docker Engine HTTP API.
// Only the subset of endpoints we need is exposed.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient returns a ready-to-use Docker client. If cfg.BaseURL is empty
// we dial the default Unix socket.
func NewClient(cfg ClientConfig) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	transport := &http.Transport{}
	baseURL := cfg.BaseURL
	if baseURL == "" {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := net.Dialer{}
			return d.DialContext(ctx, "unix", "/var/run/docker.sock")
		}
		baseURL = "http://docker"
	}
	return &Client{
		httpClient: &http.Client{Transport: transport, Timeout: cfg.Timeout},
		baseURL:    baseURL,
	}
}

// Ping verifies the Docker daemon is reachable. Unreachability is mapped to
// devdb.ErrProviderDown so callers can errors.Is.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_ping", nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("docker ping: %w", devdb.ErrProviderDown)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("docker ping %d: %w", resp.StatusCode, devdb.ErrProviderDown)
	}
	return nil
}
