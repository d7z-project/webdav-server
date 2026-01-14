package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"code.d7z.net/packages/webdav-server/mergefs"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/gobwas/glob"
	"github.com/goccy/go-yaml"
	"github.com/spf13/afero"
	"golang.org/x/net/webdav"
)

var (
	config = "./config.yml"
	debug  bool
)

func init() {
	chi.RegisterMethod("PROPFIND")
	chi.RegisterMethod("PROPPATCH")
	chi.RegisterMethod("MKOL")
	chi.RegisterMethod("COPY")
	chi.RegisterMethod("MOVE")
	chi.RegisterMethod("LOCK")
	chi.RegisterMethod("UNLOCK")
	flag.StringVar(&config, "config", config, "config file")
	flag.BoolVar(&debug, "debug", debug, "debug mode")
	flag.Parse()
	if debug {
		slog.SetLogLoggerLevel(slog.LevelDebug)
	} else {
		slog.SetLogLoggerLevel(slog.LevelWarn)
	}
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("load config err", "err", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer cancel()
		sig := make(chan os.Signal, 1)
		defer close(sig)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
	}()
	osFs := afero.NewOsFs()
	baseFs := afero.NewMemMapFs()
	_ = afero.WriteFile(baseFs, "/index.txt", []byte("hello world"), os.ModePerm)
	rootFs := mergefs.NewMountFs(afero.NewReadOnlyFs(baseFs))
	for s, pool := range cfg.Pools {
		rootFs.Mount(fmt.Sprintf("/%s", s), afero.NewBasePathFs(osFs, pool.Path))
	}
	route := chi.NewMux()
	route.Use(middleware.RequestID)
	route.Use(middleware.RealIP)
	route.Use(middleware.Logger)
	route.Use(middleware.Recoverer)
	route.Use(middleware.Timeout(60 * time.Second))
	webdavHandler := &webdav.Handler{
		Prefix:     cfg.Webdav.Prefix,
		FileSystem: NewWebdavFS(rootFs),
		LockSystem: webdav.NewMemLS(),
	}
	if cfg.Webdav.Enabled {
		slog.Info("webdav enabled")
		route.Route(cfg.Webdav.Prefix, func(r chi.Router) {
			r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
				webdavHandler.ServeHTTP(w, r)
			})
		})
	}

	route.Route("/preview", func(r chi.Router) {
		r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
			p := strings.TrimPrefix(r.URL.Path, "/preview/")
			stat, err := rootFs.Stat(p)
			if err != nil {
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				return
			}
			if stat.IsDir() {
				dir, err := afero.ReadDir(rootFs, p)
				if err != nil {
					http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
					return
				}
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				for _, info := range dir {
					_, _ = w.Write([]byte(fmt.Sprintf("<a href=\"%s\">%s</a>\n", p, info.Name())))
				}
			} else {
				file, err := rootFs.OpenFile(p, os.O_WRONLY, os.ModePerm)
				if err != nil {
					http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
					slog.Warn("open file err", "err", err)
					return
				}
				defer file.Close()
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", p))
				w.Header().Set("Content-Transfer-Encoding", "binary")
				_, _ = io.Copy(w, file)
			}
		})
	})

	listen, err := net.Listen("tcp", cfg.Bind)
	if err != nil {
		slog.Error("listen err", "err", err)
		os.Exit(1)
	}
	server := http.Server{
		Addr:    cfg.Bind,
		Handler: route,
	}
	go func() {
		if err := server.Serve(listen); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve err", "err", err)
		}
	}()
	go func() {

	}()
	<-ctx.Done()
	timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(timeout); err != nil {
		slog.Error("shutdown err", "err", err)
		os.Exit(1)
	}
}

func loadConfig() (*Config, error) {
	var nameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	data, err := os.ReadFile(config)
	if err != nil {
		return nil, err
	}
	var result Config
	if err = yaml.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if result.Bind == "" {
		return nil, errors.New("bind is required")
	}
	if result.Pools == nil || len(result.Pools) == 0 {
		return nil, errors.New("pools is required")
	}
	for name, user := range result.Users {
		if !nameRegexp.MatchString(name) {
			return nil, fmt.Errorf("invalid user name: %s", name)
		}
		if user.Password == "" && user.PublicKey == "" {
			slog.Warn("password or public key is not defined.", "user", name)
		}
	}

	for s, pool := range result.Pools {
		if !nameRegexp.MatchString(s) {
			return nil, fmt.Errorf("invalid pool name: %s", s)
		}
		if pool.Path == "" {
			return nil, fmt.Errorf("invalid pool path: %s", s)
		}
		if stat, err := os.Stat(pool.Path); err != nil || !stat.IsDir() {
			return nil, fmt.Errorf("invalid pool path %s: not exists or not dir", s)
		}
		if len(pool.Permissions) == 0 && !pool.DefaultPerm.IsRead() {
			slog.Warn("pool cannot be operated by any user.", "pool", s)
		}
		for name, permissions := range pool.Permissions {
			if !nameRegexp.MatchString(name) {
				return nil, fmt.Errorf("invalid pool name: %s", name)
			}
			if _, ok := result.Users[name]; !ok && result.LDAP == nil {
				slog.Warn("the user does not exist", "user", name)
			}
			for _, permission := range permissions {
				if permission.Prefix == "" {
					return nil, fmt.Errorf("invalid permission prefix: %s", permission.Prefix)
				}
				if _, err := glob.Compile(permission.Prefix); err != nil {
					return nil, fmt.Errorf("invalid permission prefix %s: %s", permission.Prefix, err)
				}
			}
		}
	}
	if result.Webdav.Enabled {
		result.Webdav.Prefix = "/" + strings.TrimSpace(strings.Trim(result.Webdav.Prefix, "/"))
		if result.Webdav.Prefix == "/" {
			return nil, errors.New("webdav not support prefix '/' or empty")
		}
	}
	return &result, nil
}
