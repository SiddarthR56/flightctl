package provider

import (
	"context"
	"fmt"

	"github.com/flightctl/flightctl/internal/agent/client"
	"github.com/flightctl/flightctl/internal/agent/device/errors"
	"github.com/flightctl/flightctl/internal/agent/device/fileio"
)

// extractReferenceContents copies container image contents or extracts OCI artifact contents to destination.
func extractReferenceContents(ctx context.Context, podman *client.Podman, readWriter fileio.ReadWriter, reference, destination string, isArtifact bool) error {
	if isArtifact {
		if err := ensureArtifactExtractionSupport(ctx, podman); err != nil {
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

// ensureArtifactExtractionSupport verifies the local podman version can extract OCI artifacts.
func ensureArtifactExtractionSupport(ctx context.Context, podman *client.Podman) error {
	version, err := podman.Version(ctx)
	if err != nil {
		return fmt.Errorf("%w: checking podman version: %w", errors.ErrNoRetry, err)
	}
	if !version.GreaterOrEqual(5, 5) {
		return fmt.Errorf("%w: OCI artifact extraction requires podman >= 5.5, found %d.%d", errors.ErrNoRetry, version.Major, version.Minor)
	}
	return nil
}
