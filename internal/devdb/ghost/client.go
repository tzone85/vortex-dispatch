package ghost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// spacePath builds a Ghost API path under /spaces, percent-escaping each
// dynamic segment so a name/ref/template cannot inject extra path segments or
// traverse the URL (defense-in-depth atop the devdb.IsValid allowlist applied
// upstream). segments are appended after /spaces/<spaceID>/databases.
func (c *Client) spacePath(spaceID string, segments ...string) string {
	p := c.baseURL + "/spaces/" + url.PathEscape(spaceID) + "/databases"
	for _, s := range segments {
		p += "/" + url.PathEscape(s)
	}
	return p
}

// DefaultBaseURL is the canonical Ghost cloud API endpoint.
const DefaultBaseURL = "https://api.ghost.build/v0"

// Client is a thin HTTP wrapper around the Ghost cloud API.
// It handles bearer-token auth, one 5xx retry with 500ms backoff,
// and Retry-After header on 429.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	userAgent  string
}

// ClientConfig holds parameters for NewClient.
type ClientConfig struct {
	BaseURL   string
	APIKey    string
	UserAgent string
	Timeout   time.Duration
}

// NewClient creates a Client from cfg, applying sensible defaults for empty fields.
func NewClient(cfg ClientConfig) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "vxd/devdb-ghost"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	return &Client{
		httpClient: &http.Client{Timeout: cfg.Timeout},
		baseURL:    cfg.BaseURL,
		apiKey:     cfg.APIKey,
		userAgent:  cfg.UserAgent,
	}
}

// do executes the request with bearer auth. It retries once on 5xx (500ms backoff)
// and respects Retry-After on 429.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")

	var (
		resp *http.Response
		err  error
	)
	for attempt := 0; attempt < 2; attempt++ {
		// Clone request body for retry (only needed when body present).
		if attempt > 0 && req.GetBody != nil {
			body, bodyErr := req.GetBody()
			if bodyErr != nil {
				return nil, bodyErr
			}
			req.Body = body
		}

		resp, err = c.httpClient.Do(req)
		if err != nil {
			// Transport error — retry once.
			if attempt == 0 {
				time.Sleep(500 * time.Millisecond)
				continue
			}
			return nil, err
		}

		switch {
		case resp.StatusCode == 429:
			wait := 500 * time.Millisecond
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, pErr := strconv.Atoi(ra); pErr == nil {
					wait = time.Duration(secs) * time.Second
				}
			}
			resp.Body.Close()
			if attempt == 0 {
				time.Sleep(wait)
				continue
			}
			// On second attempt still 429 — return the error via wrapHTTPError below.
			// Re-issue the request one more time just to get the body.
			resp, err = c.httpClient.Do(req)
			if err != nil {
				return nil, err
			}
			return resp, nil

		case resp.StatusCode >= 500 && resp.StatusCode < 600 && attempt == 0:
			resp.Body.Close()
			time.Sleep(500 * time.Millisecond)
			continue
		}

		return resp, nil
	}
	return resp, err
}

// dbResponse is the subset of Ghost's DB JSON we parse.
type dbResponse struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	DSN    string `json:"dsn"`
}

// Ping calls GET /health and returns an error if the service is unreachable.
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("ghost ping: build request: %w", err)
	}
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("ghost ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return wrapHTTPError(resp)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// ResolveSpaceID returns the first space ID associated with the API key.
// Callers should cache the result.
func (c *Client) ResolveSpaceID(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/spaces", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", wrapHTTPError(resp)
	}
	var body struct {
		Spaces []struct {
			ID string `json:"id"`
		} `json:"spaces"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("ghost: decode spaces response: %w", err)
	}
	if len(body.Spaces) == 0 {
		return "", fmt.Errorf("ghost: no spaces available for this API key")
	}
	return body.Spaces[0].ID, nil
}

// CreateDB creates a new database in spaceID with the given name.
func (c *Client) CreateDB(ctx context.Context, spaceID, name string) (dbResponse, error) {
	payload, _ := json.Marshal(map[string]string{"name": name})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.spacePath(spaceID),
		bytes.NewReader(payload))
	if err != nil {
		return dbResponse{}, err
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return dbResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return dbResponse{}, wrapHTTPError(resp)
	}
	var d dbResponse
	return d, json.NewDecoder(resp.Body).Decode(&d)
}

// ForkDB forks templateRef into a new database named name.
func (c *Client) ForkDB(ctx context.Context, spaceID, templateRef, name string) (dbResponse, error) {
	payload, _ := json.Marshal(map[string]string{"name": name})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.spacePath(spaceID, templateRef, "fork"),
		bytes.NewReader(payload))
	if err != nil {
		return dbResponse{}, err
	}
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return dbResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return dbResponse{}, wrapHTTPError(resp)
	}
	var d dbResponse
	return d, json.NewDecoder(resp.Body).Decode(&d)
}

// DeleteDB deletes the database identified by ref in spaceID.
func (c *Client) DeleteDB(ctx context.Context, spaceID, ref string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.spacePath(spaceID, ref), nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return wrapHTTPError(resp)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// ListDBs returns all databases in spaceID.
func (c *Client) ListDBs(ctx context.Context, spaceID string) ([]dbResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.spacePath(spaceID), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, wrapHTTPError(resp)
	}
	var body struct {
		Databases []dbResponse `json:"databases"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("ghost: decode databases response: %w", err)
	}
	return body.Databases, nil
}

// GetDB fetches a single database by ref from spaceID.
func (c *Client) GetDB(ctx context.Context, spaceID, ref string) (dbResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.spacePath(spaceID, ref), nil)
	if err != nil {
		return dbResponse{}, err
	}
	resp, err := c.do(req)
	if err != nil {
		return dbResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return dbResponse{}, wrapHTTPError(resp)
	}
	var d dbResponse
	return d, json.NewDecoder(resp.Body).Decode(&d)
}
