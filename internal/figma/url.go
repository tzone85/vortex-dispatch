// Package figma integrates Figma designs into the vxd pipeline: requirement
// text can reference figma.com design URLs, and vxd pulls the referenced
// frames (structure, styles, rendered PNGs) into a design context that the
// planner and the frontend agents consume.
//
// Auth model: Figma's API needs an operator credential, which makes any
// Figma-referencing run interactive-once rather than fire-and-forget — the
// operator runs `vxd figma auth` a single time (personal access token, an
// interactive session), after which runs are autonomous again. `vxd req`
// fails fast with that guidance when a design URL is present but no
// credential is configured, before any LLM spend.
package figma

import (
	"net/url"
	"regexp"
	"strings"
)

// Ref identifies one referenced Figma design: the file key and (optionally)
// a specific node within it.
type Ref struct {
	FileKey string // e.g. "AbC123dEf456"
	NodeID  string // canonical "12:345" form; empty = whole file
	RawURL  string // the URL as written in the requirement
}

// figmaURLRe matches figma.com design/file/proto/board URLs. The file key is
// the path segment after the kind.
var figmaURLRe = regexp.MustCompile(`https://(?:www\.)?figma\.com/(?:design|file|proto|board)/([A-Za-z0-9_-]+)[^\s)>\]]*`)

// ParseURLs extracts every Figma design reference from free text, deduplicated
// (same file key + node), preserving first-seen order.
func ParseURLs(text string) []Ref {
	matches := figmaURLRe.FindAllString(text, -1)
	seen := map[string]bool{}
	refs := make([]Ref, 0, len(matches))
	for _, raw := range matches {
		sub := figmaURLRe.FindStringSubmatch(raw)
		if len(sub) < 2 {
			continue
		}
		ref := Ref{FileKey: sub[1], NodeID: nodeIDFromURL(raw), RawURL: raw}
		key := ref.FileKey + "|" + ref.NodeID
		if seen[key] {
			continue
		}
		seen[key] = true
		refs = append(refs, ref)
	}
	return refs
}

// nodeIDFromURL pulls the node-id query parameter and canonicalises it to the
// API's "12:345" form (URLs carry "12-345" or URL-encoded "12%3A345").
func nodeIDFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	id := u.Query().Get("node-id")
	if id == "" {
		return ""
	}
	return strings.ReplaceAll(id, "-", ":")
}
