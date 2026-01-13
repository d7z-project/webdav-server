package lockedfs

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/spf13/afero"
)

type LockedFs struct {
	fs afero.Fs
	mu sync.RWMutex
}

func NewLockedFs(baseFs afero.Fs) *LockedFs {
	return &LockedFs{
		fs: baseFs,
	}
}

func (lfs *LockedFs) Create(name string) (afero.File, error) {
	lfs.mu.Lock()
	defer lfs.mu.Unlock()

	dir := filepath.Dir(name)
	if dir != "." && dir != "/" {
		if err := lfs.fs.MkdirAll(dir, 0755); err != nil {
			return nil, err
		}
	}

	file, err := lfs.fs.Create(name)
	if err != nil {
		return nil, err
	}
	return NewLockedFile(file), nil
}

func (lfs *LockedFs) Mkdir(name string, perm os.FileMode) error {
	lfs.mu.Lock()
	defer lfs.mu.Unlock()
	return lfs.fs.Mkdir(name, perm)
}

func (lfs *LockedFs) MkdirAll(path string, perm os.FileMode) error {
	lfs.mu.Lock()
	defer lfs.mu.Unlock()
	return lfs.fs.MkdirAll(path, perm)
}

func (lfs *LockedFs) Chown(name string, uid, gid int) error {
	lfs.mu.Lock()
	defer lfs.mu.Unlock()
	return lfs.fs.Chown(name, uid, gid)
}

func (lfs *LockedFs) Open(name string) (afero.File, error) {
	lfs.mu.RLock()
	defer lfs.mu.RUnlock()

	file, err := lfs.fs.Open(name)
	if err != nil {
		return nil, err
	}
	return NewLockedFile(file), nil
}

func (lfs *LockedFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if flag&os.O_RDWR != 0 || flag&os.O_WRONLY != 0 || flag&os.O_CREATE != 0 || flag&os.O_APPEND != 0 || flag&os.O_TRUNC != 0 {
		lfs.mu.Lock()
		defer lfs.mu.Unlock()
	} else {
		lfs.mu.RLock()
		defer lfs.mu.RUnlock()
	}

	file, err := lfs.fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	return NewLockedFile(file), nil
}

func (lfs *LockedFs) Remove(name string) error {
	lfs.mu.Lock()
	defer lfs.mu.Unlock()
	return lfs.fs.Remove(name)
}

func (lfs *LockedFs) RemoveAll(path string) error {
	lfs.mu.Lock()
	defer lfs.mu.Unlock()
	return lfs.fs.RemoveAll(path)
}

func (lfs *LockedFs) Rename(oldname, newname string) error {
	lfs.mu.Lock()
	defer lfs.mu.Unlock()
	return lfs.fs.Rename(oldname, newname)
}

func (lfs *LockedFs) Stat(name string) (os.FileInfo, error) {
	lfs.mu.RLock()
	defer lfs.mu.RUnlock()
	return lfs.fs.Stat(name)
}

func (lfs *LockedFs) Name() string {
	lfs.mu.RLock()
	defer lfs.mu.RUnlock()
	return lfs.fs.Name()
}

func (lfs *LockedFs) Chmod(name string, mode os.FileMode) error {
	lfs.mu.Lock()
	defer lfs.mu.Unlock()
	return lfs.fs.Chmod(name, mode)
}

func (lfs *LockedFs) Chtimes(name string, atime time.Time, mtime time.Time) error {
	lfs.mu.Lock()
	defer lfs.mu.Unlock()
	return lfs.fs.Chtimes(name, atime, mtime)
}

func (lfs *LockedFs) WithReadLock(fn func(afero.Fs) error) error {
	lfs.mu.RLock()
	defer lfs.mu.RUnlock()
	return fn(lfs.fs)
}

func (lfs *LockedFs) WithWriteLock(fn func(afero.Fs) error) error {
	lfs.mu.Lock()
	defer lfs.mu.Unlock()
	return fn(lfs.fs)
}

func (lfs *LockedFs) GetUnderlyingFs() afero.Fs {
	return lfs.fs
}

func (lfs *LockedFs) LockFile(filename string, fn func(afero.File) error) error {
	file, err := lfs.OpenFile(filename, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	if lf, ok := file.(*LockedFile); ok {
		lf.mu.Lock()
		defer lf.mu.Unlock()
		return fn(lf.file)
	}

	return fn(file)
}

func (lfs *LockedFs) ReadLockFile(filename string, fn func(afero.File) error) error {
	file, err := lfs.OpenFile(filename, os.O_RDONLY, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	if lf, ok := file.(*LockedFile); ok {
		lf.mu.RLock()
		defer lf.mu.RUnlock()
		return fn(lf.file)
	}

	return fn(file)
}
