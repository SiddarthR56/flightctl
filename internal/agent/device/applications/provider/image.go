package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/agent/client"
	"github.com/flightctl/flightctl/internal/agent/device/errors"
	"github.com/flightctl/flightctl/internal/agent/device/fileio"
	"github.com/flightctl/flightctl/pkg/log"
	"github.com/samber/lo"
)

const (
	AppTypeLabel            = "appType"
	DefaultImageManifestDir = "/"
)

type imageProvider struct {
	podman     *client.Podman
	readWriter fileio.ReadWriter
	log        *log.PrefixLogger
	spec       *ApplicationSpec

	// AppData stores the extracted app data from OCITargets to reuse in Verify
	AppData *AppData
}

func newImage(log *log.PrefixLogger, podman *client.Podman, spec *v1alpha1.ApplicationProviderSpec, readWriter fileio.ReadWriter, appType v1alpha1.AppType) (*imageProvider, error) {
	provider, err := spec.AsImageApplicationProviderSpec()
	if err != nil {
		return nil, fmt.Errorf("getting provider spec:%w", err)
	}

	// Get the OCI reference (image or artifact)
	reference, err := provider.GetReference()
	if err != nil {
		return nil, fmt.Errorf("getting OCI reference: %w", err)
	}

	// set the app name to the reference if not provided
	appName := lo.FromPtr(spec.Name)
	if appName == "" {
		appName = reference
	}
	embedded := false
	path, err := pathFromAppType(appType, appName, embedded)
	if err != nil {
		return nil, fmt.Errorf("getting app path: %w", err)
	}

	volumeManager, err := NewVolumeManager(log, appName, provider.Volumes)
	if err != nil {
		return nil, err
	}

	return &imageProvider{
		log:        log,
		podman:     podman,
		readWriter: readWriter,
		spec: &ApplicationSpec{
			Name:          appName,
			ID:            client.NewComposeID(appName),
			AppType:       appType,
			Path:          path,
			EnvVars:       lo.FromPtr(spec.EnvVars),
			Embedded:      embedded,
			ImageProvider: &provider,
			Volume:        volumeManager,
		},
	}, nil
}

