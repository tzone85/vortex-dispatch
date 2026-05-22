// Package ghost implements devdb.Provider for ghost.build's cloud Postgres service.
// Auth uses a Bearer token resolved from env (default GHOST_API_KEY) or an
// explicit value. The Ghost API is at https://api.ghost.build/v0.
package ghost

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/tzone85/vortex-dispatch/internal/devdb"
)

// Config holds construction-time settings for the Ghost provider.
type Config struct {
	// APIKey is required. Resolve before construction via ResolveAPIKey.
	APIKey string

	// SpaceID is optional; if empty the first space for the API key is used.
	SpaceID string

	// BaseURL defaults to DefaultBaseURL when empty.
	BaseURL string

	// Timeout for individual HTTP calls. Defaults to 30s.
	Timeout time.Duration

	// UserAgent overrides the default "vxd/devdb-ghost".
	UserAgent string
}

// Provider implements devdb.Provider backed by ghost.build cloud.
type Provider struct {
	client  *Client
	mu      sync.Mutex
	spaceID string // cached lazily after first ResolveSpaceID call
}

// New validates cfg and returns a ready Provider.
// Returns an error when APIKey is empty.
func New(cfg Config) (*Provider, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("ghost: APIKey required (resolve via ResolveAPIKey first)")
	}
	p := &Provider{
		client: NewClient(ClientConfig{
			APIKey:    cfg.APIKey,
			BaseURL:   cfg.BaseURL,
			UserAgent: cfg.UserAgent,
			Timeout:   cfg.Timeout,
		}),
		spaceID: cfg.SpaceID,
	}
	return p, nil
}

// Name returns "ghost".
func (p *Provider) Name() string { return "ghost" }

// Ping verifies the Ghost API is reachable.
func (p *Provider) Ping(ctx context.Context) error {
	return p.client.Ping(ctx)
}

// Create provisions an empty Postgres database via the Ghost cloud API.
func (p *Provider) Create(ctx context.Context, opts devdb.CreateOpts) (devdb.DB, error) {
	if !devdb.IsValid(opts.Name) {
		return devdb.DB{}, fmt.Errorf("%w: %q", devdb.ErrInvalidName, opts.Name)
	}
	space, err := p.resolveSpace(ctx)
	if err != nil {
		return devdb.DB{}, err
	}
	d, err := p.client.CreateDB(ctx, space, opts.Name)
	if err != nil {
		return devdb.DB{}, err
	}
	if opts.WaitReady {
		d, err = p.waitReady(ctx, space, d.ID, opts.WaitTimeout)
		if err != nil {
			return devdb.DB{}, err
		}
	}
	return p.toDevDB(d, opts), nil
}

// Fork creates a copy of template (looked up by name) and returns the new DB.
func (p *Provider) Fork(ctx context.Context, template string, opts devdb.CreateOpts) (devdb.DB, error) {
	if !devdb.IsValid(opts.Name) {
		return devdb.DB{}, fmt.Errorf("%w: %q", devdb.ErrInvalidName, opts.Name)
	}
	space, err := p.resolveSpace(ctx)
	if err != nil {
		return devdb.DB{}, err
	}

	// Resolve template name → ID.
	list, err := p.client.ListDBs(ctx, space)
	if err != nil {
		return devdb.DB{}, err
	}
	var tplRef string
	for _, db := range list {
		if db.Name == template {
			tplRef = db.ID
			break
		}
	}
	if tplRef == "" {
		return devdb.DB{}, fmt.Errorf("%w: %q", devdb.ErrTemplateMiss, template)
	}

	d, err := p.client.ForkDB(ctx, space, tplRef, opts.Name)
	if err != nil {
		return devdb.DB{}, err
	}
	if opts.WaitReady {
		d, err = p.waitReady(ctx, space, d.ID, opts.WaitTimeout)
		if err != nil {
			return devdb.DB{}, err
		}
	}
	return p.toDevDB(d, opts), nil
}

// Delete removes the database permanently. dbID is the opaque ID returned by
// Create or List (not the human-readable name).
func (p *Provider) Delete(ctx context.Context, dbID string) error {
	space, err := p.resolveSpace(ctx)
	if err != nil {
		return err
	}
	return p.client.DeleteDB(ctx, space, dbID)
}

// List returns all databases visible to this API key in the resolved space.
func (p *Provider) List(ctx context.Context) ([]devdb.DB, error) {
	space, err := p.resolveSpace(ctx)
	if err != nil {
		return nil, err
	}
	list, err := p.client.ListDBs(ctx, space)
	if err != nil {
		return nil, err
	}
	out := make([]devdb.DB, 0, len(list))
	for _, d := range list {
		out = append(out, p.toDevDB(d, devdb.CreateOpts{}))
	}
	return out, nil
}

// Schema is not yet implemented for the Ghost provider. The Ghost API does not
// expose a schema-dump endpoint; a pgx-based implementation is planned for
// Phase-2 SP2 work.
func (p *Provider) Schema(_ context.Context, _ string) (string, error) {
	return "", fmt.Errorf("ghost.Provider.Schema: not yet implemented: %w", devdb.ErrUnsupported)
}

// resolveSpace returns the cached space ID, fetching it from the API if necessary.
func (p *Provider) resolveSpace(ctx context.Context) (string, error) {
	p.mu.Lock()
	cached := p.spaceID
	p.mu.Unlock()
	if cached != "" {
		return cached, nil
	}

	s, err := p.client.ResolveSpaceID(ctx)
	if err != nil {
		return "", err
	}

	p.mu.Lock()
	p.spaceID = s
	p.mu.Unlock()
	return s, nil
}

// waitReady polls until the DB's status is "running" / "ready" or timeout fires.
func (p *Provider) waitReady(ctx context.Context, space, dbID string, timeout time.Duration) (dbResponse, error) {
	if timeout == 0 {
		timeout = 60 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		d, err := p.client.GetDB(ctx, space, dbID)
		if err != nil {
			return dbResponse{}, err
		}
		// Treat missing status (empty string) as running — some Ghost responses
		// omit the field once the DB is immediately available.
		switch d.Status {
		case "running", "ready", "":
			return d, nil
		}
		select {
		case <-ctx.Done():
			return dbResponse{}, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return dbResponse{}, fmt.Errorf("ghost: db %s not ready within %s: %w", dbID, timeout, devdb.ErrProviderDown)
}

// toDevDB converts a Ghost API response to the canonical devdb.DB type.
func (p *Provider) toDevDB(d dbResponse, opts devdb.CreateOpts) devdb.DB {
	return devdb.DB{
		ID:               d.ID,
		Name:             d.Name,
		Provider:         "ghost",
		ConnectionString: d.DSN,
		CreatedAt:        time.Now().UTC(),
		Labels:           opts.Labels,
	}
}
