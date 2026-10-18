package improve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidRepoRelativeGoFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "pkg", "a.go"), []byte("package pkg\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path string
		want bool
	}{
		{"pkg/a.go", true},
		{"", false},
		{"/etc/passwd.go", false},
		{"../x.go", false},
		{"pkg/../../x.go", false},
		{"notes.txt", false},
		{"pkg/missing.go", false},
		{"pkg", false},
	}
	for _, c := range cases {
		if got := validRepoRelativeGoFile(root, c.path); got != c.want {
			t.Errorf("validRepoRelativeGoFile(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
