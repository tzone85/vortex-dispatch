package engine

import (
	"path/filepath"
	"testing"
)

func TestProjectDir_BasicPath(t *testing.T) {
	got := ProjectDir("/home/user/.vxd", "my-project")
	want := filepath.Join("/home/user/.vxd", "projects", "my-project")
	if got != want {
		t.Errorf("ProjectDir() = %q, want %q", got, want)
	}
}

func TestProjectDir_TrailingSlash(t *testing.T) {
	got := ProjectDir("/home/user/.vxd/", "test-proj")
	want := filepath.Join("/home/user/.vxd/", "projects", "test-proj")
	if got != want {
		t.Errorf("ProjectDir() = %q, want %q", got, want)
	}
}

func TestProjectDir_EmptyProjectName(t *testing.T) {
	got := ProjectDir("/home/user/.vxd", "")
	want := filepath.Join("/home/user/.vxd", "projects", "")
	if got != want {
		t.Errorf("ProjectDir() = %q, want %q", got, want)
	}
}

func TestProjectDir_NestedBase(t *testing.T) {
	got := ProjectDir("/a/b/c", "proj")
	want := filepath.Join("/a/b/c", "projects", "proj")
	if got != want {
		t.Errorf("ProjectDir() = %q, want %q", got, want)
	}
}