func (p *imageProvider) Verify(ctx context.Context) error {
	if err := validateEnvVars(p.spec.EnvVars); err != nil {
		return fmt.Errorf("%w: validating env vars: %w", errors.ErrInvalidSpec, err)
	}

	// Get the OCI reference (image or artifact)
	reference, err := p.spec.ImageProvider.GetReference()
	if err != nil {
		return fmt.Errorf("getting OCI reference: %w", err)
	}

	// Determine if this is an artifact based on the field used
	// With validation: artifact field → quadlet, image field → compose
	isArtifactField := p.spec.ImageProvider.IsArtifact()

	// Get app type from spec or auto-detect from labels/annotations
	specDefinedType := lo.FromPtr(&p.spec.AppType)
	var detectedType v1alpha1.AppType
	var isArtifact bool

	// Always inspect to validate metadata where possible.
	detectedType, inspectedArtifact, inspectErr := inspectReference(ctx, p.podman, reference)
	isArtifact = inspectedArtifact

	// Validate that the field used matches the actual type
	if inspectErr == nil {
		if isArtifactField && !inspectedArtifact {
			p.log.Warnf("Using 'artifact' field but reference appears to be a container image (no annotations found)")
		} else if !isArtifactField && inspectedArtifact {
			p.log.Warnf("Using 'image' field but reference appears to be an OCI artifact (has annotations)")
		}
	}

	if specDefinedType == "" {
		// Auto-detect type from artifact annotation or image label
		if inspectErr != nil {
			return fmt.Errorf("getting app type: %w", inspectErr)
		}
		p.spec.AppType = detectedType
	} else {
		// Spec has app type defined - validate it matches artifact/image metadata when available
		if inspectErr == nil && detectedType != "" && detectedType != specDefinedType {
			sourceType := "image label"
			if inspectedArtifact {
				sourceType = "artifact annotation"
			}
			p.log.Warnf("App type mismatch: spec defines %q but %s has %q. Using spec definition.",
				specDefinedType, sourceType, detectedType)
		}
	}

	if isArtifactField {
		// Field intent overrides inspection for artifact detection (annotations may be missing)
		isArtifact = true
	} else {
		isArtifact = inspectedArtifact
	}

	// Validate supported app types
	if p.spec.AppType != v1alpha1.AppTypeCompose && p.spec.AppType != v1alpha1.AppTypeQuadlet {
		return fmt.Errorf("%w: %s", errors.ErrUnsupportedAppType, p.spec.AppType)
	}

	if err := ensureDependenciesFromAppType(p.spec.AppType); err != nil {
		return fmt.Errorf("%w: ensuring dependencies: %w", errors.ErrNoRetry, err)
	}

	if err := ensureDependenciesFromVolumes(ctx, p.podman, p.spec.ImageProvider.Volumes); err != nil {
		return fmt.Errorf("%w: ensuring volume dependencies: %w", errors.ErrNoRetry, err)
	}

	var tmpAppPath string
	var shouldCleanup bool

	if p.AppData != nil {
		tmpAppPath = p.AppData.TmpPath
		shouldCleanup = false
	} else {
		// no cache, extract the image or artifact contents
		var err error
		tmpAppPath, err = p.readWriter.MkdirTemp("app_temp")
		if err != nil {
			return fmt.Errorf("creating tmp dir: %w", err)
		}
		shouldCleanup = true

		// copy or extract contents to a tmp directory for further processing
		if err := extractReferenceContents(ctx, p.podman, p.readWriter, reference, tmpAppPath, isArtifact); err != nil {
			if rmErr := p.readWriter.RemoveAll(tmpAppPath); rmErr != nil {
				p.log.Warnf("Failed to cleanup temporary directory %q: %v", tmpAppPath, rmErr)
			}
			return err
		}
	}

	defer func() {
		if shouldCleanup && tmpAppPath != "" {
			if err := p.readWriter.RemoveAll(tmpAppPath); err != nil {
				p.log.Warnf("Failed to cleanup temporary directory %q: %v", tmpAppPath, err)
			}
			p.AppData = nil
		}
	}()

	switch p.spec.AppType {
	case v1alpha1.AppTypeCompose:
		// ensure the compose application content in tmp dir is valid
		if err := ensureCompose(p.readWriter, tmpAppPath); err != nil {
			return fmt.Errorf("ensuring compose: %w", err)
		}
	case v1alpha1.AppTypeQuadlet:
		if err := ensureQuadlet(p.readWriter, tmpAppPath); err != nil {
			return fmt.Errorf("ensuring quadlet: %w", err)
		}
	default:
		return fmt.Errorf("%w: %s", errors.ErrUnsupportedAppType, p.spec.AppType)
	}

	return nil
}

