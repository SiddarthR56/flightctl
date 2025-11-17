package provider

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/internal/agent/client"
	"github.com/flightctl/flightctl/internal/agent/device/errors"
	"github.com/flightctl/flightctl/internal/agent/device/fileio"
	"github.com/flightctl/flightctl/internal/util/validation"
	"github.com/flightctl/flightctl/pkg/executer"
	"github.com/flightctl/flightctl/pkg/log"
	"github.com/flightctl/flightctl/test/util"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestImageProvider(t *testing.T) {
	require := require.New(t)
	appImage := "quay.io/flightctl-tests/alpine:v1"
	tests := []struct {
		name          string
		image         string
		spec          *v1alpha1.ApplicationProviderSpec
		composeSpec   string
		labels        map[string]string
		setupMocks    func(*executer.MockExecuter, string)
		wantVerifyErr error
	}{
		{
			name:   "missing appType label",
			image:  appImage,
			labels: map[string]string{},
			spec: &v1alpha1.ApplicationProviderSpec{
				Name: lo.ToPtr("app"),
			},
			composeSpec: util.NewComposeSpec(),
			setupMocks: func(mockExec *executer.MockExecuter, appLabels string) {
				mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"inspect", appImage}).Return(appLabels, "", 0)
			},
			wantVerifyErr: errors.ErrAppLabel,
		},
		{
			name:  "appType label set to invalid value",
			image: appImage,
			labels: map[string]string{
				AppTypeLabel: "invalid",
			},
			spec: &v1alpha1.ApplicationProviderSpec{
				Name: lo.ToPtr("app"),
			},
			composeSpec: util.NewComposeSpec(),
			setupMocks: func(mockExec *executer.MockExecuter, appLabels string) {
				mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"inspect", appImage}).Return(appLabels, "", 0)
			},
			wantVerifyErr: errors.ErrUnsupportedAppType,
		},
		{
			name:  "appType compose with valid env",
			image: appImage,
			labels: map[string]string{
				AppTypeLabel: string(v1alpha1.AppTypeCompose),
			},
			spec: &v1alpha1.ApplicationProviderSpec{
				Name: lo.ToPtr("app"),
				EnvVars: lo.ToPtr(map[string]string{
					"FOO": "bar",
				}),
			},
			composeSpec: util.NewComposeSpec(),
			setupMocks: func(mockExec *executer.MockExecuter, appLabels string) {
				gomock.InOrder(
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"inspect", appImage}).Return(appLabels, "", 0),
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"inspect", appImage}).Return(appLabels, "", 0),
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"unshare", "podman", "image", "mount", appImage}).Return("/mount", "", 0),
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"image", "unmount", appImage}).Return("", "", 0),
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"unshare", "podman", "image", "mount", appImage}).Return("/mount", "", 0),
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"image", "unmount", appImage}).Return("", "", 0),
				)
			},
		},
		{
			name:  "appType compose with invalid env",
			image: appImage,
			labels: map[string]string{
				AppTypeLabel: string(v1alpha1.AppTypeCompose),
			},
			spec: &v1alpha1.ApplicationProviderSpec{
				Name: lo.ToPtr("app"),
				EnvVars: lo.ToPtr(map[string]string{
					"!nvalid": "bar",
				}),
			},
			composeSpec: util.NewComposeSpec(),
			setupMocks: func(mockExec *executer.MockExecuter, appLabels string) {
				gomock.InOrder(
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"inspect", appImage}).Return(appLabels, "", 0),
				)
			},
			wantVerifyErr: errors.ErrInvalidSpec,
		},
		{
			name:  "appType compose with invalid hardcoded container name",
			image: appImage,
			labels: map[string]string{
				AppTypeLabel: string(v1alpha1.AppTypeCompose),
			},
			spec: &v1alpha1.ApplicationProviderSpec{
				Name: lo.ToPtr("app"),
			},
			composeSpec: `version: "3.8"
services:
  service1:
    container_name: app #invalid hardcoded container name
    image: quay.io/flightctl-tests/alpine:v1`,
			setupMocks: func(mockExec *executer.MockExecuter, appLabels string) {
				gomock.InOrder(
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"inspect", appImage}).Return(appLabels, "", 0),
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"inspect", appImage}).Return(appLabels, "", 0),
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"unshare", "podman", "image", "mount", appImage}).Return("/mount", "", 0),
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"image", "unmount", appImage}).Return("", "", 0),
				)
			},
			wantVerifyErr: validation.ErrHardCodedContainerName,
		},
		{
			name:  "appType compose with no services",
			image: appImage,
			labels: map[string]string{
				AppTypeLabel: string(v1alpha1.AppTypeCompose),
			},
			spec: &v1alpha1.ApplicationProviderSpec{
				Name: lo.ToPtr("app"),
			},
			composeSpec: `version: "3.8"
services:
image: quay.io/flightctl-tests/alpine:v1`,
			setupMocks: func(mockExec *executer.MockExecuter, appLabels string) {
				gomock.InOrder(
					// Inspect to determine appType from image label
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"inspect", appImage}).Return(appLabels, "", 0),
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"inspect", appImage}).Return(appLabels, "", 0),
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"unshare", "podman", "image", "mount", appImage}).Return("/mount", "", 0),
					mockExec.EXPECT().ExecuteWithContext(gomock.Any(), "podman", []string{"image", "unmount", appImage}).Return("", "", 0),
				)
			},
			wantVerifyErr: errors.ErrNoComposeServices,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := log.NewPrefixLogger("test")
			log.SetLevel(logrus.DebugLevel)

			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			mockExec := executer.NewMockExecuter(ctrl)
			tmpDir := t.TempDir()
			rw := fileio.NewReadWriter()
			rw.SetRootdir(tmpDir)
			err := rw.MkdirAll("/mount", fileio.DefaultDirectoryPermissions)
			require.NoError(err)
			err = rw.WriteFile("/mount/podman-compose.yaml", []byte(tt.composeSpec), fileio.DefaultFilePermissions)
			require.NoError(err)
			podman := client.NewPodman(log, mockExec, rw, util.NewPollConfig())

			spec := v1alpha1.ImageApplicationProviderSpec{
				Image: lo.ToPtr(tt.image),
			}
			provider := tt.spec
			err = provider.FromImageApplicationProviderSpec(spec)
			require.NoError(err)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			inspect := mockPodmanInspect(tt.labels)
			inspectBytes, err := json.Marshal(inspect)
			require.NoError(err)

			tt.setupMocks(mockExec, string(inspectBytes))

			appType := lo.FromPtr(tt.spec.AppType)
			if appType == "" {
				appType, err = typeFromImage(ctx, podman, tt.image)
				if err != nil && tt.wantVerifyErr != nil {
					require.ErrorIs(err, tt.wantVerifyErr)
					return
				}
			}

			imageProvider, err := newImage(log, podman, provider, rw, appType)
			if tt.wantVerifyErr != nil && err != nil {
				require.ErrorIs(err, tt.wantVerifyErr)
				return
			}
			require.NoError(err)

			err = imageProvider.Verify(ctx)
			if tt.wantVerifyErr != nil {
				require.Error(err)
				require.ErrorIs(err, tt.wantVerifyErr)
				return
			}
			require.NoError(err)
			err = imageProvider.Install(ctx)
			require.NoError(err)
			// verify env file
			if tt.spec.EnvVars != nil {
				appPath, err := pathFromAppType(imageProvider.spec.AppType, imageProvider.spec.Name, imageProvider.spec.Embedded)
				require.NoError(err)
				require.True(rw.PathExists(filepath.Join(appPath, ".env")))
				envFile, err := rw.ReadFile(filepath.Join(appPath, ".env"))
				require.NoError(err)
				for k, v := range lo.FromPtr(tt.spec.EnvVars) {
					require.Contains(string(envFile), k+"="+v)
				}
			}
		})
	}
}

