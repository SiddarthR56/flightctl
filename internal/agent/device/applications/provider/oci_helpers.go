package provider

import (
	"context"
	"fmt"

	"github.com/flightctl/flightctl/internal/agent/client"
	"github.com/flightctl/flightctl/internal/agent/device/fileio"
)

// extractReferenceContents copies container image contents or extracts OCI artifact contents to destination.
func extractReferenceContents(ctx context.Context, podman *client.Podman, readWriter fileio.ReadWriter, reference, destination string, isArtifact bool) error {
	if isArtifact {
		if err := podman.EnsureArtifactSupport(ctx); err != nil {
			return err
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
