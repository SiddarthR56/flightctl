package client

import (
	"context"
	"testing"

	"github.com/flightctl/flightctl/internal/agent/device/fileio"
	"github.com/flightctl/flightctl/pkg/executer"
	"github.com/flightctl/flightctl/pkg/log"
	"github.com/flightctl/flightctl/test/util"
	"github.com/stretchr/testify/require"
	"go.uber.org/gomock"
)

func newTestPodman(t *testing.T, mockExec *executer.MockExecuter) *Podman {
	t.Helper()

	logger := log.NewPrefixLogger("test")
	rw := fileio.NewReadWriter(fileio.WithTestRootDir(t.TempDir()))

	return NewPodman(logger, mockExec, rw, util.NewPollConfig())
}

func TestEnsureArtifactSupport(t *testing.T) {
	testCases := []struct {
		name          string
		versionOutput string
		exitCode      int
		expectErr     bool
	}{
		{
			name:          "podman 5.5 passes",
			versionOutput: "podman version 5.5.0",
		},
		{
			name:          "podman 5.6 passes",
			versionOutput: "podman version 5.6.1",
		},
		{
			name:          "podman below minimum fails",
			versionOutput: "podman version 5.4.0",
			expectErr:     true,
		},
		{
			name:      "version command failure",
			exitCode:  125,
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			mockExec := executer.NewMockExecuter(ctrl)
			podman := newTestPodman(t, mockExec)

			stderr := ""
			if tc.exitCode != 0 {
				stderr = "boom"
			}

			mockExec.EXPECT().
				ExecuteWithContext(gomock.Any(), "podman", "--version").
				Return(tc.versionOutput, stderr, tc.exitCode)

			err := podman.EnsureArtifactSupport(context.Background())
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPullArtifactChecksPodmanVersion(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := executer.NewMockExecuter(ctrl)
	podman := newTestPodman(t, mockExec)

	artifactRef := "example.com/org/app:latest"

	gomock.InOrder(
		mockExec.EXPECT().
			ExecuteWithContext(gomock.Any(), "podman", "--version").
			Return("podman version 5.5.1", "", 0),
		mockExec.EXPECT().
			ExecuteWithContext(gomock.Any(), "podman", "artifact", "pull", artifactRef).
			Return("pulled", "", 0),
	)

	_, err := podman.pullArtifact(context.Background(), artifactRef, &clientOptions{})
	require.NoError(t, err)
}

func TestPullArtifactFailsWhenPodmanTooOld(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := executer.NewMockExecuter(ctrl)
	podman := newTestPodman(t, mockExec)

	mockExec.EXPECT().
		ExecuteWithContext(gomock.Any(), "podman", "--version").
		Return("podman version 5.4.2", "", 0)

	_, err := podman.pullArtifact(context.Background(), "example.com/org/app:latest", &clientOptions{})
	require.Error(t, err)
}

func TestArtifactExistsUsesArtifactInspect(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := executer.NewMockExecuter(ctrl)
	podman := newTestPodman(t, mockExec)

	artifactRef := "registry.example.com/org/app:latest"

	mockExec.EXPECT().
		ExecuteWithContext(gomock.Any(), "podman", "artifact", "inspect", artifactRef).
		Return("", "", 0)

	require.True(t, podman.ArtifactExists(context.Background(), artifactRef))
}

func TestArtifactExistsReturnsFalseOnError(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockExec := executer.NewMockExecuter(ctrl)
	podman := newTestPodman(t, mockExec)

	artifactRef := "registry.example.com/org/app:latest"

	mockExec.EXPECT().
		ExecuteWithContext(gomock.Any(), "podman", "artifact", "inspect", artifactRef).
		Return("", "boom", 125)

	require.False(t, podman.ArtifactExists(context.Background(), artifactRef))
}
