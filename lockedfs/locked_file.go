package lockedfs

import (
	"os"
	"sync"

	"github.com/spf13/afero"
)

type LockedFile struct {
	file afero.File
	mu   sync.RWMutex
}

func NewLockedFile(file afero.File) *LockedFile {
	return &LockedFile{
		file: file,
	}
}

func (lf *LockedFile) Name() string {
	lf.mu.RLock()
	defer lf.mu.RUnlock()
	return lf.file.Name()
}

func (lf *LockedFile) Read(p []byte) (n int, err error) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.Read(p)
}

func (lf *LockedFile) ReadAt(p []byte, off int64) (n int, err error) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.ReadAt(p, off)
}

func (lf *LockedFile) Write(p []byte) (n int, err error) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.Write(p)
}

func (lf *LockedFile) WriteAt(p []byte, off int64) (n int, err error) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.WriteAt(p, off)
}

func (lf *LockedFile) Seek(offset int64, whence int) (int64, error) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.Seek(offset, whence)
}

func (lf *LockedFile) Close() error {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.Close()
}

func (lf *LockedFile) Readdir(count int) ([]os.FileInfo, error) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.Readdir(count)
}

func (lf *LockedFile) Readdirnames(n int) ([]string, error) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.Readdirnames(n)
}

func (lf *LockedFile) Stat() (os.FileInfo, error) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.Stat()
}

func (lf *LockedFile) Sync() error {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.Sync()
}

func (lf *LockedFile) Truncate(size int64) error {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.Truncate(size)
}

func (lf *LockedFile) WriteString(s string) (ret int, err error) {
	lf.mu.Lock()
	defer lf.mu.Unlock()
	return lf.file.WriteString(s)
}

func (lf *LockedFile) GetUnderlyingFile() afero.File {
	lf.mu.RLock()
	defer lf.mu.RUnlock()
	return lf.file
}
