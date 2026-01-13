package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"github.com/gobwas/glob"
	"github.com/spf13/afero"
	"golang.org/x/net/webdav"
	"gopkg.in/yaml.v3"
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
	cfg, err := loadConfig()
	if err != nil {
		slog.Error("load config err", "err", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	webdav.Handler{
		Prefix:     cfg.Webdav.Prefix,
		FileSystem: nil,
		LockSystem: nil,
		Logger:     nil,
	}
	go func() {
		defer cancel()
		sig := make(chan os.Signal, 1)
		defer close(sig)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
	}()

	<-ctx.Done()
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
	return &result, nil
}
