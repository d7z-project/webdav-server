package mergefs

import (
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/spf13/afero"
)

// Mount 定义挂载点
type Mount struct {
	Prefix string
	Fs     afero.Fs
}

// MountFs 实现支持多个挂载点的文件系统
type MountFs struct {
	mounts    []Mount
	defaultFs afero.Fs
	mu        sync.RWMutex
}

// NewMountFs 创建新的 MountFs
func NewMountFs(defaultFs afero.Fs) *MountFs {
	if defaultFs == nil {
		defaultFs = afero.NewOsFs()
	}
	return &MountFs{
		mounts:    make([]Mount, 0),
		defaultFs: defaultFs,
	}
}

// Mount 添加挂载点
func (m *MountFs) Mount(prefix string, fs afero.Fs) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix = "/" + strings.Trim(prefix, "/")
	if prefix == "/" {
		return fmt.Errorf("prefix must not be /")
	}
	for _, mount := range m.mounts {
		if mount.Prefix == prefix {
			return fmt.Errorf("mount point %q already exists", prefix)
		}
	}
	m.mounts = append(m.mounts, Mount{Prefix: prefix, Fs: fs})
	slices.SortFunc(m.mounts, func(a, b Mount) int {
		return -cmp.Compare(a.Prefix, b.Prefix)
	})
	return nil
}

func (m *MountFs) Unmount(prefix string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	prefix = "/" + strings.Trim(prefix, "/")
	for i, mount := range m.mounts {
		if mount.Prefix == prefix {
			m.mounts = append(m.mounts[:i], m.mounts[i+1:]...)
			return true
		}
	}
	return false
}

// GetMount 获取指定路径对应的挂载点和相对路径
func (m *MountFs) GetMount(path string) (afero.Fs, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	path = NormalizePath(path)
	if path == "/" {
		// fmt.Println("DEBUG: GetMount returning defaultFs for /")
		return m.defaultFs, path
	}
	for _, mount := range m.mounts {
		if path == mount.Prefix || strings.HasPrefix(path, mount.Prefix+"/") {
			return mount.Fs, strings.TrimPrefix(path, mount.Prefix)
		}
	}
	return m.defaultFs, path
}

// NormalizePath 清理路径
func NormalizePath(p string) string {
	p = path.Clean(filepath.ToSlash(p))
	if p == "." {
		p = "/"
	}
	return "/" + strings.Trim(p, "/")
}

func (m *MountFs) Create(name string) (afero.File, error) {
	mount, p := m.GetMount(name)
	return mount.Create(p)
}

func (m *MountFs) Mkdir(name string, perm os.FileMode) error {
	if _, ok := m.directDir(name); ok {
		return &os.PathError{
			Op:   "mkdir",
			Path: name,
			Err:  os.ErrExist,
		}
	}
	mount, p := m.GetMount(name)
	return mount.Mkdir(p, perm)
}

func (m *MountFs) MkdirAll(path string, perm os.FileMode) error {
	if _, ok := m.directDir(path); ok {
		return &os.PathError{
			Op:   "mkdir",
			Path: path,
			Err:  os.ErrExist,
		}
	}
	mount, relPath := m.GetMount(path)
	return mount.MkdirAll(relPath, perm)
}

func (m *MountFs) Remove(path string) error {
	// 挂载点无法被删除
	if _, ok := m.directDir(path); ok {
		return &os.PathError{
			Op:   "remove",
			Path: path,
			Err:  os.ErrPermission,
		}
	}
	// 如果存在子路径挂载则也无法删除
	if m.hasChildMount(path) {
		return &os.PathError{
			Op:   "remove",
			Path: path,
			Err:  fmt.Errorf("directory contains a mount point"),
		}
	}
	mount, p := m.GetMount(path)
	return mount.Remove(p)
}

func (m *MountFs) RemoveAll(path string) error {
	if _, ok := m.directDir(path); ok {
		return &os.PathError{
			Op:   "remove",
			Path: path,
			Err:  os.ErrPermission,
		}
	}
	// 如果存在子路径挂载则也无法删除
	if m.hasChildMount(path) {
		return &os.PathError{
			Op:   "remove",
			Path: path,
			Err:  fmt.Errorf("directory contains a mount point"),
		}
	}
	mount, relPath := m.GetMount(path)
	return mount.RemoveAll(relPath)
}

