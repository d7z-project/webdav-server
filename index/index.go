package index

import (
	"net/http"

	"code.d7z.net/packages/webdav-server/assets"
	"code.d7z.net/packages/webdav-server/common"
	"github.com/go-chi/chi/v5"
)

func WithIndex(ctx *common.FsContext, route *chi.Mux) {
	route.Get("/", func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Query().Get("login") != "" {
			if _, _, ok := request.BasicAuth(); !ok {
				writer.Header().Add("WWW-Authenticate", `Basic realm="Webdav Server"`)
				writer.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		writer.Header().Add("Content-Type", "text/html; charset=utf-8")
		_ = assets.ZIndex.Execute(writer, map[string]interface{}{
			"Config": ctx.Config,
		})
	})
}
