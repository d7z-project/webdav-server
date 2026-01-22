package common

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/pkg/errors"

	"code.d7z.net/packages/webdav-server/mergefs"
	"github.com/spf13/afero"
)

var (
	NoAuthorizedError = errors.New("no authorized")
	NoPermissionError = errors.New("no permission")
)

type FsContext struct {
	ctx    context.Context
	Config *Config
	users  map[string]afero.Fs
}

func (c *FsContext) Context() context.Context {
	return c.ctx
}

func NewContext(ctx context.Context, cfg *Config) (*FsContext, error) {
	f := &FsContext{
		ctx:    ctx,
		Config: cfg,
		users:  make(map[string]afero.Fs),
	}
	pools := make(map[string]afero.Fs)
	osFs := afero.NewOsFs()

	for s, pool := range cfg.Pools {
		pools[s] = afero.NewBasePathFs(osFs, pool.Path)
	}
	for userName := range cfg.Users {
		baseFS := afero.NewMemMapFs()
		rootFs := mergefs.NewMountFs(afero.NewReadOnlyFs(baseFS))
		_ = afero.WriteFile(baseFS, "/README.txt", []byte(fmt.Sprintf("欢迎你,%s", userName)), os.ModePerm)
		for poolName, poolFS := range pools {
			perm, ok := cfg.Pools[poolName].Permissions[userName]
			if !ok {
				perm = cfg.Pools[poolName].DefaultPerm
			}
			if !perm.IsRead() {
				continue
			}
			distFS := poolFS
			if !perm.IsWrite() {
				distFS = afero.NewReadOnlyFs(distFS)
			}
			if err := rootFs.Mount(fmt.Sprintf("/%s", poolName), distFS); err != nil {
				return nil, err
			}
		}
		f.users[userName] = rootFs
	}
	return f, nil
}

type AuthFS struct {
	User string
	afero.Fs
}

func (c *FsContext) LoadFS(r *http.Request, guest bool) (*AuthFS, error) {
	username, password, ok := r.BasicAuth()
	if !ok {
		if !guest {
			return nil, errors.Wrapf(NoAuthorizedError, "basic auth required")
		}
		return &AuthFS{
			User: "guest",
			Fs:   c.users["guest"],
		}, nil
	}
	user, ok := c.Config.Users[username]
	if !ok {
		return nil, errors.Wrapf(NoAuthorizedError, "user pool %s not found", user)
	}
	if user.Password != password {
		return nil, errors.Wrapf(NoAuthorizedError, "user %s password not allowed", user)
	}
	return &AuthFS{
		User: username,
		Fs:   c.users[username],
	}, nil
}