func (m *MountFs) Rename(oldname, newname string) error {
	if m.hasChildMount(oldname) {
		return &os.PathError{
			Op:   "rename",
			Path: oldname,
			Err:  fmt.Errorf("directory contains a mount point"),
		}
	}

	oldFs, oldPath := m.GetMount(oldname)
	newFs, newPath := m.GetMount(newname)

	// 如果跨文件系统，需要特殊处理
	if oldFs != newFs {
		return m.crossRename(oldFs, oldPath, newFs, newPath)
	}

	return oldFs.Rename(oldPath, newPath)
}

func (m *MountFs) crossRename(srcFs afero.Fs, src string, dstFs afero.Fs, dst string) error {
	srcFile, err := srcFs.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}
	if srcInfo.IsDir() {
		return m.crossRenameDir(srcFs, src, dstFs, dst)
	}

	// copy file
	err = copyFile(srcFs, src, dstFs, dst)
	if err != nil {
		return err
	}
	return srcFs.Remove(src)
}

func (m *MountFs) crossRenameDir(srcFs afero.Fs, src string, dstFs afero.Fs, dst string) error {
	// 创建目标目录
	err := dstFs.MkdirAll(dst, 0o755)
	if err != nil {
		return err
	}
	dir, err := srcFs.Open(src)
	if err != nil {
		return err
	}
	defer dir.Close()

	infos, err := dir.Readdir(-1)
	if err != nil {
		return err
	}
	for _, info := range infos {
		srcPath := path.Join(src, info.Name())
		dstPath := path.Join(dst, info.Name())

		if info.IsDir() {
			err = m.crossRenameDir(srcFs, srcPath, dstFs, dstPath)
		} else {
			err = copyFile(srcFs, srcPath, dstFs, dstPath)
		}

		if err != nil {
			return err
		}
	}
	return srcFs.RemoveAll(src)
}

func copyFile(srcFs afero.Fs, src string, dstFs afero.Fs, dst string) error {
	srcFile, err := srcFs.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	dstFile, err := dstFs.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()
	_, err = io.Copy(dstFile, srcFile)
	if err != nil {
		_ = dstFs.Remove(dst)
		return err
	}
	srcInfo, err := srcFs.Stat(src)
	if err != nil {
		_ = dstFs.Remove(dst)
		return err
	}
	err = dstFs.Chmod(dst, srcInfo.Mode())
	if err != nil {
		_ = dstFs.Remove(dst)
		return err
	}
	return nil
}

func (m *MountFs) Stat(name string) (os.FileInfo, error) {
	name = NormalizePath(name)

	// 1. Check for direct mount points
	if mount, ok := m.directDir(name); ok {
		return &mountFileInfo{
			name:  filepath.Base(name),
			mode:  os.ModeDir | 0o755,
			mount: &mount,
		}, nil
	}

	// 2. Check underlying filesystem
	mount, p := m.GetMount(name)
	info, err := mount.Stat(p)
	if err == nil {
		return info, nil
	}
	// If the error is not 'IsNotExist', return it immediately
	if !os.IsNotExist(err) {
		return nil, err
	}

	// 3. Check for virtual intermediate directories
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, mount := range m.mounts {
		if strings.HasPrefix(mount.Prefix, name) && mount.Prefix != name {
			// name is a prefix of a mount point, but not the mount point itself

			// Ensure it is a directory prefix
			if name == "/" || strings.HasPrefix(mount.Prefix, name+"/") {
				return &virtualFileInfo{
					name: filepath.Base(name),
					mode: os.ModeDir | 0o755, // Virtual directories are always directories
				}, nil
			}
		}
	}

	// If not virtual, return the original error from underlying filesystem
	return nil, err
}

func (m *MountFs) Name() string {
	return "MountFs"
}

func (m *MountFs) Chmod(name string, mode os.FileMode) error {
	mount, p := m.GetMount(name)
	return mount.Chmod(p, mode)
}

func (m *MountFs) Chown(name string, uid, gid int) error {
	mount, p := m.GetMount(name)
	return mount.Chown(p, uid, gid)
}

func (m *MountFs) Chtimes(name string, atime time.Time, mtime time.Time) error {
	mount, p := m.GetMount(name)
	return mount.Chtimes(p, atime, mtime)
}

// LstatIfPossible 实现 afero.Lstater 接口（如果底层文件系统支持）
func (m *MountFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	mount, p := m.GetMount(name)
	if lstater, ok := mount.(afero.Lstater); ok {
		return lstater.LstatIfPossible(p)
	}

	info, err := mount.Stat(p)
	return info, false, err
}