func mockPodmanInspect(labels map[string]string) []client.PodmanInspect {
	inspect := client.PodmanInspect{
		Config: client.PodmanContainerConfig{
			Labels: labels,
		},
	}
	return []client.PodmanInspect{inspect}
}

func TestImageProviderArtifactQuadlet(t *testing.T) {
	require := require.New(t)

	log := log.NewPrefixLogger("test")
	log.SetLevel(logrus.DebugLevel)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockExec := executer.NewMockExecuter(ctrl)

	rw := fileio.NewReadWriter()
	rw.SetRootdir(t.TempDir())

	podman := client.NewPodman(log, mockExec, rw, util.NewPollConfig())

	artifactRef := "quay.io/flightctl-tests/quadlet-artifact:v1"
	appName := "artifact-app"

	providerSpec := &v1alpha1.ApplicationProviderSpec{
		Name:    lo.ToPtr(appName),
		AppType: lo.ToPtr(v1alpha1.AppTypeQuadlet),
	}
	imageSpec := v1alpha1.ImageApplicationProviderSpec{
		Artifact: lo.ToPtr(artifactRef),
	}
	require.NoError(providerSpec.FromImageApplicationProviderSpec(imageSpec))

	appType := lo.FromPtr(providerSpec.AppType)
	imageProvider, err := newImage(log, podman, providerSpec, rw, appType)
	require.NoError(err)

	inspect := mockPodmanInspect(map[string]string{})
	inspectBytes, err := json.Marshal(inspect)
	require.NoError(err)

	annotationJSON := `{"manifest":{"annotations":{"appType":"quadlet"}}}`

	writeQuadlet := func(dest string) {
		require.NoError(rw.MkdirAll(dest, fileio.DefaultDirectoryPermissions))
		content := "[Container]\nImage=docker.io/library/busybox:latest\n"
		require.NoError(rw.WriteFile(filepath.Join(dest, "web.container"), []byte(content), fileio.DefaultFilePermissions))
	}

	mockExec.EXPECT().
		ExecuteWithContext(gomock.Any(), "podman", gomock.AssignableToTypeOf([]string{})).
		DoAndReturn(func(ctx context.Context, cmd string, args ...string) (string, string, int) {
			switch {
			case len(args) == 1 && args[0] == "--version":
				return "podman version 5.5.1", "", 0
			case len(args) == 2 && args[0] == "inspect" && args[1] == artifactRef:
				return string(inspectBytes), "", 0
			case len(args) == 3 && args[0] == "artifact" && args[1] == "inspect" && args[2] == artifactRef:
				return annotationJSON, "", 0
			case len(args) >= 4 && args[0] == "artifact" && args[1] == "extract" && args[2] == artifactRef:
				dest := args[3]
				writeQuadlet(dest)
				return "", "", 0
			default:
				return "", "unexpected command", 125
			}
		}).
		AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(imageProvider.Verify(ctx))
	require.NoError(imageProvider.Install(ctx))

	appPath, err := pathFromAppType(imageProvider.spec.AppType, imageProvider.spec.Name, imageProvider.spec.Embedded)
	require.NoError(err)

	entries, err := rw.ReadDir(appPath)
	require.NoError(err)

	found := false
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".container") {
			found = true
			break
		}
	}
	require.True(found, "expected quadlet container file to be installed")
}

