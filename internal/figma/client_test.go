package figma

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture server: /v1/me, /v1/files/:key/nodes, /v1/images/:key, and a PNG.
func newFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	var srvURL string

	mux.HandleFunc("/v1/me", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Figma-Token") != "good-token" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"status":403,"err":"Invalid token"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"u1","email":"designer@example.com","handle":"Designer"}`))
	})

	mux.HandleFunc("/v1/files/KEY1/nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Figma-Token") != "good-token" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, _ = w.Write([]byte(`{
			"name": "My App",
			"nodes": {
				"12:345": {
					"document": {
						"id": "12:345", "name": "Home / Desktop", "type": "FRAME",
						"absoluteBoundingBox": {"width": 1440, "height": 900},
						"children": [
							{"id": "12:346", "name": "Nav", "type": "FRAME", "children": [
								{"id":"12:347","name":"Logo","type":"TEXT","style":{"fontFamily":"Fraunces","fontSize":24,"fontWeight":600}}
							]},
							{"id": "12:350", "name": "Hero CTA", "type": "COMPONENT",
							 "fills": [{"type":"SOLID","color":{"r":0.9,"g":0.31,"b":0.11,"a":1}}]}
						]
					}
				}
			}
		}`))
	})

	mux.HandleFunc("/v1/images/KEY1", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("ids") == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"err":null,"images":{"12:345":"` + srvURL + `/render/12-345.png"}}`))
	})

	mux.HandleFunc("/render/12-345.png", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("\x89PNG fake-bytes"))
	})

	srv := httptest.NewServer(mux)
	srvURL = srv.URL
	t.Cleanup(srv.Close)
	return srv
}

func TestClient_Me_ValidatesToken(t *testing.T) {
	srv := newFixtureServer(t)

	c := NewClient("good-token")
	c.BaseURL = srv.URL
	me, err := c.Me(t.Context())
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.Email != "designer@example.com" {
		t.Errorf("unexpected me: %+v", me)
	}

	bad := NewClient("bad-token")
	bad.BaseURL = srv.URL
	if _, err := bad.Me(t.Context()); err == nil {
		t.Error("invalid token must surface an error")
	}
}

func TestBuildDesignContext_ExtractsStructureStylesAndRender(t *testing.T) {
	srv := newFixtureServer(t)
	c := NewClient("good-token")
	c.BaseURL = srv.URL

	outDir := t.TempDir()
	dc, err := BuildDesignContext(t.Context(), c, []Ref{{FileKey: "KEY1", NodeID: "12:345", RawURL: "https://figma.com/design/KEY1?node-id=12-345"}}, outDir)
	if err != nil {
		t.Fatalf("BuildDesignContext: %v", err)
	}

	md := dc.Markdown
	for _, want := range []string{
		"My App",         // file name
		"Home / Desktop", // frame name
		"1440",           // frame dimensions ground the layout
		"Fraunces",       // typography extracted from text styles
		"#E64F1C",        // fill color converted to hex (0.9,0.31,0.11)
		"Hero CTA",       // component inventory
	} {
		if !strings.Contains(md, want) {
			t.Errorf("design context missing %q:\n%s", want, md)
		}
	}

	if len(dc.Images) != 1 {
		t.Fatalf("want 1 downloaded render, got %d", len(dc.Images))
	}
	data, err := os.ReadFile(filepath.Join(outDir, dc.Images[0]))
	if err != nil || len(data) == 0 {
		t.Errorf("render PNG not written: %v", err)
	}
	if !strings.Contains(md, dc.Images[0]) {
		t.Errorf("markdown must reference the downloaded render %q", dc.Images[0])
	}
}

func TestBuildDesignContext_NoRefsIsNil(t *testing.T) {
	dc, err := BuildDesignContext(t.Context(), NewClient("x"), nil, t.TempDir())
	if err != nil || dc != nil {
		t.Errorf("no refs must be a nil context, got %+v err=%v", dc, err)
	}
}

func TestBuildDesignContext_AllRefsFailedIsAnError(t *testing.T) {
	srv := newFixtureServer(t)
	c := NewClient("good-token")
	c.BaseURL = srv.URL
	// UNKNOWN key: the fixture 404s, so the only ref fails.
	_, err := BuildDesignContext(t.Context(), c, []Ref{{FileKey: "UNKNOWN", NodeID: "1:1", RawURL: "https://figma.com/design/UNKNOWN"}}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "failed to fetch") {
		t.Errorf("all-refs-failed must be a loud error, got %v", err)
	}
}

func TestDownload_RejectsNonFigmaHosts(t *testing.T) {
	c := NewClient("tok") // production BaseURL — only Figma CDN hosts allowed
	cases := []string{
		"http://169.254.169.254/latest/meta-data", // SSRF classic
		"https://evil.example.com/render.png",     // non-Figma host
		"http://www.figma.com/render.png",         // right host, wrong scheme
		"https://notfigma.com/render.png",         // suffix trick
		"https://evilfigma.com/render.png",        // no dot boundary
	}
	for _, u := range cases {
		if _, err := c.Download(t.Context(), u); err == nil {
			t.Errorf("Download must refuse %q", u)
		}
	}
}

func TestParseURLs_HyphenatedFileKey(t *testing.T) {
	refs := ParseURLs("https://www.figma.com/design/Ab-Cd_9/My-App")
	if len(refs) != 1 || refs[0].FileKey != "Ab-Cd_9" {
		t.Errorf("hyphen/underscore keys must parse whole, got %+v", refs)
	}
}

func TestResolveToken_UnreadableFileIsDistinguished(t *testing.T) {
	t.Setenv(TokenEnvVar, "")
	dir := t.TempDir()
	path, err := SaveToken(dir, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	_, _, rerr := ResolveToken(dir)
	if rerr == nil || !strings.Contains(rerr.Error(), "unreadable") {
		t.Errorf("permission failure must be named, got %v", rerr)
	}
}

// TestFigmaBuildContext_FilePermsOwnerOnly pins owner-only permissions on the
// design-context artifacts: the .vxd-design dir is 0o700 and every file in it
// (markdown + PNG renders) is 0o600. Design refs map back to internal design
// files and must not be readable by other users on a shared dispatch host.
// A pre-existing dir with looser perms is repaired.
func TestFigmaBuildContext_FilePermsOwnerOnly(t *testing.T) {
	srv := newFixtureServer(t)
	c := NewClient("good-token")
	c.BaseURL = srv.URL

	outDir := filepath.Join(t.TempDir(), DirName)
	// Pre-create with loose perms — BuildDesignContext must tighten, not trust.
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	dc, err := BuildDesignContext(t.Context(), c, []Ref{{FileKey: "KEY1", NodeID: "12:345", RawURL: "https://figma.com/design/KEY1?node-id=12-345"}}, outDir)
	if err != nil {
		t.Fatalf("BuildDesignContext: %v", err)
	}
	if dc == nil || len(dc.Images) == 0 {
		t.Fatal("fixture should produce a design context with a render")
	}

	info, err := os.Stat(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Errorf("design dir perms = %o, want 700", got)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("expected files under design dir")
	}
	for _, e := range entries {
		fi, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != 0o600 {
			t.Errorf("%s perms = %o, want 600", e.Name(), got)
		}
	}
}
