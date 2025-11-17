package provider

import (
	"context"
	"fmt"
	"os"

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
