package dashstart

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser launches the user's default browser at url. Failures are
// returned to the caller; the dashstart orchestrator chooses to swallow
// them (the URL has already been printed to stdout) rather than abort
// the `vxd req` run.
//
// The function is a tiny wrapper that mirrors web.openBrowser. Duplicating
// the four lines avoids dragging an import cycle on internal/web.
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	default:
		return fmt.Errorf("dashstart: no browser opener for GOOS=%s", runtime.GOOS)
	}
	return cmd.Start()
}
