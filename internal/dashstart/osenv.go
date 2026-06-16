package dashstart

import "os"

// osGetenv is a package-level seam so OSEnv.Getenv has a single, easy-to-stub
// indirection point. Tests don't need to touch it (they pass a fake Environ),
// but keeping the real call here also keeps the import surface of OSEnv.Getenv
// to just this file.
func osGetenv(key string) string { return os.Getenv(key) }
