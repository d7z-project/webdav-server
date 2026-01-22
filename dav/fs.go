package dav

import (
	"context"
	"os"

	"github.com/spf13/afero"
	"golang.org/x/net/webdav"
)

type WebdavFS struct {
	afero.Fs
}

func NewWebdavFS(fs afero.Fs) *WebdavFS {
	return &WebdavFS{fs}
}

func (w *WebdavFS) Mkdir(_ context.Context, name string, perm os.FileMode) error {
	return w.Fs.Mkdir(name, perm)
}

func (w *WebdavFS) OpenFile(_ context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	return w.Fs.OpenFile(name, flag, perm)
}

func (w *WebdavFS) RemoveAll(_ context.Context, name string) error {
	return w.Fs.RemoveAll(name)
}

func (w *WebdavFS) Rename(_ context.Context, oldName, newName string) error {
	return w.Fs.Rename(oldName, newName)
}

func (w *WebdavFS) Stat(_ context.Context, name string) (os.FileInfo, error) {
	return w.Fs.Stat(name)
}
