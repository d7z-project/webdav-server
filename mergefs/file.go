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

// mountFsFile 是对 afero.File 的一个包装，专门用于处理 MountFs 中的目录。
// 它重写了 Readdir 和 Readdirnames 方法，以便在列出目录内容时，能够正确地包含挂载点。
type mountFsFile struct {
	afero.File
	fs      *MountFs // 指向其所属的 MountFs
	path    string   // 文件或目录在 MountFs 中的完整路径
	offset  int      // 用于 Readdir/Readdirnames 的读取偏移量
	entries []fs.DirEntry
}

// newMountFsFile 创建并返回一个新的 mountFsFile 实例。
func newMountFsFile(file afero.File, fs *MountFs, path string) (*mountFsFile, error) {
	f := &mountFsFile{
		File: file,
		fs:   fs,
		path: NormalizePath(path),
	}
	entries, err := f.collectEntries() // Collect entries once at creation
	if err != nil {
		return nil, err
	}
	f.entries = entries
	return f, nil
}

// Readdir 读取并返回目录中的 os.FileInfo 列表。
// 这个实现会合并来自底层文件系统的条目和在当前目录下的挂载点。
// count 指定最多返回多少个条目。如果 count <= 0，则返回所有条目。
func (f *mountFsFile) Readdir(count int) ([]os.FileInfo, error) {
	// 如果已经读完所有条目
	if f.offset >= len(f.entries) { // Use f.entries directly
		if count <= 0 {
			return []os.FileInfo{}, nil
		}
		return nil, io.EOF
	}

	start := f.offset
	end := len(f.entries) // Use f.entries directly
	if count > 0 && start+count < end {
		end = start + count
	}

	remainingEntries := f.entries[start:end] // Use f.entries directly

	infos := make([]os.FileInfo, len(remainingEntries))
	for i, entry := range remainingEntries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		infos[i] = info
	}

	f.offset = end

	if count > 0 && len(infos) == 0 {
		return nil, io.EOF
	}

	return infos, nil
}

// Readdirnames 读取并返回目录中的文件名列表。
// 实现逻辑与 Readdir 类似，但只返回名称。
func (f *mountFsFile) Readdirnames(count int) ([]string, error) {
	// 如果已经读完所有条目
	if f.offset >= len(f.entries) { // Use f.entries directly
		if count <= 0 {
			return []string{}, nil
		}
		return nil, io.EOF
	}

	start := f.offset
	end := len(f.entries) // Use f.entries directly
	if count > 0 && start+count < end {
		end = start + count
	}

	remainingEntries := f.entries[start:end] // Use f.entries directly

	names := make([]string, len(remainingEntries))
	for i, entry := range remainingEntries {
		names[i] = entry.Name()
	}

	f.offset = end

	if count > 0 && len(names) == 0 {
		return nil, io.EOF
	}

	return names, nil
}

func (f *mountFsFile) getEntries() ([]fs.DirEntry, error) {
	// Entries are now populated once at creation.
	// This method is no longer needed for lazy loading, but keeping it for consistency if other parts need it.
	return f.entries, nil
}

