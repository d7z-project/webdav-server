package mergefs

import (
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"io"
	"os"
	"testing"
)

func TestMountFsFile_Readdir(t *testing.T) {
	// Setup
	defaultFs := afero.NewMemMapFs()
	mountFs := NewMountFs(defaultFs)

	// Create some files and directories in the default FS
	_ = defaultFs.Mkdir("/dir1", 0755)
	_, _ = defaultFs.Create("/file1.txt")

	// Create a mounted FS
	mountedFs := afero.NewMemMapFs()
	_ = mountFs.Mount("/mounted", mountedFs) // Mount an empty filesystem at /mounted

	// Open the root directory
	file, err := mountFs.Open("/")
	assert.NoError(t, err)
	defer file.Close()

	// Check if it's a mountFsFile
	mountFile, ok := file.(*mountFsFile)
	assert.True(t, ok)

	// Test initial Readdir(0) - should get all entries
	entries, err := mountFile.Readdir(0)
	assert.NoError(t, err)

	expectedNames := []string{"dir1", "file1.txt", "mounted"}
	assert.Len(t, entries, len(expectedNames), "Expected number of entries mismatch after Readdir(0)")
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	t.Logf("Readdir(0) entries: %v", names) // Debug log
	assert.ElementsMatch(t, expectedNames, names, "Root directory list should include default FS content and mount point")

	// Reset for sequential reading with count > 0
	t.Logf("--- Starting sequential Readdir(1) tests ---")
	_, _ = mountFile.Seek(0, io.SeekStart) // Reset offset

	// First Readdir(1) call
	entries, err = mountFile.Readdir(1)
	t.Logf("First Readdir(1) call: entries=%v, err=%v", entries, err)
	assert.NoError(t, err, "First Readdir(1) call should not return an error")
	assert.Len(t, entries, 1, "First Readdir(1) call should return 1 entry")
	assert.Equal(t, "dir1", entries[0].Name(), "First entry should be 'dir1'")

	// Second Readdir(1) call
	entries, err = mountFile.Readdir(1)
	t.Logf("Second Readdir(1) call: entries=%v, err=%v", entries, err)
	assert.NoError(t, err, "Second Readdir(1) call should not return an error")
	assert.Len(t, entries, 1, "Second Readdir(1) call should return 1 entry")
	assert.Equal(t, "file1.txt", entries[0].Name(), "Second entry should be 'file1.txt'")

	// Third Readdir(1) call
	entries, err = mountFile.Readdir(1)
	t.Logf("Third Readdir(1) call: entries=%v, err=%v", entries, err)
	assert.NoError(t, err, "Third Readdir(1) call should not return an error")
	assert.Len(t, entries, 1, "Third Readdir(1) call should return 1 entry")
	assert.Equal(t, "mounted", entries[0].Name(), "Third entry should be 'mounted'")

	// Fourth Readdir(1) call - should be EOF
	entries, err = mountFile.Readdir(1)
	t.Logf("Fourth Readdir(1) call: entries=%v, err=%v", entries, err)
	assert.Equal(t, io.EOF, err, "Fourth Readdir(1) call should return io.EOF")
	assert.Empty(t, entries, "Fourth Readdir(1) call should return empty entries")

	// Subsequent calls should also return io.EOF
	entries, err = mountFile.Readdir(1)
	t.Logf("Fifth Readdir(1) call: entries=%v, err=%v", entries, err)
	assert.Equal(t, io.EOF, err, "Fifth Readdir(1) call should also return io.EOF")
	assert.Empty(t, entries, "Fifth Readdir(1) call should return empty entries")
}

func TestMountFsFile_Readdirnames(t *testing.T) {
	// Setup
	defaultFs := afero.NewMemMapFs()
	mountFs := NewMountFs(defaultFs)
	_ = defaultFs.Mkdir("/dir1", 0755)
	_, _ = defaultFs.Create("/file1.txt")
	mountedFs := afero.NewMemMapFs()
	_ = mountFs.Mount("/mounted", mountedFs) // Mount an empty filesystem at /mounted

	file, err := mountFs.Open("/")
	assert.NoError(t, err)
	defer file.Close()

	mountFile, ok := file.(*mountFsFile)
	assert.True(t, ok)

	names, err := mountFile.Readdirnames(0)
	assert.NoError(t, err)
	expectedNames := []string{"dir1", "file1.txt", "mounted"}
	assert.ElementsMatch(t, expectedNames, names)

	// Test Readdirnames with count
	_, _ = mountFile.Seek(0, io.SeekStart) // Reset
	names, err = mountFile.Readdirnames(1)
	assert.NoError(t, err)
	assert.Len(t, names, 1)
	assert.Equal(t, "dir1", names[0])

	names, err = mountFile.Readdirnames(1)
	assert.NoError(t, err)
	assert.Len(t, names, 1)
	assert.Equal(t, "file1.txt", names[0])

	names, err = mountFile.Readdirnames(1)
	assert.NoError(t, err)
	assert.Len(t, names, 1)
	assert.Equal(t, "mounted", names[0])

	_, err = mountFile.Readdirnames(1)
	assert.Equal(t, io.EOF, err)
	_, err = mountFile.Readdirnames(1)
	assert.Equal(t, io.EOF, err)
}

func TestMountFsFile_Seek(t *testing.T) {
	defaultFs := afero.NewMemMapFs()
	mountFs := NewMountFs(defaultFs)
	_ = defaultFs.Mkdir("/dir", 0755)
	file, err := mountFs.Open("/dir")
	assert.NoError(t, err)
	defer file.Close()

	mountFile, ok := file.(*mountFsFile)
	assert.True(t, ok)
	mountFile.offset = 10 // set some offset

	// Seek to start
	n, err := mountFile.Seek(0, io.SeekStart)
	assert.NoError(t, err)
	assert.Equal(t, int64(0), n)
	assert.Equal(t, 0, mountFile.offset)

	// Other seeks should be passed to the underlying file
	// (MemMapFile doesn't support seeking on directories, so we can't test much more)
	n, err = mountFile.Seek(0, io.SeekCurrent)
	assert.NoError(t, err) // Should succeed even if it does nothing
	assert.Equal(t, int64(0), n)
}
func TestDirEntry(t *testing.T) {
	info, _ := afero.NewMemMapFs().Create("test")
	defer info.Close()
	stat, _ := info.Stat()
	entry := &dirEntry{info: stat}

	assert.Equal(t, stat.Name(), entry.Name())
	assert.Equal(t, stat.IsDir(), entry.IsDir())
	assert.Equal(t, stat.Mode().Type(), entry.Type())
	i, err := entry.Info()
	assert.NoError(t, err)
	assert.Equal(t, stat, i)
}

func TestMountDirEntry(t *testing.T) {
	mount := &Mount{Prefix: "/m", Fs: afero.NewMemMapFs()}
	entry := &mountDirEntry{
		name:  "test",
		mode:  os.ModeDir | 0755,
		mount: mount,
	}

	assert.Equal(t, "test", entry.Name())
	assert.True(t, entry.IsDir())
	assert.Equal(t, os.ModeDir, entry.Type())
	assert.Equal(t, int64(0), entry.Size())
	assert.Equal(t, os.ModeDir|os.FileMode(0755), entry.Mode())
	assert.False(t, entry.ModTime().IsZero())

	info, err := entry.Info()
	assert.NoError(t, err)
	assert.Equal(t, entry, info)
}
