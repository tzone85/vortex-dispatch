package figma

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is the Figma REST API root.
const DefaultBaseURL = "https://api.figma.com"

// requestTimeout bounds a single API or image-download call.
const requestTimeout = 60 * time.Second

// maxImageBytes caps a single render download (a 2x PNG of a large frame is
// a few MB; anything beyond this is a mis-render, not a design).
const maxImageBytes = 20 << 20

// Client is a minimal Figma REST API client (personal-access-token auth).
type Client struct {
	BaseURL string
	http    *http.Client
	token   string
}

// NewClient builds a client with the given token (X-Figma-Token header).
func NewClient(token string) *Client {
	return &Client{
		BaseURL: DefaultBaseURL,
		http:    &http.Client{Timeout: requestTimeout},
		token:   token,
	}
}

// Me identifies the token's user — used to validate a token interactively.
type Me struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Handle string `json:"handle"`
}

// Me calls GET /v1/me and returns the authenticated user.
func (c *Client) Me(ctx context.Context) (Me, error) {
	var me Me
	if err := c.getJSON(ctx, "/v1/me", nil, &me); err != nil {
		return Me{}, err
	}
	return me, nil
}

// Node is the subset of Figma's node tree the design context needs: names,
// types, dimensions, text styles, and solid fills.
type Node struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Children []Node `json:"children,omitempty"`
	Style    *struct {
		FontFamily string  `json:"fontFamily"`
		FontSize   float64 `json:"fontSize"`
		FontWeight float64 `json:"fontWeight"`
	} `json:"style,omitempty"`
	Fills []struct {
		Type  string `json:"type"`
		Color *struct {
			R, G, B, A float64
		} `json:"color,omitempty"`
	} `json:"fills,omitempty"`
	AbsoluteBoundingBox *struct {
		Width  float64 `json:"width"`
		Height float64 `json:"height"`
	} `json:"absoluteBoundingBox,omitempty"`
}

// fileNodesResponse is GET /v1/files/:key/nodes.
type fileNodesResponse struct {
	Name  string `json:"name"`
	Nodes map[string]struct {
		Document Node `json:"document"`
	} `json:"nodes"`
}

// FileNodes fetches the named nodes (or the document root when ids is empty
// is not supported — callers pass "0:0" for whole-file) from a file.
func (c *Client) FileNodes(ctx context.Context, fileKey string, ids []string) (name string, nodes []Node, err error) {
	q := url.Values{"ids": {joinIDs(ids)}, "depth": {"3"}}
	var resp fileNodesResponse
	if err := c.getJSON(ctx, "/v1/files/"+url.PathEscape(fileKey)+"/nodes", q, &resp); err != nil {
		return "", nil, err
	}
	for _, n := range resp.Nodes {
		nodes = append(nodes, n.Document)
	}
	return resp.Name, nodes, nil
}

// imagesResponse is GET /v1/images/:key.
type imagesResponse struct {
	Err    *string           `json:"err"`
	Images map[string]string `json:"images"`
}

// ImageURLs asks Figma to render the given nodes as PNG and returns
// node-id → short-lived download URL.
func (c *Client) ImageURLs(ctx context.Context, fileKey string, ids []string) (map[string]string, error) {
	q := url.Values{"ids": {joinIDs(ids)}, "format": {"png"}, "scale": {"2"}}
	var resp imagesResponse
	if err := c.getJSON(ctx, "/v1/images/"+url.PathEscape(fileKey), q, &resp); err != nil {
		return nil, err
	}
	if resp.Err != nil && *resp.Err != "" {
		return nil, fmt.Errorf("figma image render: %s", *resp.Err)
	}
	return resp.Images, nil
}

// Download fetches a rendered image URL (already signed by Figma) into
// memory, bounded by maxImageBytes. The destination is validated against
// Figma's CDN hosts (or the configured BaseURL host, for tests) so a
// tampered API response cannot point the download at an internal address.
func (c *Client) Download(ctx context.Context, imageURL string) ([]byte, error) {
	if err := c.validateDownloadURL(imageURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download render: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
}

// getJSON performs an authenticated GET and decodes the JSON response.
func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Figma-Token", c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// Body may carry {"err": "..."} — include a short prefix, never the token.
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("figma API %s: HTTP %d: %s", path, resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// validateDownloadURL accepts Figma CDN hosts over https, plus the BaseURL
// host verbatim (httptest fixtures).
func (c *Client) validateDownloadURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("render URL: %w", err)
	}
	if base, baseErr := url.Parse(c.BaseURL); baseErr == nil && base.Host != "" && u.Host == base.Host {
		return nil
	}
	if u.Scheme != "https" {
		return fmt.Errorf("render URL must be https, got %q", u.Scheme)
	}
	host := strings.ToLower(u.Hostname())
	if host == "figma.com" || strings.HasSuffix(host, ".figma.com") || strings.HasSuffix(host, ".figmausercontent.com") {
		return nil
	}
	return fmt.Errorf("render URL host %q is not a Figma CDN — refusing download", host)
}

func joinIDs(ids []string) string {
	return strings.Join(ids, ",")
}
