package figma

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TokenEnvVar is the environment variable checked first for a Figma
// personal access token.
const TokenEnvVar = "FIGMA_TOKEN"

// tokenFileName under the vxd state dir (mode 0o600).
const tokenFileName = "figma.token"

// TokenPath returns where `vxd figma auth` persists the token.
func TokenPath(stateDir string) string {
	return filepath.Join(stateDir, tokenFileName)
}

// ResolveToken finds a Figma token: the FIGMA_TOKEN env var wins, then the
// token file under stateDir. Returns the token and a human-readable source
// label, or an error naming the interactive step that fixes it.
func ResolveToken(stateDir string) (token, source string, err error) {
	if t := strings.TrimSpace(os.Getenv(TokenEnvVar)); t != "" {
		return t, "env " + TokenEnvVar, nil
	}
	path := TokenPath(stateDir)
	data, readErr := os.ReadFile(path)
	if readErr == nil {
		if t := strings.TrimSpace(string(data)); t != "" {
			return t, path, nil
		}
	}
	return "", "", fmt.Errorf(
		"no Figma credential found: the requirement references a Figma design, which needs a one-time INTERACTIVE auth session (unlike vxd's usual fire-and-forget runs).\n"+
			"Run `vxd figma auth` once (you'll create a personal access token in the browser and paste it here), or export %s.\n"+
			"After that single session, Figma-referencing runs are autonomous again", TokenEnvVar)
}

// SaveToken persists the token at TokenPath with owner-only permissions,
// tightening the directory too (it may hold other credentials).
func SaveToken(stateDir, token string) (string, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return "", fmt.Errorf("empty token")
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", fmt.Errorf("create state dir: %w", err)
	}
	path := TokenPath(stateDir)
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write token: %w", err)
	}
	return path, nil
}
