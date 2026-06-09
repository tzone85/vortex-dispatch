//go:build windows

package docker

// defaultDockerHost returns the platform default when DOCKER_HOST is unset.
//
// We deliberately return the named-pipe URL even though Go's stdlib HTTP
// transport cannot dial it without an extra dependency (github.com/Microsoft/
// go-winio). This is a fail-closed choice: an unconfigured host will get a
// clear dial error, NOT a silent connection to a plaintext daemon.
//
// Returning the obvious tcp://localhost:2375 fallback would silently target a
// Docker Desktop daemon that — when the "Expose daemon on tcp://localhost:2375
// without TLS" option is enabled — runs without authentication or transport
// encryption and grants root-equivalent control of the host to anything that
// can reach the port. Any other local user or process could pivot through it.
// We refuse to make that a built-in default.
//
// Operators on Windows must set DOCKER_HOST explicitly, for example:
//   - tcp://localhost:2375 (only if you have already accepted the plaintext risk)
//   - tcp://localhost:2376 with TLS env vars set (DOCKER_TLS_VERIFY, DOCKER_CERT_PATH)
//   - npipe:////./pipe/docker_engine (works once npipe dialing is wired up;
//     today this default makes the failure mode loud and obvious instead of silent).
func defaultDockerHost() string {
	return "npipe:////./pipe/docker_engine"
}
