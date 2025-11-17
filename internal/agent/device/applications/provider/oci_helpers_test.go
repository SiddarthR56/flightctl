package provider

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/flightctl/flightctl/internal/agent/device/fileio"
	"github.com/stretchr/testify/require"
)

func TestPrepareArtifactDestination(t *testing.T) {
	t.Run("creates missing directory", func(t *testing.T) {
		rw := fileio.NewReadWriter(fileio.WithTestRootDir(t.TempDir()))
		dest := "/etc/containers/systemd/my-app"

		require.NoError(t, prepareArtifactDestination(rw, dest))

		info, err := os.Stat(rw.PathFor(dest))
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("replaces existing file", func(t *testing.T) {
		rw := fileio.NewReadWriter(fileio.WithTestRootDir(t.TempDir()))
		dest := "/etc/containers/systemd/my-app"
		fullPath := rw.PathFor(dest)

		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), fileio.DefaultDirectoryPermissions))
		require.NoError(t, os.WriteFile(fullPath, []byte("quadlet"), fileio.DefaultFilePermissions))

		require.NoError(t, prepareArtifactDestination(rw, dest))

		info, err := os.Stat(fullPath)
		require.NoError(t, err)
		require.True(t, info.IsDir())
	})

	t.Run("clears existing directory contents", func(t *testing.T) {
		rw := fileio.NewReadWriter(fileio.WithTestRootDir(t.TempDir()))
		dest := "/etc/containers/systemd/my-app"

		require.NoError(t, rw.MkdirAll(dest, fileio.DefaultDirectoryPermissions))
		require.NoError(t, rw.WriteFile(filepath.Join(dest, "old.container"), []byte("content"), fileio.DefaultFilePermissions))

		require.NoError(t, prepareArtifactDestination(rw, dest))

		entries, err := rw.ReadDir(dest)
		require.NoError(t, err)
		require.Len(t, entries, 0)
	})
}
