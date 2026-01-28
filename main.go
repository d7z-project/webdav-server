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
	"code.d7z.net/packages/webdav-server/index"
	"code.d7z.net/packages/webdav-server/preview"
	"code.d7z.net/packages/webdav-server/sftp_service"
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
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sig)
		defer close(sig)
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

	// Static files
	route.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(assets.StaticFS))))

	if cfg.Webdav.Enabled {
		slog.Info("webdav enabled")
		route.Route(cfg.Webdav.Prefix, dav.WithWebdav(ctx))
	}
	route.Route("/preview", preview.WithPreview(ctx))
	index.WithIndex(ctx, route)

	httpListen, err := net.Listen("tcp", cfg.Bind)
	if err != nil {
		slog.Error("listen http err", "err", err)
		os.Exit(1)
	}
	var sftpListen net.Listener
	var sftpServer *sftp_service.SFTPServer
	if cfg.SFTP.Enabled {
		sftpServer, err = sftp_service.NewSFTPServer(ctx)
		if err != nil {
			slog.Error("sftp init err", "err", err)
			os.Exit(1)
		}
		sftpListen, err = net.Listen("tcp", cfg.SFTP.Bind)
		if err != nil {
			slog.Error("listen sftp err", "err", err)
			os.Exit(1)
		}

	}
	server := http.Server{
		Addr:    cfg.Bind,
		Handler: route,
	}
	go func() {
		if err := server.Serve(httpListen); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("serve err", "err", err)
		}
	}()
	go func() {
		if sftpServer != nil && sftpListen != nil {
			slog.Info("sftp enabled", "addr", cfg.SFTP.Bind)
			sftpServer.Serve(ctx, sftpListen)
		}
	}()
	<-osCtx.Done()
	timeout, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(timeout); err != nil {
		slog.Error("shutdown err", "err", err)
		os.Exit(1)
	}
}
