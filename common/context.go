package common

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/ssh"

	"code.d7z.net/packages/webdav-server/mergefs"
	"github.com/spf13/afero"
)

var (
	NoAuthorizedError = errors.New("no authorized")
	NoPermissionError = errors.New("no permission")
)

func verifyPassword(hashedPassword, plainPassword string) bool {
	if strings.HasPrefix(hashedPassword, "argon2id:") {
		return verifyArgon2id(strings.TrimPrefix(hashedPassword, "argon2id:"), plainPassword)
	}
	if strings.HasPrefix(hashedPassword, "sha256:") {
		expectedHash := strings.TrimPrefix(hashedPassword, "sha256:")
		sum := sha256.Sum256([]byte(plainPassword))
		actualHash := fmt.Sprintf("%x", sum)
		return subtle.ConstantTimeCompare([]byte(expectedHash), []byte(actualHash)) == 1
	}
	return hashedPassword == plainPassword
}

func verifyArgon2id(encodedHash, password string) bool {
	// Standard modular crypt format: $argon2id$v=19$m=65536,t=3,p=4$salt$hash
	vals := strings.Split(encodedHash, "$")
	if len(vals) != 6 {
		return false
	}

	var version int
	_, err := fmt.Sscanf(vals[2], "v=%d", &version)
	if err != nil {
		return false
	}

	var memory uint32
	var iterations uint32
	var parallelism uint8
	_, err = fmt.Sscanf(vals[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism)
	if err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(vals[4])
	if err != nil {
		return false
	}

	hash, err := base64.RawStdEncoding.DecodeString(vals[5])
	if err != nil {
		return false
	}

	otherHash := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(hash)))
	return subtle.ConstantTimeCompare(hash, otherHash) == 1
}

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

func (c *FsContext) LoadFS(username, password string, publicKey ssh.PublicKey, guestAccept bool) (*AuthFS, error) {
	if username == "guest" {
		if !guestAccept {
			return nil, errors.Wrapf(NoPermissionError, "guest not allowed")
		}
		return &AuthFS{
			User: "guest",
			Fs:   c.users["guest"],
		}, nil
	}
	if password == "" && publicKey == nil {
		return nil, errors.Wrapf(NoPermissionError, "no password or public key")
	}
	user, ok := c.Config.Users[username]
	if !ok {
		return nil, errors.Wrapf(NoAuthorizedError, "user %s not found", username)
	}
	if password != "" {
		if !verifyPassword(user.Password, password) {
			return nil, errors.Wrapf(NoAuthorizedError, "user %s password not allowed", username)
		}
	}

	if publicKey != nil {
		matched := false
		for _, key := range user.PublicKeys {
			out, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key))
			if err != nil {
				return nil, errors.Wrapf(NoAuthorizedError, "user %s public key parsing failed", username)
			}
			if string(out.Marshal()) == string(publicKey.Marshal()) {
				matched = true
				break
			}
		}
		if !matched {
			return nil, errors.Wrapf(NoAuthorizedError, "user %s public key not allowed", username)
		}
	}
	return &AuthFS{
		User: username,
		Fs:   c.users[username],
	}, nil
}

func (c *FsContext) LoadWebFS(r *http.Request, guestAccept bool) (*AuthFS, error) {
	username, password, ok := r.BasicAuth()
	if !ok {
		username = "guest"
	}
	return c.LoadFS(username, password, nil, guestAccept)
}

func (c *FsContext) LoadUserFS(username string) afero.Fs {
	return c.users[username]
}
