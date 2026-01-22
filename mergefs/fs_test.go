package mergefs

import (
	"fmt"
	"io/fs"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestMountFs_MountAndGetMount(t *testing.T) {
	// 创建内存文件系统作为默认文件系统
	defaultFs := afero.NewMemMapFs()
	// 创建一个新的 MountFs
	mountFs := NewMountFs(defaultFs)

	// 创建一个用于挂载的内存文件系统
	mountedFs := afero.NewMemMapFs()
	// 挂载前缀
	prefix := "/test"

	// 挂载文件系统
	err := mountFs.Mount(prefix, mountedFs)
	assert.NoError(t, err, "挂载文件系统不应出错")

	// 测试获取挂载点
	fs, path := mountFs.GetMount("/test/some/path")
	assert.Equal(t, mountedFs, fs, "获取的应是挂载的文件系统")
	assert.Equal(t, "/some/path", path, "获取的应是挂载点内的相对路径")

	// 测试获取根路径
	fs, path = mountFs.GetMount("/")
	assert.Equal(t, defaultFs, fs, "根路径应返回默认文件系统")
	assert.Equal(t, "/", path, "根路径的相对路径应为'/'")

	// 测试获取未挂载的路径
	fs, path = mountFs.GetMount("/unmounted/path")
	assert.Equal(t, defaultFs, fs, "未挂载的路径应返回默认文件系统")
	assert.Equal(t, "/unmounted/path", path, "未挂载的路径的相对路径应为原路径")
}

func TestMountFs_MkdirAndStat(t *testing.T) {
	defaultFs := afero.NewMemMapFs()
	mountFs := NewMountFs(defaultFs)
	mountedFs := afero.NewMemMapFs()
	_ = mountFs.Mount("/mounted", mountedFs)

	// 在默认文件系统上创建目录
	err := mountFs.Mkdir("/dir1", 0755)
	assert.NoError(t, err, "在默认文件系统上创建目录不应出错")

	// 验证目录是否存在
	info, err := mountFs.Stat("/dir1")
	assert.NoError(t, err, "Stat 不应出错")
	assert.True(t, info.IsDir(), "应为目录")

	// 在挂载的文件系统上创建目录
	err = mountFs.Mkdir("/mounted/dir2", 0755)
	assert.NoError(t, err, "在挂载的文件系统上创建目录不应出错")

	// 验证目录是否存在
	info, err = mountFs.Stat("/mounted/dir2")
	assert.NoError(t, err, "Stat 不应出错")
	assert.True(t, info.IsDir(), "应为目录")

	// 试图创建已存在的目录
	err = mountFs.Mkdir("/dir1", 0755)
	assert.Error(t, err, "试图创建已存在的目录应出错")
	assert.True(t, os.IsExist(err), "错误应为 os.ErrExist")

	// Stat 挂载点本身
	info, err = mountFs.Stat("/mounted")
	assert.NoError(t, err, "Stat 挂载点不应出错")
	assert.True(t, info.IsDir(), "挂载点应是目录")
	assert.Equal(t, "mounted", info.Name(), "挂载点名称应正确")
}

func TestMountFs_CreateAndRemove(t *testing.T) {
	defaultFs := afero.NewMemMapFs()
	mountFs := NewMountFs(defaultFs)
	mountedFs := afero.NewMemMapFs()
	_ = mountFs.Mount("/mounted", mountedFs)

	// 在默认文件系统创建文件
	file, err := mountFs.Create("/file1.txt")
	assert.NoError(t, err, "在默认文件系统创建文件不应出错")
	_ = file.Close()

	// 验证文件存在
	_, err = mountFs.Stat("/file1.txt")
	assert.NoError(t, err, "Stat 文件不应出错")

	// 删除文件
	err = mountFs.Remove("/file1.txt")
	assert.NoError(t, err, "删除文件不应出错")

	// 验证文件已删除
	_, err = mountFs.Stat("/file1.txt")
	assert.Error(t, err, "Stat 已删除文件应出错")

	// 在挂载的文件系统创建文件
	file, err = mountFs.Create("/mounted/file2.txt")
	assert.NoError(t, err, "在挂载的文件系统创建文件不应出错")
	_ = file.Close()

	// 删除挂载点上的文件
	err = mountFs.Remove("/mounted/file2.txt")
	assert.NoError(t, err, "删除挂载点上的文件不应出错")

	// 试图删除挂载点
	err = mountFs.Remove("/mounted")
	assert.Error(t, err, "试图删除挂载点应出错")
}

func TestMountFs_Readdir(t *testing.T) {
	defaultFs := afero.NewMemMapFs()
	_ = defaultFs.Mkdir("/dir_in_default", 0755)
	_, _ = defaultFs.Create("/file_in_default.txt")

	mountFs := NewMountFs(defaultFs)

	mountedFs1 := afero.NewMemMapFs()
	_ = mountedFs1.Mkdir("/dir_in_mounted1", 0755)
	_, _ = mountedFs1.Create("/file_in_mounted1.txt")
	_ = mountFs.Mount("/mount1", mountedFs1)

	mountedFs2 := afero.NewMemMapFs()
	_ = mountFs.Mount("/mount2", mountedFs2)
	_ = mountFs.Mount("/mount1/sub_mount", afero.NewMemMapFs())

	// 读取根目录
	file, err := mountFs.Open("/")
	assert.NoError(t, err)
	defer file.Close()

	entries, err := file.Readdir(0)
	assert.NoError(t, err)

	expected := []string{"dir_in_default", "file_in_default.txt", "mount1", "mount2"}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	assert.ElementsMatch(t, expected, names, "根目录列表应包含默认文件系统内容和挂载点")

	// 读取挂载点目录
	file, err = mountFs.Open("/mount1")
	assert.NoError(t, err)
	defer file.Close()

	entries, err = file.Readdir(0)
	assert.NoError(t, err)
	fmt.Println(entries)

	expected = []string{"dir_in_mounted1", "file_in_mounted1.txt", "sub_mount"}
	names = make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	assert.ElementsMatch(t, expected, names, "挂载点目录列表应正确")
}

func TestMountFs_Rename(t *testing.T) {
	defaultFs := afero.NewMemMapFs()
	_, _ = defaultFs.Create("/file1.txt")
	_ = defaultFs.MkdirAll("/src/sub", 0755)
	_, _ = defaultFs.Create("/src/sub/file.txt")

	mountFs := NewMountFs(defaultFs)
	mountedFs := afero.NewMemMapFs()
	_ = mountFs.Mount("/mounted", mountedFs)

	// 在同一文件系统内重命名文件
	err := mountFs.Rename("/file1.txt", "/file2.txt")
	assert.NoError(t, err, "在同一文件系统内重命名文件不应出错")
	_, err = mountFs.Stat("/file2.txt")
	assert.NoError(t, err, "文件应被重命名")

	// 跨文件系统重命名文件（从默认到挂载）
	err = mountFs.Rename("/file2.txt", "/mounted/file3.txt")
	assert.NoError(t, err, "跨文件系统重命名文件不应出错")
	_, err = mountFs.Stat("/file2.txt")
	assert.Error(t, err, "源文件应被删除")
	_, err = mountFs.Stat("/mounted/file3.txt")
	assert.NoError(t, err, "目标文件应被创建")

	// 跨文件系统重命名目录
	err = mountFs.Rename("/src", "/mounted/dest")
	assert.NoError(t, err, "跨文件系统重命名目录不应出错")
	_, err = mountFs.Stat("/src")
	assert.Error(t, err, "源目录应被删除")
	_, err = mountFs.Stat("/mounted/dest")
	assert.NoError(t, err, "目标目录应被创建")
	_, err = mountFs.Stat("/mounted/dest/sub/file.txt")
	assert.NoError(t, err, "目录内容应被移动")
}
func TestMountFs_OpenFile(t *testing.T) {
	// Setup
	defaultFs := afero.NewMemMapFs()
	mountFs := NewMountFs(defaultFs)
	mountedFs := afero.NewMemMapFs()
	assert.NoError(t, mountFs.Mount("/mounted", mountedFs))
	_, err := defaultFs.Create("/test.txt")
	assert.NoError(t, err)
	assert.NoError(t, mountedFs.Mkdir("/dir", 0755))

	// Test opening a file from the default FS
	file, err := mountFs.OpenFile("/test.txt", os.O_RDONLY, 0)
	assert.NoError(t, err)
	assert.NotNil(t, file)
	file.Close()

	// Test opening a directory from the mounted FS
	dir, err := mountFs.OpenFile("/mounted/dir", os.O_RDONLY, 0)
	assert.NoError(t, err)
	assert.NotNil(t, dir)
	// Check if it's a mountFsFile
	_, ok := dir.(*mountFsFile)
	assert.True(t, ok, "Directory should be wrapped in mountFsFile")
	dir.Close()

	// Test opening a non-existent file
	_, err = mountFs.OpenFile("/nonexistent.txt", os.O_RDONLY, 0)
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err))
}
func TestMountFs_RemoveAll(t *testing.T) {
	// Setup
	defaultFs := afero.NewMemMapFs()
	mountFs := NewMountFs(defaultFs)
	mountedFs := afero.NewMemMapFs()
	assert.NoError(t, mountFs.Mount("/mounted", mountedFs))
	assert.NoError(t, mountFs.Mount("/mounted/sub", afero.NewMemMapFs()))
	assert.NoError(t, defaultFs.MkdirAll("/a/b/c", 0755))

	// Test removing a directory with a mount point under it
	err := mountFs.RemoveAll("/mounted")
	assert.Error(t, err)
	pathErr, ok := err.(*fs.PathError)
	assert.True(t, ok)
	assert.Equal(t, "remove", pathErr.Op)
	assert.Equal(t, "/mounted", pathErr.Path)
	assert.Contains(t, pathErr.Err.Error(), "permission denied")

	// Test removing a directory from default Fs
	err = mountFs.RemoveAll("/a")
	assert.NoError(t, err)
	_, err = mountFs.Stat("/a")
	assert.True(t, os.IsNotExist(err))

}
func TestMountFs_Unmount(t *testing.T) {
	// Setup
	defaultFs := afero.NewMemMapFs()
	mountFs := NewMountFs(defaultFs)
	mountedFs := afero.NewMemMapFs()
	prefix := "/test"
	assert.NoError(t, mountFs.Mount(prefix, mountedFs))

	// Test unmounting an existing mount point
	unmounted := mountFs.Unmount(prefix)
	assert.True(t, unmounted)

	// Verify that the mount point is gone
	fs, path := mountFs.GetMount(prefix + "/some/path")
	assert.Equal(t, defaultFs, fs)
	assert.Equal(t, "/test/some/path", path)

	// Test unmounting a non-existent mount point
	unmounted = mountFs.Unmount("/nonexistent")
	assert.False(t, unmounted)
}

