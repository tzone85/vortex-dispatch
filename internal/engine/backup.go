package engine

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CreateBackup creates a tar.gz archive of the state directory.
// Returns the path to the created archive.
//
// Close errors on the tar/gzip/file writers are surfaced as the function
// return value. Deferred Close in the old form silently swallowed
// gzip.Writer.Close, which is exactly when the FINAL compressed bytes
// are flushed — a failure there produced a truncated .tar.gz reported
// as success.
func CreateBackup(stateDir, outputDir string) (retPath string, retErr error) {
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return "", fmt.Errorf("read state dir: %w", err)
	}

	timestamp := time.Now().Format("2006-01-02-150405")
	project := filepath.Base(stateDir)
	archiveName := fmt.Sprintf("vxd-backup-%s-%s.tar.gz", project, timestamp)
	archivePath := filepath.Join(outputDir, archiveName)

	outFile, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("create archive: %w", err)
	}
	gzWriter := gzip.NewWriter(outFile)
	tarWriter := tar.NewWriter(gzWriter)

	// Close in reverse order — tar finalises trailer, gzip flushes
	// compressed footer, file closes the descriptor. Each Close error
	// short-circuits the rest because a failure at the inner layer makes
	// the outer ones meaningless.
	defer func() {
		if cerr := tarWriter.Close(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("close tar writer: %w", cerr)
		}
		if cerr := gzWriter.Close(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("close gzip writer: %w", cerr)
		}
		if cerr := outFile.Close(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("close archive file: %w", cerr)
		}
		// If we surfaced a Close error, leave the half-written file
		// behind under the original path — callers should not see a
		// "success" path string with a corrupt archive at it.
		if retErr != nil {
			retPath = ""
		}
	}()

	// Walk the state tree so nested directories (logs/, worktrees/,
	// projects/<name>/, artifacts/) are archived too. Prior shallow
	// implementation skipped every entry where IsDir() was true — the
	// archive name "project state" was true only for top-level files;
	// the SQLite WAL, per-project event logs, and artifact dumps were
	// silently omitted. `_ = entries` keeps the early-error ReadDir
	// behaviour (fail fast if the state dir itself is missing) without
	// using the slice further.
	_ = entries
	if err := filepath.WalkDir(stateDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == stateDir {
			return nil // skip the root itself; entries below get added relative
		}
		rel, err := filepath.Rel(stateDir, path)
		if err != nil {
			return err
		}
		return addEntryToTar(tarWriter, path, rel, d)
	}); err != nil {
		return "", fmt.Errorf("walk state dir: %w", err)
	}

	return archivePath, nil
}

// addEntryToTar writes one file or directory entry to tw under `name`.
// Directories are emitted as headers (no body) so consumers preserve
// permissions and ordering. Symlinks emit a header pointing at their
// target rather than dereferencing — a symlink to outside the state
// dir is not in scope for the backup.
func addEntryToTar(tw *tar.Writer, fullPath, name string, d fs.DirEntry) error {
	info, err := d.Info()
	if err != nil {
		return fmt.Errorf("stat %s: %w", fullPath, err)
	}

	linkTarget := ""
	if info.Mode()&os.ModeSymlink != 0 {
		t, err := os.Readlink(fullPath)
		if err != nil {
			return fmt.Errorf("readlink %s: %w", fullPath, err)
		}
		linkTarget = t
	}

	header, err := tar.FileInfoHeader(info, linkTarget)
	if err != nil {
		return fmt.Errorf("tar header %s: %w", fullPath, err)
	}
	header.Name = filepath.ToSlash(name)
	if info.IsDir() {
		header.Name += "/"
	}

	if err := tw.WriteHeader(header); err != nil {
		return fmt.Errorf("write tar header %s: %w", name, err)
	}

	// Only regular files carry a body.
	if !info.Mode().IsRegular() {
		return nil
	}
	f, err := os.Open(fullPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", fullPath, err)
	}
	defer f.Close()
	if _, err := io.Copy(tw, f); err != nil {
		return fmt.Errorf("copy %s: %w", fullPath, err)
	}
	return nil
}
