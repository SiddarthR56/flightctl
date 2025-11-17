package provider

import (
	"archive/tar"
	"compress/gzip"
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

func TestExpandArtifactArchives(t *testing.T) {
	t.Run("extracts tar gz archive", func(t *testing.T) {
		rw := fileio.NewReadWriter(fileio.WithTestRootDir(t.TempDir()))
		dest := "/tmp/app"
		require.NoError(t, rw.MkdirAll(dest, fileio.DefaultDirectoryPermissions))

		writeArchive(t, rw.PathFor(filepath.Join(dest, "bundle.tar.gz")), true, map[string]string{
			"app.container":                "container",
			"container.d/10-override.conf": "override",
		})

		require.NoError(t, expandArtifactArchives(rw, dest))

		entries, err := rw.ReadDir(dest)
		require.NoError(t, err)
		require.Len(t, entries, 2)

		content, err := rw.ReadFile(filepath.Join(dest, "app.container"))
		require.NoError(t, err)
		require.Equal(t, "container", string(content))

		exists, err := rw.PathExists(filepath.Join(dest, "bundle.tar.gz"), fileio.WithSkipContentCheck())
		require.NoError(t, err)
		require.False(t, exists)
	})

	t.Run("handles tar archive alongside raw files", func(t *testing.T) {
		rw := fileio.NewReadWriter(fileio.WithTestRootDir(t.TempDir()))
		dest := "/tmp/app"
		require.NoError(t, rw.MkdirAll(dest, fileio.DefaultDirectoryPermissions))
		require.NoError(t, rw.WriteFile(filepath.Join(dest, "existing.container"), []byte("raw"), fileio.DefaultFilePermissions))

		writeArchive(t, rw.PathFor(filepath.Join(dest, "bundle.tar")), false, map[string]string{
			"app.network": "net",
		})

		require.NoError(t, expandArtifactArchives(rw, dest))

		content, err := rw.ReadFile(filepath.Join(dest, "app.network"))
		require.NoError(t, err)
		require.Equal(t, "net", string(content))

		raw, err := rw.ReadFile(filepath.Join(dest, "existing.container"))
		require.NoError(t, err)
		require.Equal(t, "raw", string(raw))

		exists, err := rw.PathExists(filepath.Join(dest, "bundle.tar"), fileio.WithSkipContentCheck())
		require.NoError(t, err)
		require.False(t, exists)
	})

	t.Run("rejects traversal paths", func(t *testing.T) {
		rw := fileio.NewReadWriter(fileio.WithTestRootDir(t.TempDir()))
		dest := "/tmp/app"
		require.NoError(t, rw.MkdirAll(dest, fileio.DefaultDirectoryPermissions))

		writeArchive(t, rw.PathFor(filepath.Join(dest, "bundle.tar")), false, map[string]string{
			"../evil.container": "nope",
		})

		err := expandArtifactArchives(rw, dest)
		require.Error(t, err)
		require.Contains(t, err.Error(), "path traversal")
	})
}

func writeArchive(t *testing.T, archivePath string, gz bool, files map[string]string) {
	t.Helper()
	file, err := os.Create(archivePath)
	require.NoError(t, err)
	defer file.Close()

	var writer *tar.Writer
	if gz {
		gzw := gzip.NewWriter(file)
		defer gzw.Close()
		writer = tar.NewWriter(gzw)
	} else {
		writer = tar.NewWriter(file)
	}
	defer writer.Close()

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		require.NoError(t, writer.WriteHeader(hdr))
		_, err := writer.Write([]byte(content))
		require.NoError(t, err)
	}
}
