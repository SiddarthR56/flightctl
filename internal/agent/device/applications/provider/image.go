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

	// detectionResult caches the detection result after Verify
	detectionResult *DetectionResult
}

func newImage(
	log *log.PrefixLogger,
	podman *client.Podman,
	spec *v1alpha1.ApplicationProviderSpec,
	readWriter fileio.ReadWriter,
) (*imageProvider, error) {
	provider, err := spec.AsImageApplicationProviderSpec()
	if err != nil {
		return nil, fmt.Errorf("getting provider spec:%w", err)
	}

	reference := provider.Image
	if reference == "" {
		return nil, fmt.Errorf("image reference is required")
	}

	// set the app name to the reference if not provided
	appName := lo.FromPtr(spec.Name)
	if appName == "" {
		appName = reference
	}

	// Get appType from spec if provided
	appType := lo.FromPtr(spec.AppType)

	// Path will be set in Verify() once we know the appType
	var path string
	embedded := false
	if appType != "" {
		path, err = pathFromAppType(appType, appName, embedded)
		if err != nil {
			return nil, fmt.Errorf("getting app path: %w", err)
		}
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

	if p.spec.ImageProvider == nil {
		return fmt.Errorf("image application spec is nil")
	}

	// Get the OCI reference
	reference := p.spec.ImageProvider.Image
	if reference == "" {
		return fmt.Errorf("image reference is required")
	}

	detectionResult, err := p.getDetectionResult(ctx, reference)
	if err != nil {
		return err
	}
	p.detectionResult = detectionResult
	isArtifact := detectionResult.IsArtifact

	// Update spec with detected appType if it wasn't set
	if p.spec.AppType == "" {
		p.spec.AppType = detectionResult.AppType
	}

	// Update the path now that we know the app type
	if p.spec.Path == "" {
		var err error
		p.spec.Path, err = pathFromAppType(detectionResult.AppType, p.spec.Name, p.spec.Embedded)
		if err != nil {
			return fmt.Errorf("getting app path: %w", err)
		}
	}

	// Validate the detection result
	if err := ValidateDetectionResult(detectionResult, p.spec); err != nil {
		return fmt.Errorf("%w: %w", errors.ErrInvalidSpec, err)
	}

	// Ensure artifact support if needed
	if detectionResult.IsArtifact {
		if err := p.podman.EnsureArtifactSupport(ctx); err != nil {
			return err
		}
	}

	p.log.Infof("Detected %s as %s (appType=%s)", reference, detectionResult.OCIType, detectionResult.AppType)

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

	// Get the OCI reference
	reference := p.spec.ImageProvider.Image
	if reference == "" {
		return fmt.Errorf("image reference is required")
	}

	// Determine artifact vs image from cached detection result
	isArtifact := false
	if p.detectionResult != nil {
		isArtifact = p.detectionResult.IsArtifact
	}

	if isArtifact {
		if err := p.podman.EnsureArtifactSupport(ctx); err != nil {
			return err
		}
		p.log.Debugf("Extracting artifact contents: %s", reference)
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

func (p *imageProvider) getDetectionResult(ctx context.Context, reference string) (*DetectionResult, error) {
	if p.AppData != nil && p.AppData.DetectionResult != nil {
		result := p.AppData.DetectionResult
		p.log.Debugf("Using cached detection result for %s: ociType=%s appType=%s",
			reference, result.OCIType, result.AppType)
		return result, nil
	}

	appType, isArtifact, err := p.resolveAppType(ctx, reference)
	if err != nil {
		return nil, fmt.Errorf("detecting reference type for %s: %w", reference, err)
	}

	return &DetectionResult{
		AppType:    appType,
		OCIType:    ociTypeFromArtifact(isArtifact),
		IsArtifact: isArtifact,
	}, nil
}

func (p *imageProvider) resolveAppType(ctx context.Context, reference string) (v1alpha1.AppType, bool, error) {
	specDefinedType := lo.FromPtr(&p.spec.AppType)

	preferArtifact, err := p.preferArtifact(ctx, reference)
	if err != nil {
		return "", false, err
	}

	detectedType, inspectedArtifact, inspectErr := inspectReference(ctx, p.podman, reference, preferArtifact)

	switch {
	case specDefinedType == "":
		if inspectErr != nil {
			return "", inspectedArtifact, fmt.Errorf("getting app type: %w", inspectErr)
		}
		specDefinedType = detectedType
	default:
		if inspectErr == nil && detectedType != "" && detectedType != specDefinedType {
			sourceType := "image label"
			if inspectedArtifact {
				sourceType = "artifact annotation"
			}
			p.log.Warnf("App type mismatch: spec defines %q but %s has %q. Using spec definition.",
				specDefinedType, sourceType, detectedType)
		} else if inspectErr != nil {
			p.log.Warnf("Unable to inspect reference %s: %v. Using spec-defined app type.", reference, inspectErr)
			return specDefinedType, preferArtifact, nil
		}
	}

	return specDefinedType, inspectedArtifact, nil
}

func (p *imageProvider) preferArtifact(ctx context.Context, reference string) (bool, error) {
	switch {
	case p.podman.ArtifactExists(ctx, reference):
		return true, nil
	case p.podman.ImageExists(ctx, reference):
		return false, nil
	default:
		return false, fmt.Errorf("reference %s not available locally", reference)
	}
}

// inspectReference returns the app type and whether the reference is an artifact (true) or image (false).
// When preferArtifact is true, artifact annotations are inspected if image metadata does not provide app type information.
func inspectReference(ctx context.Context, podman *client.Podman, reference string, preferArtifact bool) (appType v1alpha1.AppType, isArtifact bool, err error) {
	var metadata map[string]string

	resp, inspectErr := podman.Inspect(ctx, reference)
	if inspectErr == nil {
		annotations, labels, parseErr := parseReferenceMetadata(resp)
		if parseErr != nil {
			inspectErr = parseErr
		} else if len(annotations) > 0 {
			metadata = annotations
			isArtifact = true
		} else {
			metadata = labels
			isArtifact = false
		}
	}
	if inspectErr != nil && !preferArtifact {
		return "", false, inspectErr
	}

	if preferArtifact && (len(metadata) == 0 || !isArtifact) {
		annotations, err := podman.InspectArtifactAnnotations(ctx, reference)
		if err != nil {
			return "", true, err
		}
		metadata = annotations
		isArtifact = true
	}

	if len(metadata) == 0 {
		return "", isArtifact, fmt.Errorf("%w: %s, %s", errors.ErrAppLabel, AppTypeLabel, reference)
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

func parseReferenceMetadata(resp string) (map[string]string, map[string]string, error) {
	var inspectData []struct {
		Annotations map[string]string `json:"Annotations"`
		Config      struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}

	if err := json.Unmarshal([]byte(resp), &inspectData); err != nil {
		return nil, nil, fmt.Errorf("parse inspect response: %w", err)
	}

	if len(inspectData) == 0 {
		return nil, nil, fmt.Errorf("no inspect data found")
	}

	return inspectData[0].Annotations, inspectData[0].Config.Labels, nil
}

// typeFromImage inspects the local OCI reference to determine the application type.
func typeFromImage(ctx context.Context, podman *client.Podman, reference string) (v1alpha1.AppType, error) {
	appType, _, err := inspectReference(ctx, podman, reference, false)
	return appType, err
}
