package index

import (
	"log/slog"
	"net/http"
	"strings"

	"code.d7z.net/packages/webdav-server/assets"
	"code.d7z.net/packages/webdav-server/common"
	"github.com/go-chi/chi/v5"
)

func WithIndex(ctx *common.FsContext, route *chi.Mux) {
	route.Get("/logout", func(writer http.ResponseWriter, request *http.Request) {
		http.SetCookie(writer, &http.Cookie{
			Name:   "webdav_session",
			Value:  "",
			Path:   "/",
			MaxAge: -1,
		})
		http.Redirect(writer, request, "/", http.StatusFound)
	})

	route.Get("/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Content-Type", "text/html; charset=utf-8")
		_ = assets.ZLogin.Execute(w, map[string]interface{}{
			"Return": r.URL.Query().Get("return"),
		})
	})

	route.Post("/login", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Invalid request", http.StatusBadRequest)
			return
		}
		username := r.FormValue("username")
		password := r.FormValue("password")
		returnUrl := r.FormValue("return")
		if returnUrl == "" {
			returnUrl = "/"
		}

		if _, err := ctx.LoadFS(username, password, nil, false); err != nil {
			w.Header().Add("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_ = assets.ZLogin.Execute(w, map[string]interface{}{
				"Error":  "用户名或密码错误",
				"Return": returnUrl,
			})
			return
		}

		// Auth successful, set cookie
		token := ctx.SignToken(username)
		isSecure := r.TLS != nil || strings.ToLower(r.Header.Get("X-Forwarded-Proto")) == "https"

		http.SetCookie(w, &http.Cookie{
			Name:     "webdav_session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			Secure:   isSecure,
			SameSite: http.SameSiteLaxMode,
			MaxAge:   86400 * 7, // 7 days
		})

		slog.Info("Login success", "user", username, "remote", r.RemoteAddr)
		http.Redirect(w, r, returnUrl, http.StatusFound)
	})

	route.Get("/", func(writer http.ResponseWriter, request *http.Request) {
		// Check for existing session
		var currentUser string
		if cookie, err := request.Cookie("webdav_session"); err == nil {
			if user, err := ctx.VerifyToken(cookie.Value); err == nil {
				currentUser = user
			}
		}

		// If login param is present, redirect to login page (legacy support or direct link)
		if request.URL.Query().Get("login") != "" {
			http.Redirect(writer, request, "/login", http.StatusFound)
			return
		}

		writer.Header().Add("Content-Type", "text/html; charset=utf-8")
		_ = assets.ZIndex.Execute(writer, map[string]interface{}{
			"Config":   ctx.Config,
			"IsLogged": currentUser != "" && currentUser != "guest",
			"User":     currentUser,
		})
	})
}
