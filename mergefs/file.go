package mergefs

import (
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/spf13/afero"
)

type mountFsFile struct {
	afero.File
	fs      *MountFs
	path    string
	entries []fs.DirEntry // 缓存的目录条目
	offset  int           // 读取偏移
}

// newMountFsFile 创建新的 mountFsFile
func newMountFsFile(file afero.File, fs *MountFs, path string) *mountFsFile {
	return &mountFsFile{
		File: file,
		fs:   fs,
		path: cleanPath(path),
	}
}

// Readdir 读取目录内容，包括挂载点
func (f *mountFsFile) Readdir(count int) ([]os.FileInfo, error) {
	if f.entries == nil {
		if err := f.collectEntries(); err != nil {
			return nil, err
		}
	}

	// 根据 count 计算要返回的条目
	var result []os.FileInfo
	remaining := len(f.entries) - f.offset

	if count <= 0 {
		// count <= 0 时返回所有剩余条目
		result = make([]os.FileInfo, remaining)
		for i := 0; i < remaining; i++ {
			info, err := f.entries[f.offset+i].Info()
			if err != nil {
				return nil, err
			}
			result[i] = info
		}
		f.offset = len(f.entries)
		return result, nil
	}

	// 返回最多 count 个条目
	toRead := count
	if remaining < toRead {
		toRead = remaining
	}

	result = make([]os.FileInfo, toRead)
	for i := 0; i < toRead; i++ {
		info, err := f.entries[f.offset+i].Info()
		if err != nil {
			return nil, err
		}
		result[i] = info
	}
	f.offset += toRead

	if toRead == 0 {
		return nil, io.EOF
	}

	return result, nil
}

// Readdirnames 读取目录名称，包括挂载点
func (f *mountFsFile) Readdirnames(count int) ([]string, error) {
	// 如果还没有缓存条目，先收集所有条目
	if f.entries == nil {
		if err := f.collectEntries(); err != nil {
			return nil, err
		}
	}

	// 根据 count 计算要返回的名称
	var result []string
	remaining := len(f.entries) - f.offset

	if count <= 0 {
		// count <= 0 时返回所有剩余名称
		result = make([]string, remaining)
		for i := 0; i < remaining; i++ {
			result[i] = f.entries[f.offset+i].Name()
		}
		f.offset = len(f.entries)
		return result, nil
	}

	// 返回最多 count 个名称
	toRead := count
	if remaining < toRead {
		toRead = remaining
	}

	result = make([]string, toRead)
	for i := 0; i < toRead; i++ {
		result[i] = f.entries[f.offset+i].Name()
	}
	f.offset += toRead

	if toRead == 0 {
		return nil, io.EOF
	}

	return result, nil
}

// collectEntries 收集目录条目，包括底层文件系统的条目和挂载点
func (f *mountFsFile) collectEntries() error {
	// 先收集底层文件系统的条目
	var entries []fs.DirEntry

	// 读取底层文件系统的所有条目
	for {
		infos, err := f.File.Readdir(100) // 分批读取
		if err != nil && err != io.EOF {
			return err
		}

		for _, info := range infos {
			entries = append(entries, &dirEntry{info})
		}

		if err == io.EOF || len(infos) == 0 {
			break
		}
	}

	// 重置底层文件的读取位置
	f.File.Seek(0, io.SeekStart)

	// 收集当前目录下的直接子挂载点
	mounts := f.fs.getDirectMountsUnder(f.path)

	// 创建挂载点条目
	for _, mount := range mounts {
		// 获取挂载点相对于当前目录的名称
		relPath := strings.TrimPrefix(mount.Prefix, f.path)
		relPath = strings.TrimPrefix(relPath, "/")

		// 取第一级目录名
		parts := strings.Split(relPath, "/")
		if len(parts) == 0 {
			continue
		}

		mountName := parts[0]

		// 检查是否已存在同名条目
		found := false
		for i, entry := range entries {
			if entry.Name() == mountName {
				// 用挂载点替换原有条目
				entries[i] = &mountDirEntry{
					name: mountName,
					mode: os.ModeDir | 0755,
				}
				found = true
				break
			}
		}

		if !found {
			// 添加新的挂载点条目
			entries = append(entries, &mountDirEntry{
				name: mountName,
				mode: os.ModeDir | 0755,
			})
		}
	}

	// 按名称排序
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	f.entries = entries
	return nil
}

// Seek 实现 Seek 方法
func (f *mountFsFile) Seek(offset int64, whence int) (int64, error) {
	// 如果是目录且已经缓存了条目，重置偏移
	if f.entries != nil {
		if whence == io.SeekStart && offset == 0 {
			f.offset = 0
			return 0, nil
		}
	}
	return f.File.Seek(offset, whence)
}

// dirEntry 实现 fs.DirEntry 接口
type dirEntry struct {
	info os.FileInfo
}

func (d *dirEntry) Name() string               { return d.info.Name() }
func (d *dirEntry) IsDir() bool                { return d.info.IsDir() }
func (d *dirEntry) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d *dirEntry) Info() (os.FileInfo, error) { return d.info, nil }

// mountDirEntry 挂载点的目录条目
type mountDirEntry struct {
	name string
	mode os.FileMode
}

func (m *mountDirEntry) Name() string               { return m.name }
func (m *mountDirEntry) IsDir() bool                { return m.mode.IsDir() }
func (m *mountDirEntry) Type() fs.FileMode          { return m.mode.Type() }
func (m *mountDirEntry) Info() (os.FileInfo, error) { return m, nil }

// 实现 os.FileInfo 接口
func (m *mountDirEntry) Size() int64        { return 0 }
func (m *mountDirEntry) Mode() os.FileMode  { return m.mode }
func (m *mountDirEntry) ModTime() time.Time { return time.Time{} }
func (m *mountDirEntry) Sys() interface{}   { return nil }

// mountFileInfo 挂载点的文件信息
type mountFileInfo struct {
	name string
	mode os.FileMode
}

func (m *mountFileInfo) Name() string       { return m.name }
func (m *mountFileInfo) Size() int64        { return 0 }
func (m *mountFileInfo) Mode() os.FileMode  { return m.mode }
func (m *mountFileInfo) ModTime() time.Time { return time.Time{} }
func (m *mountFileInfo) IsDir() bool        { return m.mode.IsDir() }
func (m *mountFileInfo) Sys() interface{}   { return nil }
