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
func CreateBackup(stateDir, outputDir string) (string, error) {
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
	defer outFile.Close()

	gzWriter := gzip.NewWriter(outFile)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

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
