package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"code.d7z.net/packages/webdav-server/assets"
	"code.d7z.net/packages/webdav-server/common"
	"code.d7z.net/packages/webdav-server/dav"
	"code.d7z.net/packages/webdav-server/preview"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

var (
	config = "./config.yml"
	debug  bool
)

func init() {
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
	cfg, err := common.LoadConfig(config)
	if err != nil {
		slog.Error("load config err", "err", err)
		os.Exit(1)
	}
	osCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		defer cancel()
		sig := make(chan os.Signal, 1)
		defer close(sig)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
	}()
	ctx, err := common.NewContext(osCtx, cfg)
	if err != nil {
		slog.Error("new context err", "err", err)
		os.Exit(1)
	}

	route := chi.NewMux()
	route.Use(middleware.RequestID)
	route.Use(middleware.RealIP)
	route.Use(middleware.Recoverer)
	if debug {
		route.Use(middleware.Logger)
	}
	route.Use(middleware.Timeout(60 * time.Second))
	if cfg.Webdav.Enabled {
		slog.Info("webdav enabled")
		route.Route(cfg.Webdav.Prefix, dav.WithWebdav(ctx))

	}
	route.Route("/preview", preview.WithPreview(ctx))
	listen, err := net.Listen("tcp", cfg.Bind)
	if err != nil {
		slog.Error("listen err", "err", err)
		os.Exit(1)
	}

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
			"Config": cfg,
		})
	})
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
	<-osCtx.Done()
	timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(timeout); err != nil {
		slog.Error("shutdown err", "err", err)
		os.Exit(1)
	}
}
