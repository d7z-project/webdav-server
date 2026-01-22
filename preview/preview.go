package preview

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"code.d7z.net/packages/webdav-server/assets"
	"code.d7z.net/packages/webdav-server/common"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
)

func WithPreview(ctx *common.FsContext) func(r chi.Router) {
	return func(r chi.Router) {
		r.Route("/", func(r chi.Router) {
			r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
				fs, err := ctx.LoadFS(r, true)
				if err != nil {
					if errors.Is(err, common.NoAuthorizedError) {
						w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
						http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
						return
					}
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				p := strings.TrimPrefix(r.URL.Path, "/preview/")
				stat, err := fs.Stat(p)
				if err != nil {
					http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
					return
				}
				if stat.IsDir() {
					dir, err := afero.ReadDir(fs, p)
					if err != nil {
						http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
						return
					}
					slices.SortFunc(dir, func(a, b os.FileInfo) int {
						if a.IsDir() == b.IsDir() {
							return strings.Compare(a.Name(), b.Name())
						} else if a.IsDir() {
							return -1
						}
						return 1
					})
					w.Header().Set("Content-Type", "text/html; charset=utf-8")
					_ = assets.ZPreview.Execute(w, map[string]interface{}{
						"Path": p,
						"User": fs.User,
						"Dirs": dir,
					})
				} else {
					file, err := fs.OpenFile(p, os.O_RDONLY, os.ModePerm)
					if err != nil {
						http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
						slog.Warn("open file err", "err", err)
						return
					}
					defer file.Close()
					http.ServeContent(w, r, file.Name(), stat.ModTime(), file)
				}
			})
			r.Post("/*", func(w http.ResponseWriter, r *http.Request) {
				p := strings.TrimPrefix(r.URL.Path, "/preview")
				fs, err := ctx.LoadFS(r, false)
				if err != nil {
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
					return
				}
				r.Body = http.MaxBytesReader(w, r.Body, int64(ctx.Config.Preview.MaxUploadSize))
				if err = r.ParseMultipartForm(10 << 20); err != nil {
					http.Error(w, "文件过大或解析错误", http.StatusRequestEntityTooLarge)
					return
				}

				override := r.FormValue("force") == "true"
				file, handler, err := r.FormFile("file")
				if err != nil {
					http.Error(w, "获取文件失败", http.StatusInternalServerError)
					return
				}
				defer file.Close()
				destPath := filepath.Join(p, handler.Filename)
				stat, err := fs.Stat(destPath)
				if err == nil {
					if stat.IsDir() {
						http.Error(w, "目录无法上传内容", http.StatusBadRequest)
						return
					}
					if !override {
						http.Error(w, "目录无法上传内容", http.StatusBadRequest)
						return
					}
				}
				destFile, err := fs.OpenFile(filepath.Join(destPath), os.O_WRONLY|os.O_CREATE, os.ModePerm)
				if err != nil {
					http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
				}
				defer destFile.Close()
				_, err = io.Copy(destFile, file)
			})
		})
	}
}
