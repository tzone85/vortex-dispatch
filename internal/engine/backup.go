package engine

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
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

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		filePath := filepath.Join(stateDir, entry.Name())
		if err := addFileToTar(tarWriter, filePath, entry.Name()); err != nil {
			return "", fmt.Errorf("add %s to archive: %w", entry.Name(), err)
		}
	}

	return archivePath, nil
}

func addFileToTar(tw *tar.Writer, filePath, name string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	header, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	header.Name = name

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, f)
	return err
}
