package figma

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveToken_EnvWins(t *testing.T) {
	dir := t.TempDir()
	if _, err := SaveToken(dir, "file-token"); err != nil {
		t.Fatal(err)
	}
	t.Setenv(TokenEnvVar, "env-token")
	tok, source, err := ResolveToken(dir)
	if err != nil || tok != "env-token" || !strings.Contains(source, TokenEnvVar) {
		t.Errorf("env must win: %q %q %v", tok, source, err)
	}
}

func TestResolveToken_FileFallback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(TokenEnvVar, "")
	if _, err := SaveToken(dir, "  file-token\n"); err != nil {
		t.Fatal(err)
	}
	tok, source, err := ResolveToken(dir)
	if err != nil || tok != "file-token" {
		t.Errorf("file fallback: %q %q %v", tok, source, err)
	}
}

func TestResolveToken_MissingNamesTheInteractiveStep(t *testing.T) {
	t.Setenv(TokenEnvVar, "")
	_, _, err := ResolveToken(t.TempDir())
	if err == nil {
		t.Fatal("missing token must error")
	}
	for _, want := range []string{"vxd figma auth", "INTERACTIVE", TokenEnvVar} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error must guide the operator (%q missing): %v", want, err)
		}
	}
}

func TestSaveToken_OwnerOnlyPerms(t *testing.T) {
	dir := t.TempDir()
	path, err := SaveToken(dir, "tok")
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("token file must be 0600, got %v", st.Mode().Perm())
	}
	if filepath.Base(path) != "figma.token" {
		t.Errorf("unexpected token path %s", path)
	}
	if _, err := SaveToken(dir, "   "); err == nil {
		t.Error("blank token must be rejected")
	}
}
