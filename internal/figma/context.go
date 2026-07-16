package figma

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// DesignContext is the pipeline-facing product of a Figma pull: a markdown
// block for prompts and the rendered reference images written under outDir.
type DesignContext struct {
	Markdown string   // injected into the planner prompt + frontend agent briefs
	Images   []string // file names (relative to outDir) of downloaded PNG renders
}

// ContextFileName is where the markdown lands inside a design dir.
const ContextFileName = "DESIGN_CONTEXT.md"

// DirName is the design-artifact directory created in repo roots and
// worktrees. It is gitignored — design pulls are working material, not
// deliverables.
const DirName = ".vxd-design"

// BuildDesignContext pulls every referenced design and produces the combined
// context. Renders are written to outDir. A nil return (with nil error) means
// there was nothing to pull. Individual ref failures degrade to a note in the
// markdown rather than failing the whole pull — a deleted frame must not
// strand a requirement that also has buildable references.
func BuildDesignContext(ctx context.Context, c *Client, refs []Ref, outDir string) (*DesignContext, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	// Owner-only: the design context carries node IDs + token metadata that
	// map back to internal design files — on a shared dispatch host it must
	// not be readable by other local users. Chmod repairs a pre-existing dir
	// left looser by an older VXD.
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return nil, fmt.Errorf("create design dir: %w", err)
	}
	if err := os.Chmod(outDir, 0o700); err != nil {
		return nil, fmt.Errorf("tighten design dir perms: %w", err)
	}

	var md strings.Builder
	md.WriteString("## DESIGN REFERENCE (pulled from Figma)\n\n")
	md.WriteString("The requirement references specific Figma designs. Build the UI to MATCH these designs — they override generic design choices. Rendered PNGs of the referenced frames are in " + DirName + "/ — OPEN and study them before writing any UI code.\n")

	dc := &DesignContext{}
	fetched := 0
	for _, ref := range refs {
		ids := []string{ref.NodeID}
		if ref.NodeID == "" {
			ids = []string{"0:0"} // document root
		}

		fileName, nodes, err := c.FileNodes(ctx, ref.FileKey, ids)
		if err != nil {
			log.Printf("[figma] fetch %s: %v", ref.RawURL, err)
			fmt.Fprintf(&md, "\n### %s\n(unavailable: fetch failed — verify the link and token access)\n", ref.RawURL)
			continue
		}

		fetched++
		fmt.Fprintf(&md, "\n### File: %s (%s)\n", fileName, ref.RawURL)
		for _, n := range nodes {
			describeNode(&md, n, 0)
		}

		// Render + download the referenced node (best-effort).
		urls, err := c.ImageURLs(ctx, ref.FileKey, ids)
		if err != nil {
			log.Printf("[figma] render %s: %v", ref.RawURL, err)
			continue
		}
		for nodeID, imgURL := range urls {
			if imgURL == "" {
				continue
			}
			data, err := c.Download(ctx, imgURL)
			if err != nil {
				log.Printf("[figma] download render %s: %v", nodeID, err)
				continue
			}
			name := fmt.Sprintf("%s-%s.png", ref.FileKey, strings.ReplaceAll(nodeID, ":", "-"))
			if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
				log.Printf("[figma] write render %s: %v", name, err)
				continue
			}
			dc.Images = append(dc.Images, name)
			fmt.Fprintf(&md, "- Rendered reference: %s/%s\n", DirName, name)
		}
	}

	if fetched == 0 {
		return nil, fmt.Errorf("all %d Figma design reference(s) failed to fetch — verify the links and that the token has access to the file(s)", len(refs))
	}

	dc.Markdown = md.String()

	// Persist the markdown next to the renders so the executor can copy the
	// whole directory into worktrees.
	if err := os.WriteFile(filepath.Join(outDir, ContextFileName), []byte(dc.Markdown), 0o600); err != nil {
		return nil, fmt.Errorf("write design context: %w", err)
	}
	return dc, nil
}

// describeNode renders one node (and two levels of children — matching the
// fetch depth) into the markdown inventory: names, types, dimensions, text
// styles, and solid fill colors.
func describeNode(md *strings.Builder, n Node, depth int) {
	if depth > 2 {
		return
	}
	indent := strings.Repeat("  ", depth)
	line := fmt.Sprintf("%s- [%s] %s", indent, n.Type, n.Name)
	if n.AbsoluteBoundingBox != nil && n.AbsoluteBoundingBox.Width > 0 {
		line += fmt.Sprintf(" (%.0fx%.0f)", n.AbsoluteBoundingBox.Width, n.AbsoluteBoundingBox.Height)
	}
	if n.Style != nil && n.Style.FontFamily != "" {
		line += fmt.Sprintf(" — font: %s %.0f/%.0f", n.Style.FontFamily, n.Style.FontSize, n.Style.FontWeight)
	}
	for _, f := range n.Fills {
		if f.Type == "SOLID" && f.Color != nil {
			line += " — fill: " + hexColor(f.Color.R, f.Color.G, f.Color.B)
			break
		}
	}
	md.WriteString(line + "\n")
	for _, child := range n.Children {
		describeNode(md, child, depth+1)
	}
}

// hexColor converts Figma's 0..1 float RGB to a #RRGGBB hex string.
func hexColor(r, g, b float64) string {
	to := func(v float64) int {
		i := int(v*255 + 0.5)
		if i < 0 {
			return 0
		}
		if i > 255 {
			return 255
		}
		return i
	}
	return fmt.Sprintf("#%02X%02X%02X", to(r), to(g), to(b))
}
