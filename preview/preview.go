package preview

import (
	"errors"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"strings"

	"code.d7z.net/packages/webdav-server/assets"
	"code.d7z.net/packages/webdav-server/common"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
)

func WithPreview(ctx *common.FsContext) func(r chi.Router) {
	return func(r chi.Router) {
		r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
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
	}
}
