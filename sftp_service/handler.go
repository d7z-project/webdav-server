package sftp_service

import (
	"io"
	"os"
	"time"

	"github.com/pkg/sftp"
	"github.com/spf13/afero"
)

// FSHandlers 初始化 SFTP Handlers
func FSHandlers(fs afero.Fs) sftp.Handlers {
	if fs == nil {
		fs = afero.NewMemMapFs()
	}
	h := &fsHandler{fs: fs}
	return sftp.Handlers{
		FileGet:  h,
		FilePut:  h,
		FileCmd:  h,
		FileList: h,
	}
}

type fsHandler struct {
	fs afero.Fs
}

func (f *fsHandler) Filelist(request *sftp.Request) (sftp.ListerAt, error) {
	switch request.Method {
	case "List":
		entries, err := afero.ReadDir(f.fs, request.Filepath)
		if err != nil {
			return nil, err
		}
		return listerAt(entries), nil

	case "Stat":
		fi, err := f.fs.Stat(request.Filepath)
		if err != nil {
			return nil, err
		}
		return listerAt([]os.FileInfo{fi}), nil

	case "Lstat":
		if lstater, ok := f.fs.(afero.Lstater); ok {
			fi, _, err := lstater.LstatIfPossible(request.Filepath)
			if err != nil {
				return nil, err
			}
			return listerAt([]os.FileInfo{fi}), nil
		}

		fi, err := f.fs.Stat(request.Filepath)
		if err != nil {
			return nil, err
		}
		return listerAt([]os.FileInfo{fi}), nil

	case "Readlink":
		if linkReader, ok := f.fs.(afero.LinkReader); ok {
			target, err := linkReader.ReadlinkIfPossible(request.Filepath)
			if err != nil {
				return nil, err
			}
			// Readlink 需要返回包含目标路径的 FileInfo
			return listerAt([]os.FileInfo{
				&memFileInfo{name: target},
			}), nil
		}
		return nil, sftp.ErrSshFxOpUnsupported
	}

	return nil, sftp.ErrSshFxOpUnsupported
}

func (f *fsHandler) Filecmd(request *sftp.Request) error {
	switch request.Method {
	case "Setstat":
		attrs := request.Attributes()
		flags := request.AttrFlags()
		path := request.Filepath

		if flags.Size {
			file, err := f.fs.OpenFile(path, os.O_WRONLY, 0o644)
			if err != nil {
				return err
			}
			err = file.Truncate(int64(attrs.Size))
			closeErr := file.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}

		if flags.UidGid {
			if err := f.fs.Chown(path, int(attrs.UID), int(attrs.GID)); err != nil {
				return err
			}
		}

		if flags.Permissions {
			if err := f.fs.Chmod(path, attrs.FileMode()); err != nil {
				return err
			}
		}

		if flags.Acmodtime {
			atime := time.Unix(int64(attrs.Atime), 0)
			mtime := time.Unix(int64(attrs.Mtime), 0)
			if err := f.fs.Chtimes(path, atime, mtime); err != nil {
				return err
			}
		}
		return nil

	case "Rename":
		return f.fs.Rename(request.Filepath, request.Target)

	case "Rmdir":
		return f.fs.Remove(request.Filepath)

	case "Remove":
		return f.fs.Remove(request.Filepath)

	case "Mkdir":
		return f.fs.MkdirAll(request.Filepath, 0o755)

	case "Symlink":
		if linker, ok := f.fs.(afero.Symlinker); ok {
			return linker.SymlinkIfPossible(request.Target, request.Filepath)
		}
		return sftp.ErrSshFxOpUnsupported
	}

	return sftp.ErrSshFxOpUnsupported
}

func (f *fsHandler) Filewrite(request *sftp.Request) (io.WriterAt, error) {
	flag := getOpenFlag(request.Pflags())
	file, err := f.fs.OpenFile(request.Filepath, flag, 0o666)
	if err != nil {
		return nil, err
	}

	if w, ok := file.(io.WriterAt); ok {
		return w, nil
	}

	_ = file.Close()
	return nil, sftp.ErrSshFxOpUnsupported
}

func (f *fsHandler) Fileread(request *sftp.Request) (io.ReaderAt, error) {
	flag := getOpenFlag(request.Pflags())
	file, err := f.fs.OpenFile(request.Filepath, flag, 0o666)
	if err != nil {
		return nil, err
	}

	if r, ok := file.(io.ReaderAt); ok {
		return r, nil
	}

	_ = file.Close()
	return nil, sftp.ErrSshFxOpUnsupported
}

// -----------------------------------------------------------------------------
// Helper types and functions
// -----------------------------------------------------------------------------

func getOpenFlag(pflags sftp.FileOpenFlags) int {
	var flag int
	if pflags.Read && pflags.Write {
		flag = os.O_RDWR
	} else if pflags.Read {
		flag = os.O_RDONLY
	} else if pflags.Write {
		flag = os.O_WRONLY
	}

	if pflags.Append {
		flag |= os.O_APPEND
	}
	if pflags.Creat {
		flag |= os.O_CREATE
	}
	if pflags.Trunc {
		flag |= os.O_TRUNC
	}
	if pflags.Excl {
		flag |= os.O_EXCL
	}
	return flag
}

type listerAt []os.FileInfo

func (l listerAt) ListAt(ls []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(ls, l[offset:])
	if n < len(ls) {
		return n, io.EOF
	}
	return n, nil
}

type memFileInfo struct {
	name string
}

func (m *memFileInfo) Name() string       { return m.name }
func (m *memFileInfo) Size() int64        { return 0 }
func (m *memFileInfo) Mode() os.FileMode  { return os.ModeSymlink | 0o777 }
func (m *memFileInfo) ModTime() time.Time { return time.Now() }
func (m *memFileInfo) IsDir() bool        { return false }
func (m *memFileInfo) Sys() interface{}   { return nil }
