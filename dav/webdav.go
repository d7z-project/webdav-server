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
	chi.RegisterMethod("SEARCH")
	chi.RegisterMethod("REPORT")
	chi.RegisterMethod("PROPFIND")
	chi.RegisterMethod("PROPPATCH")
	chi.RegisterMethod("MKOL")
	chi.RegisterMethod("MKCOL")
	chi.RegisterMethod("COPY")
	chi.RegisterMethod("MOVE")
	chi.RegisterMethod("LOCK")
	chi.RegisterMethod("UNLOCK")
}

func WithWebdav(ctx *common.FsContext) func(r chi.Router) {
	locker := webdav.NewMemLS()
	return func(r chi.Router) {
		r.HandleFunc("/*", func(writer http.ResponseWriter, request *http.Request) {
			loadFS, err := ctx.LoadWebFS(request, false)
			if err != nil {
				username, _, _ := request.BasicAuth()
				if username == "" {
					username = "guest"
				}
				slog.Warn("|security| Login failed.", "source", "webdav", "remote", request.RemoteAddr, "user", username, "err", err.Error())
				if errors.Is(err, common.NoAuthorizedError) {
					writer.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
					http.Error(writer, err.Error(), http.StatusUnauthorized)
				} else if errors.Is(err, common.NoPermissionError) {
					if username == "guest" {
						writer.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
						http.Error(writer, err.Error(), http.StatusUnauthorized)
					} else {
						http.Error(writer, err.Error(), http.StatusForbidden)
					}
				} else {
					slog.Error("未知错误 ！", "err", err.Error())
					http.Error(writer, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				}
				return
			}
			slog.Info("|webdav| Request.", "method", request.Method, "path", request.URL.Path, "remote", request.RemoteAddr, "user", loadFS.User)
			handler := &webdav.Handler{
				Prefix:     ctx.Config.Webdav.Prefix,
				FileSystem: NewWebdavFS(loadFS),
				LockSystem: locker,
			}
			handler.ServeHTTP(writer, request)
		})
	}
}