func TestImageProviderArtifactRequiresPodman55(t *testing.T) {
	require := require.New(t)

	log := log.NewPrefixLogger("test")
	log.SetLevel(logrus.DebugLevel)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockExec := executer.NewMockExecuter(ctrl)

	rw := fileio.NewReadWriter()
	rw.SetRootdir(t.TempDir())

	podman := client.NewPodman(log, mockExec, rw, util.NewPollConfig())

	artifactRef := "quay.io/flightctl-tests/quadlet-artifact:v1"
	appName := "artifact-app"

	providerSpec := &v1alpha1.ApplicationProviderSpec{
		Name:    lo.ToPtr(appName),
		AppType: lo.ToPtr(v1alpha1.AppTypeQuadlet),
	}
	imageSpec := v1alpha1.ImageApplicationProviderSpec{
		Artifact: lo.ToPtr(artifactRef),
	}
	require.NoError(providerSpec.FromImageApplicationProviderSpec(imageSpec))

	appType := lo.FromPtr(providerSpec.AppType)
	imageProvider, err := newImage(log, podman, providerSpec, rw, appType)
	require.NoError(err)

	inspectJSON := `[{"Annotations":{"appType":"quadlet"},"Config":{"Labels":{}}}]`

	mockExec.EXPECT().
		ExecuteWithContext(gomock.Any(), "podman", gomock.AssignableToTypeOf([]string{})).
		DoAndReturn(func(ctx context.Context, cmd string, args ...string) (string, string, int) {
			switch {
			case len(args) == 2 && args[0] == "inspect" && args[1] == artifactRef:
				return inspectJSON, "", 0
			case len(args) == 1 && args[0] == "--version":
				return "podman version 5.4.0", "", 0
			default:
				return "", "unexpected command", 125
			}
		}).
		AnyTimes()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = imageProvider.Verify(ctx)
	require.Error(err)
	require.Contains(err.Error(), "podman >= 5.5")
}