// collectEntries 负责从底层文件系统收集目录条目，并将其与当前路径下的挂载点合并。
// 返回的条目列表按名称排序。
func (f *mountFsFile) collectEntries() ([]fs.DirEntry, error) {
	// 1. 从底层文件系统读取所有条目
	rawInfos, err := f.File.Readdir(-1)
	if err != nil {
		return nil, err
	}

	entryMap := make(map[string]fs.DirEntry)
	for _, info := range rawInfos {
		entryMap[info.Name()] = &dirEntry{info}
	}

	// 2. 收集当前目录下的所有相关挂载点（包括深层挂载点，用于构建虚拟目录）
	mounts := f.fs.getMountsUnder(f.path)

	// 3. 处理挂载点和虚拟目录
	for _, mount := range mounts {
		// 获取挂载点相对于当前目录的名称
		relPath := strings.TrimPrefix(mount.Prefix, f.path)
		relPath = strings.TrimPrefix(relPath, "/")
		parts := strings.Split(relPath, "/")
		if len(parts) == 0 {
			continue
		}
		name := parts[0]

		isDirectMount := len(parts) == 1

		_, exists := entryMap[name]

		if isDirectMount {
			// 直接挂载点优先级最高，总是覆盖
			entryMap[name] = &mountDirEntry{
				name:  name,
				mode:  os.ModeDir | 0o755,
				mount: &mount,
			}
		} else if !exists {
			// 虚拟目录，仅当不存在时添加
			entryMap[name] = &dirEntry{info: &virtualFileInfo{
				name: name,
				mode: os.ModeDir | 0o755,
			}}
		}
	}

	// 4. 将 map 转换为切片并排序
	var entries []fs.DirEntry
	for _, entry := range entryMap {
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	return entries, nil
}

// Seek 实现了 io.Seeker 接口。
// 主要用于在调用 Readdir/Readdirnames 之前重置内部偏移量。
func (f *mountFsFile) Seek(offset int64, whence int) (int64, error) {
	// 如果是 seek 到文件开头，则重置 readdir 的偏移量
	if whence == io.SeekStart && offset == 0 {
		f.offset = 0
		// 对于目录，底层的 Seek 可能不支持，但我们自己的偏移量需要重置
	}
	// 将 seek 操作传递给底层的文件对象
	return f.File.Seek(offset, whence)
}

// virtualFileInfo 代表一个虚拟目录的 os.FileInfo。
// 它用于表示挂载点路径中的中间目录，这些目录在底层文件系统中可能不存在。
type virtualFileInfo struct {
	name string
	mode os.FileMode
}

func (v *virtualFileInfo) Name() string       { return v.name }
func (v *virtualFileInfo) Size() int64        { return 0 }
func (v *virtualFileInfo) Mode() os.FileMode  { return v.mode }
func (v *virtualFileInfo) ModTime() time.Time { return time.Time{} } // 虚拟目录没有实际修改时间
func (v *virtualFileInfo) IsDir() bool        { return v.mode.IsDir() }
func (v *virtualFileInfo) Sys() interface{}   { return nil }

// dirEntry 是一个简单的 os.FileInfo 到 fs.DirEntry 的适配器。
type dirEntry struct {
	info os.FileInfo
}

func (d *dirEntry) Name() string               { return d.info.Name() }
func (d *dirEntry) IsDir() bool                { return d.info.IsDir() }
func (d *dirEntry) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d *dirEntry) Info() (os.FileInfo, error) { return d.info, nil }

// mountDirEntry 代表一个挂载点，它同时实现了 fs.DirEntry 和 os.FileInfo 接口。
// 这使得挂载点可以像普通目录一样出现在 Readdir 的结果中。
type mountDirEntry struct {
	name  string
	mode  os.FileMode
	mount *Mount
}

func (m *mountDirEntry) Name() string               { return m.name }
func (m *mountDirEntry) IsDir() bool                { return m.mode.IsDir() }
func (m *mountDirEntry) Type() fs.FileMode          { return m.mode.Type() }
func (m *mountDirEntry) Info() (os.FileInfo, error) { return m, nil }
func (m *mountDirEntry) Size() int64                { return 0 } // 挂载点目录大小通常为 0 或 4096，这里简化为 0
func (m *mountDirEntry) Mode() os.FileMode          { return m.mode }

func (m *mountDirEntry) ModTime() time.Time {
	// 尝试获取挂载的根文件系统 "/" 的修改时间
	if m.mount != nil {
		if info, err := m.mount.Fs.Stat("/"); err == nil {
			return info.ModTime()
		}
	}
	return time.Time{} // 返回零时
}
func (m *mountDirEntry) Sys() interface{} { return nil }

// mountFileInfo 代表一个挂载点目录的 os.FileInfo。
// 当直接 Stat 一个挂载点路径时，会返回这个类型。
type mountFileInfo struct {
	name  string
	mode  os.FileMode
	mount *Mount
}

func (m *mountFileInfo) Name() string { return m.name }
func (m *mountFileInfo) Size() int64  { return 0 } // 同样，大小简化为 0
func (m *mountFileInfo) Mode() os.FileMode {
	// 尝试获取挂载的根文件系统 "/" 的模式
	if m.mount != nil {
		if info, err := m.mount.Fs.Stat("/"); err == nil {
			return info.Mode()
		}
	}
	return m.mode
}

func (m *mountFileInfo) ModTime() time.Time {
	// 尝试获取挂载的根文件系统 "/" 的修改时间
	if m.mount != nil {
		if info, err := m.mount.Fs.Stat("/"); err == nil {
			return info.ModTime()
		}
	}
	return time.Time{}
}
func (m *mountFileInfo) IsDir() bool      { return m.mode.IsDir() }
func (m *mountFileInfo) Sys() interface{} { return nil }