func TestMountFs_NestedMount(t *testing.T) {
	defaultFs := afero.NewMemMapFs()
	mountFs := NewMountFs(defaultFs)

	// Mount a filesystem at a nested path
	mountedFs := afero.NewMemMapFs()
	_ = mountedFs.Mkdir("/testdir", 0755)
	err := mountFs.Mount("/path/to/alice", mountedFs)
	assert.NoError(t, err)

	// 1. Check contents of root ("/")
	root, err := mountFs.Open("/")
	assert.NoError(t, err)
	rootEntries, err := root.Readdirnames(0)
	assert.NoError(t, err)
	assert.Contains(t, rootEntries, "path", "Root should contain virtual 'path' directory")

	// 2. Check contents of "/path"
	pathDir, err := mountFs.Open("/path")
	assert.NoError(t, err)
	pathEntries, err := pathDir.Readdirnames(0)
	assert.NoError(t, err)
	assert.Contains(t, pathEntries, "to", "'/path' should contain virtual 'to' directory")

	// 3. Check contents of "/path/to"
	toDir, err := mountFs.Open("/path/to")
	assert.NoError(t, err)
	toEntries, err := toDir.Readdirnames(0)
	assert.NoError(t, err)
	assert.Contains(t, toEntries, "alice", "'/path/to' should contain 'alice' mount point")

	// 4. Check stat of virtual directories
	pathInfo, err := mountFs.Stat("/path")
	assert.NoError(t, err)
	assert.True(t, pathInfo.IsDir())

	toInfo, err := mountFs.Stat("/path/to")
	assert.NoError(t, err)
	assert.True(t, toInfo.IsDir())

	// 5. Check stat of the actual mount point
	aliceInfo, err := mountFs.Stat("/path/to/alice")
	assert.NoError(t, err)
	assert.True(t, aliceInfo.IsDir())

	// 6. Check contents of the actual mount point
	aliceDir, err := mountFs.Open("/path/to/alice")
	assert.NoError(t, err)
	aliceEntries, err := aliceDir.Readdirnames(0)
	assert.NoError(t, err)
	assert.Contains(t, aliceEntries, "testdir")
}
