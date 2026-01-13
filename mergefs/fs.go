package mergefs

import (
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
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
func (m *MountFs) Mount(prefix string, fs afero.Fs) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 标准化前缀
	prefix = strings.TrimSuffix(prefix, "/")
	if prefix == "" {
		prefix = "/"
	}

	// 移除已存在的相同前缀挂载
	for i, mount := range m.mounts {
		if mount.Prefix == prefix {
			m.mounts[i].Fs = fs
			return
		}
	}

	// 按前缀长度降序排列，确保最长匹配优先
	m.mounts = append(m.mounts, Mount{Prefix: prefix, Fs: fs})
	m.sortMounts()
}

// Unmount 移除挂载点
func (m *MountFs) Unmount(prefix string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	prefix = strings.TrimSuffix(prefix, "/")
	for i, mount := range m.mounts {
		if mount.Prefix == prefix {
			m.mounts = append(m.mounts[:i], m.mounts[i+1:]...)
			return true
		}
	}
	return false
}

// GetMount 获取指定路径对应的挂载点和相对路径
func (m *MountFs) GetMount(name string) (afero.Fs, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// 清理路径
	name = cleanPath(name)

	// 查找匹配的挂载点
	for _, mount := range m.mounts {
		if mount.Prefix == "/" {
			// 根挂载点匹配所有路径
			return mount.Fs, name
		}

		if name == mount.Prefix || strings.HasPrefix(name, mount.Prefix+"/") {
			relPath := strings.TrimPrefix(name, mount.Prefix)
			if relPath == "" {
				relPath = "/"
			} else if !strings.HasPrefix(relPath, "/") {
				relPath = "/" + relPath
			}
			return mount.Fs, relPath
		}
	}

	// 没有匹配的挂载点，使用默认文件系统
	return m.defaultFs, name
}

// sortMounts 按前缀长度降序排列挂载点
func (m *MountFs) sortMounts() {
	sort.SliceStable(m.mounts, func(i, j int) bool {
		return len(m.mounts[i].Prefix) > len(m.mounts[j].Prefix)
	})
}

// cleanPath 清理路径
func cleanPath(p string) string {
	p = filepath.ToSlash(p)
	p = path.Clean(p)
	if p == "." {
		p = "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

func (m *MountFs) Create(name string) (afero.File, error) {
	mount, p := m.GetMount(name)
	return mount.Create(p)
}

func (m *MountFs) Mkdir(name string, perm os.FileMode) error {
	mount, p := m.GetMount(name)
	return mount.Mkdir(p, perm)
}

func (m *MountFs) MkdirAll(path string, perm os.FileMode) error {
	mount, relPath := m.GetMount(path)
	return mount.MkdirAll(relPath, perm)
}

func (m *MountFs) Open(name string) (afero.File, error) {
	mount, p := m.GetMount(name)
	return mount.Open(p)
}

func (m *MountFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	mount, p := m.GetMount(name)
	return mount.OpenFile(p, flag, perm)
}

func (m *MountFs) Remove(name string) error {
	mount, p := m.GetMount(name)
	return mount.Remove(p)
}

func (m *MountFs) RemoveAll(path string) error {
	mount, relPath := m.GetMount(path)
	return mount.RemoveAll(relPath)
}

func (m *MountFs) Rename(oldname, newname string) error {
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
	err = dstFs.Chmod(dst, srcInfo.Mode())
	if err != nil {
		_ = dstFs.Remove(dst)
		return err
	}
	return srcFs.Remove(src)
}

func (m *MountFs) crossRenameDir(srcFs afero.Fs, src string, dstFs afero.Fs, dst string) error {
	// 创建目标目录
	err := dstFs.MkdirAll(dst, 0755)
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
			err = m.crossRename(srcFs, srcPath, dstFs, dstPath)
		}

		if err != nil {
			return err
		}
	}
	return srcFs.RemoveAll(src)
}

func (m *MountFs) Stat(name string) (os.FileInfo, error) {
	mount, p := m.GetMount(name)
	return mount.Stat(p)
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

// 实现 afero.Lstater 接口（如果底层文件系统支持）
func (m *MountFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	mount, p := m.GetMount(name)
	if lstater, ok := mount.(afero.Lstater); ok {
		return lstater.LstatIfPossible(p)
	}

	info, err := mount.Stat(p)
	return info, false, err
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

	name = cleanPath(name)
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