// OpenFile 修改 OpenFile 方法，返回包装后的文件对象
func (m *MountFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	mount, p := m.GetMount(name)
	file, err := mount.OpenFile(p, flag, perm)
	if err != nil {
		return nil, err
	}
	// 获取文件信息以判断是否为目录
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if info.IsDir() {
		mf, err := newMountFsFile(file, m, name)
		if err != nil {
			file.Close()
			return nil, err
		}
		return mf, nil
	}

	// 普通文件直接返回
	return file, nil
}

func (m *MountFs) Open(name string) (afero.File, error) {
	name = NormalizePath(name)

	// Check if 'name' is a virtual directory
	info, err := m.Stat(name)
	if err == nil && info.IsDir() {
		_, isVirtual := info.(*virtualFileInfo)
		if isVirtual {
			// If it's a virtual directory, create an in-memory FS to represent it
			memFs := afero.NewMemMapFs()
			// We need a file handle to pass to newMountFsFile, so open the root of this temporary FS
			virtualFile, err := memFs.OpenFile("/", os.O_RDONLY, 0)
			if err != nil {
				return nil, err
			}
			mf, err := newMountFsFile(virtualFile, m, name)
			if err != nil {
				virtualFile.Close()
				return nil, err
			}
			return mf, nil
		}
	}

	// If not a virtual directory or Stat failed, proceed with normal OpenFile logic
	return m.OpenFile(name, os.O_RDONLY, 0)
}

// SymlinkIfPossible 实现 afero.Linker 接口（如果底层文件系统支持）
func (m *MountFs) SymlinkIfPossible(oldname, newname string) error {
	oldFs, oldPath := m.GetMount(oldname)
	newFs, newPath := m.GetMount(newname)

	if oldFs != newFs {
		return &os.LinkError{
			Op:  "symlink",
			Old: oldname,
			New: newname,
			Err: fs.ErrInvalid,
		}
	}

	if linker, ok := oldFs.(afero.Linker); ok {
		return linker.SymlinkIfPossible(oldPath, newPath)
	}

	return &os.LinkError{
		Op:  "symlink",
		Old: oldname,
		New: newname,
		Err: afero.ErrNoSymlink,
	}
}

func (m *MountFs) ReadlinkIfPossible(name string) (string, error) {
	mount, p := m.GetMount(name)
	if linker, ok := mount.(afero.LinkReader); ok {
		return linker.ReadlinkIfPossible(p)
	}
	return "", &os.PathError{Op: "readlink", Path: name, Err: afero.ErrNoReadlink}
}

// 辅助方法

// ListMounts 列出所有挂载点
func (m *MountFs) ListMounts() []Mount {
	m.mu.RLock()
	defer m.mu.RUnlock()

	mounts := make([]Mount, len(m.mounts))
	copy(mounts, m.mounts)
	return mounts
}

// GetMountInfo 获取指定路径的挂载信息
func (m *MountFs) GetMountInfo(name string) (string, afero.Fs, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	name = NormalizePath(name)
	for _, mount := range m.mounts {
		if name == mount.Prefix || strings.HasPrefix(name, mount.Prefix+"/") {
			relPath := strings.TrimPrefix(name, mount.Prefix)
			if relPath == "" {
				relPath = "/"
			}
			return mount.Prefix, mount.Fs, relPath
		}
	}
	return "/", m.defaultFs, name
}

// directDir 获取目录的挂载信息
func (m *MountFs) directDir(dir string) (Mount, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	dir = NormalizePath(dir)
	for _, mount := range m.mounts {
		if mount.Prefix == dir {
			return mount, true
		}
	}
	return Mount{}, false
}

func (m *MountFs) hasChildMount(dir string) bool {
	dir = NormalizePath(dir) + "/"
	for _, mount := range m.mounts {
		if strings.HasPrefix(mount.Prefix, dir) {
			return true
		}
	}
	return false
}

func (m *MountFs) getMountsUnder(dir string) []Mount {
	m.mu.RLock()
	defer m.mu.RUnlock()

	dir = NormalizePath(dir)
	var result []Mount

	for _, mount := range m.mounts {
		// 挂载点自身不能作为其子挂载点
		if mount.Prefix == dir {
			continue
		}

		// 检查挂载点是否以当前目录为前缀
		// 必须确保是真正的子目录 (e.g. /a vs /ab)
		if dir == "/" {
			if strings.HasPrefix(mount.Prefix, "/") {
				result = append(result, mount)
			}
		} else {
			if strings.HasPrefix(mount.Prefix, dir+"/") {
				result = append(result, mount)
			}
		}
	}
	return result
}
