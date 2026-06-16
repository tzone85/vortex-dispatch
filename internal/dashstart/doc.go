// Package dashstart provides the orchestrator that lets `vxd req` reuse or
// launch the always-on web dashboard daemon. The package is intentionally
// small and side-effect free at the seams so unit tests can substitute the
// HTTP probe, the env vars, and the spawner without forking real processes.
package dashstart