func (p *imageProvider) Install(ctx context.Context) error {
	// cleanup any cached extracted path from OCITargets since Install will extract to final location
	if p.AppData != nil {
		p.log.Debugf("Cleaning up cached app data before Install")
		if cleanupErr := p.AppData.Cleanup(); cleanupErr != nil {
			p.log.Warnf("Failed to cleanup cached app data: %v", cleanupErr)
		}
		p.AppData = nil
	}

	if p.spec.ImageProvider == nil {
		return fmt.Errorf("image application spec is nil")
	}

	// Get the OCI reference (image or artifact)
	reference, err := p.spec.ImageProvider.GetReference()
	if err != nil {
		return fmt.Errorf("getting OCI reference: %w", err)
	}

	// Determine artifact vs image based on the field used
	isArtifact := p.spec.ImageProvider.IsArtifact()

	if isArtifact {
		p.log.Infof("Extracting artifact contents: %s", reference)
	} else {
		p.log.Debugf("Copying image contents: %s", reference)
	}

	if err := extractReferenceContents(ctx, p.podman, p.readWriter, reference, p.spec.Path, isArtifact); err != nil {
		return err
	}

	if err := writeENVFile(p.spec.Path, p.readWriter, p.spec.EnvVars); err != nil {
		return fmt.Errorf("writing env file: %w", err)
	}

	switch p.spec.AppType {
	case v1alpha1.AppTypeCompose:
		if err := writeComposeOverride(p.log, p.spec.Path, p.spec.Volume, p.readWriter, client.ComposeOverrideFilename); err != nil {
			return fmt.Errorf("writing override file %w", err)
		}
	case v1alpha1.AppTypeQuadlet:
		if err := installQuadlet(p.readWriter, p.spec.Path, p.spec.ID); err != nil {
			return fmt.Errorf("installing quadlet: %w", err)
		}
	default:
		return fmt.Errorf("%w: %s", errors.ErrUnsupportedAppType, p.spec.AppType)
	}

	return nil
}

func (p *imageProvider) Remove(ctx context.Context) error {
	// cleanup any cached extracted path
	if p.AppData != nil {
		p.log.Debugf("Cleaning up cached app data before Remove")
		if cleanupErr := p.AppData.Cleanup(); cleanupErr != nil {
			p.log.Warnf("Failed to cleanup cached app data: %v", cleanupErr)
		}
		p.AppData = nil
	}

	if err := p.readWriter.RemoveAll(p.spec.Path); err != nil {
		return fmt.Errorf("removing application: %w", err)
	}
	return nil
}

func (p *imageProvider) Name() string {
	return p.spec.Name
}

func (p *imageProvider) Spec() *ApplicationSpec {
	return p.spec
}

// inspectReference returns app type and whether the reference is an artifact (true) or image (false).
// It inspects the reference once and checks for annotations (artifact) or labels (image).
func inspectReference(ctx context.Context, podman *client.Podman, reference string) (appType v1alpha1.AppType, isArtifact bool, err error) {
	inspectJSON, err := podman.Inspect(ctx, reference)
	if err != nil {
		return "", false, err
	}

	var inspectData []struct {
		Annotations map[string]string `json:"Annotations"`
		Config      struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.Unmarshal([]byte(inspectJSON), &inspectData); err != nil {
		return "", false, fmt.Errorf("parse inspect response: %w", err)
	}

	if len(inspectData) == 0 {
		return "", false, fmt.Errorf("no inspect data found")
	}

	// Check for annotations first (OCI artifacts), then fall back to labels (images)
	var metadata map[string]string
	if len(inspectData[0].Annotations) > 0 {
		metadata = inspectData[0].Annotations
		isArtifact = true
	} else if len(inspectData[0].Config.Labels) > 0 {
		metadata = inspectData[0].Config.Labels
		isArtifact = false
	} else {
		metadata = make(map[string]string)
		isArtifact = false
	}

	appType, err = extractAppType(metadata, AppTypeLabel, reference)
	return appType, isArtifact, err
}

// extractAppType extracts and validates appType from a metadata map (labels or annotations)
func extractAppType(metadata map[string]string, key, reference string) (v1alpha1.AppType, error) {
	appTypeValue, ok := metadata[key]
	if !ok {
		return "", fmt.Errorf("%w: %s, %s", errors.ErrAppLabel, key, reference)
	}
	appType := v1alpha1.AppType(appTypeValue)
	if appType == "" {
		return "", fmt.Errorf("%w: %s", errors.ErrParseAppType, appTypeValue)
	}
	return appType, nil
}

// typeFromImage inspects the local OCI reference to determine the application type.
func typeFromImage(ctx context.Context, podman *client.Podman, reference string) (v1alpha1.AppType, error) {
	appType, _, err := inspectReference(ctx, podman, reference)
	return appType, err
}
