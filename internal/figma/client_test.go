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
