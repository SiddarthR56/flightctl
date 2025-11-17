package provider

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/flightctl/flightctl/internal/agent/client"
	"github.com/flightctl/flightctl/internal/agent/device/fileio"
)

// extractReferenceContents copies container image contents or extracts OCI artifact contents to destination.
func extractReferenceContents(ctx context.Context, podman *client.Podman, readWriter fileio.ReadWriter, reference, destination string, isArtifact bool) error {
	if isArtifact {
		if err := podman.EnsureArtifactSupport(ctx); err != nil {
			return err
		}
		if err := prepareArtifactDestination(readWriter, destination); err != nil {
			return fmt.Errorf("prepare artifact destination: %w", err)
		}
		if _, err := podman.ExtractArtifact(ctx, reference, destination); err != nil {
			return fmt.Errorf("extract artifact contents: %w", err)
		}
		if err := expandArtifactArchives(readWriter, destination); err != nil {
			return fmt.Errorf("expand artifact archives: %w", err)
		}
		return nil
	}

	if err := podman.CopyContainerData(ctx, reference, destination); err != nil {
		return fmt.Errorf("copy image contents: %w", err)
	}
	return nil
}

// prepareArtifactDestination ensures the destination path exists as an empty directory.
// Podman will treat the destination as a file path when extracting single-file artifacts,
// so we remove any existing file and recreate the directory to keep the installer logic consistent.
func prepareArtifactDestination(readWriter fileio.ReadWriter, destination string) error {
	fullPath := readWriter.PathFor(destination)
	info, err := os.Stat(fullPath)
	switch {
	case err == nil:
		if info.IsDir() {
			if err := readWriter.RemoveContents(destination); err != nil {
				return fmt.Errorf("clearing artifact destination %q: %w", destination, err)
			}
			return nil
		}

		// Path exists but is a file - remove it so we can recreate a directory.
		if err := readWriter.RemoveFile(destination); err != nil {
			return fmt.Errorf("removing artifact file %q: %w", destination, err)
		}
	case os.IsNotExist(err):
		// fall through to create directory
	default:
		return fmt.Errorf("stat artifact destination %q: %w", destination, err)
	}

	if err := readWriter.MkdirAll(destination, fileio.DefaultDirectoryPermissions); err != nil {
		return fmt.Errorf("creating artifact destination %q: %w", destination, err)
	}
	return nil
}

func expandArtifactArchives(readWriter fileio.ReadWriter, destination string) error {
	entries, err := readWriter.ReadDir(destination)
	if err != nil {
		return fmt.Errorf("reading artifact directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !isTarArchive(name) {
			continue
		}

		archivePath := filepath.Join(destination, name)
		if err := extractArchiveFile(readWriter, archivePath, destination); err != nil {
			return fmt.Errorf("extracting archive %s: %w", name, err)
		}
		if err := readWriter.RemoveFile(archivePath); err != nil {
			return fmt.Errorf("removing archive %s: %w", name, err)
		}
	}

	return nil
}

func isTarArchive(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".tar") ||
		strings.HasSuffix(lower, ".tar.gz") ||
		strings.HasSuffix(lower, ".tgz")
}

func extractArchiveFile(readWriter fileio.ReadWriter, archivePath, destination string) error {
	fullArchivePath := readWriter.PathFor(archivePath)
	file, err := os.Open(fullArchivePath)
	if err != nil {
		return fmt.Errorf("opening archive %s: %w", archivePath, err)
	}
	defer file.Close()

	var reader io.Reader = file
	if strings.HasSuffix(strings.ToLower(archivePath), ".gz") || strings.HasSuffix(strings.ToLower(archivePath), ".tgz") {
		gzr, err := gzip.NewReader(file)
		if err != nil {
			return fmt.Errorf("creating gzip reader for %s: %w", archivePath, err)
		}
		defer gzr.Close()
		reader = gzr
	}

	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading archive %s: %w", archivePath, err)
		}

		cleanName, err := cleanArchiveEntryPath(header.Name)
		if err != nil {
			return fmt.Errorf("invalid archive entry %q: %w", header.Name, err)
		}

		targetPath := filepath.Join(destination, cleanName)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := readWriter.MkdirAll(targetPath, fileio.DefaultDirectoryPermissions); err != nil {
				return fmt.Errorf("creating directory %s: %w", targetPath, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := readWriter.MkdirAll(filepath.Dir(targetPath), fileio.DefaultDirectoryPermissions); err != nil {
				return fmt.Errorf("creating parent directory for %s: %w", targetPath, err)
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				return fmt.Errorf("reading file %s from archive: %w", header.Name, err)
			}
			perm := fileio.DefaultFilePermissions
			if header.FileInfo() != nil {
				perm = header.FileInfo().Mode()
			}
			if err := readWriter.WriteFile(targetPath, data, perm); err != nil {
				return fmt.Errorf("writing file %s: %w", targetPath, err)
			}
		default:
			return fmt.Errorf("unsupported archive entry type %d for %s", header.Typeflag, header.Name)
		}
	}

	return nil
}

func cleanArchiveEntryPath(name string) (string, error) {
	clean := filepath.Clean(name)
	if clean == "." {
		return "", fmt.Errorf("empty entry path")
	}
	if strings.HasPrefix(clean, "..") || strings.Contains(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path traversal detected: %s", name)
	}
	if filepath.IsAbs(clean) {
		return "", fmt.Errorf("absolute paths are not allowed: %s", name)
	}
	return clean, nil
}
