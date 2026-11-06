package test

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Inspect every historical path, including deleted files and rename aliases.
// Checking just rev-list --objects names can miss a blob used at multiple paths.
func checkPublicHistory(root string, refs []string) error {
	git := func(args ...string) ([]byte, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		return cmd.Output()
	}
	shallow, err := git("rev-parse", "--is-shallow-repository")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(shallow)) != "false" {
		return fmt.Errorf("public history requires a full clone (git fetch --unshallow)")
	}
	// Resolve user-supplied refs first so they cannot become log options.
	args := []string{"log", "--raw", "--no-abbrev", "--no-renames", "--format=", "-z", "--full-history", "--root", "-m"}
	for _, ref := range refs {
		oid, err := git("rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
		if err != nil {
			return fmt.Errorf("resolve history ref %q: %w", ref, err)
		}
		args = append(args, strings.TrimSpace(string(oid)))
	}
	args = append(args, "--")
	raw, err := git(args...)
	if err != nil {
		return fmt.Errorf("enumerate public history: %w", err)
	}
	parts := bytes.Split(raw, []byte{0})
	blobs := make(map[string][]string)
	for i := 0; i < len(parts); i++ {
		header := strings.TrimSpace(string(parts[i]))
		if header == "" {
			continue
		}
		fields := strings.Fields(header)
		if len(fields) != 5 || !strings.HasPrefix(fields[0], ":") || i+1 >= len(parts) {
			return fmt.Errorf("invalid historical file record")
		}
		i++
		path := string(parts[i])
		if reason := publicBoundaryViolation(path, ""); reason != "" {
			return fmt.Errorf("history %s: %s", path, reason)
		}
		if fields[4] == "D" || fields[1] == "160000" || !strings.HasSuffix(strings.ToLower(path), ".md") {
			continue
		}
		blobs[fields[3]] = append(blobs[fields[3]], path)
	}
	var input strings.Builder
	for oid := range blobs {
		fmt.Fprintln(&input, oid)
	}
	cmd := exec.Command("git", "cat-file", "--batch")
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(input.String())
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("read historical blobs: %w", err)
	}
	reader := bufio.NewReader(bytes.NewReader(output))
	for range blobs {
		header, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[1] != "blob" {
			return fmt.Errorf("invalid blob record")
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 || size >= len(output) {
			return fmt.Errorf("invalid blob size")
		}
		content := make([]byte, size+1)
		if _, err := io.ReadFull(reader, content); err != nil {
			return err
		}
		for _, path := range blobs[fields[0]] {
			if reason := publicBoundaryViolation(path, string(content[:size])); reason != "" {
				return fmt.Errorf("history %s (%s): %s", path, fields[0], reason)
			}
		}
	}
	return nil
}

func TestPublicHistory(t *testing.T) {
	if os.Getenv("VXD_CHECK_PUBLIC_HISTORY") != "1" {
		t.Skip("run make public-history for the full-history publication gate")
	}
	refs := strings.Fields(os.Getenv("VXD_PUBLIC_HISTORY_REFS"))
	if len(refs) == 0 {
		refs = []string{"HEAD"}
	}
	if err := checkPublicHistory(repoRoot(t), refs); err != nil {
		t.Fatal(err)
	}
}

func TestPublicHistoryRejectsDeletedMaterial(t *testing.T) {
	for _, path := range []string{"docs/opportunities/removed.txt", "docs/removed.md"} {
		t.Run(path, func(t *testing.T) {
			root := t.TempDir()
			git := func(args ...string) {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = root
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			git("init", "-b", "main")
			git("config", "user.email", "test@example.com")
			git("config", "user.name", "Test")
			git("commit", "--allow-empty", "-m", "clean")
			git("tag", "clean")
			if err := checkPublicHistory(root, []string{"HEAD"}); err != nil {
				t.Fatal(err)
			}
			file := filepath.Join(root, path)
			if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(file, []byte("## Revenue Projections\nPrivate forecast\n"), 0644); err != nil {
				t.Fatal(err)
			}
			git("add", ".")
			git("commit", "-m", "add")
			// The same blob can appear under an allowed and a forbidden name.
			// Preserve an alias to ensure the checker examines every path.
			if path == "docs/removed.md" {
				git("mv", path, "docs/alias.txt")
				git("commit", "-m", "rename")
				git("mv", "docs/alias.txt", path)
				git("commit", "-m", "restore name")
			}
			git("rm", path)
			git("commit", "-m", "remove")
			if err := checkPublicHistory(root, []string{"HEAD"}); err == nil {
				t.Fatal("deleted publication violation was accepted")
			}
			if err := checkPublicHistory(root, []string{"clean"}); err != nil {
				t.Fatalf("explicit clean ref rejected: %v", err)
			}
			shallow := filepath.Join(t.TempDir(), "shallow")
			git("clone", "--quiet", "--depth=1", "file://"+root, shallow)
			if err := checkPublicHistory(shallow, []string{"HEAD"}); err == nil || !strings.Contains(err.Error(), "full clone") {
				t.Fatalf("shallow history must fail closed: %v", err)
			}
		})
	}
}
