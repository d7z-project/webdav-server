package dav

import (
	"errors"
	"log/slog"
	"net/http"

	"code.d7z.net/packages/webdav-server/common"
	"github.com/go-chi/chi/v5"
	"golang.org/x/net/webdav"
)

func init() {
	chi.RegisterMethod("PROPFIND")
	chi.RegisterMethod("PROPPATCH")
	chi.RegisterMethod("MKOL")
	chi.RegisterMethod("COPY")
	chi.RegisterMethod("MOVE")
	chi.RegisterMethod("LOCK")
	chi.RegisterMethod("UNLOCK")
}

func WithWebdav(ctx *common.FsContext) func(r chi.Router) {
	locker := webdav.NewMemLS()
	return func(r chi.Router) {
		r.HandleFunc("/*", func(writer http.ResponseWriter, request *http.Request) {
			loadFS, err := ctx.LoadFS(request, false)
			if err != nil {
				slog.Debug("no authorized filesystem", "err", err.Error())
				if errors.Is(err, common.NoAuthorizedError) {
					writer.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
					http.Error(writer, err.Error(), http.StatusUnauthorized)
				} else if errors.Is(err, common.NoPermissionError) {
					http.Error(writer, err.Error(), http.StatusForbidden)
				} else {
					slog.Error("未知错误 ！", "err", err.Error())
					http.Error(writer, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				}
				return
			}
			handler := &webdav.Handler{
				Prefix:     ctx.Config.Webdav.Prefix,
				FileSystem: NewWebdavFS(loadFS),
				LockSystem: locker,
			}
			handler.ServeHTTP(writer, request)
		})
	}
}
