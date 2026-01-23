package common

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/inhies/go-bytesize"
	"golang.org/x/crypto/ssh"
)

var nameRegexp = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

type Config struct {
	// 绑定端口
	Bind string `yaml:"bind"`
	// 映射池
	Pools map[string]ConfigPool `yaml:"pools"`
	// 用户表
	Users map[string]ConfigUser `yaml:"users"`

	Webdav  ConfigWebdav  `yaml:"webdav"`
	SFTP    ConfigSFTP    `yaml:"sftp"`
	Preview ConfigPreview `yaml:"preview"`
}

type ConfigWebdav struct {
	Enabled bool   `yaml:"enabled"`
	Prefix  string `yaml:"prefix"`
}
type ConfigSFTP struct {
	Enabled        bool     `yaml:"enabled"`
	Bind           string   `yaml:"bind"`
	Privatekeys    []string `yaml:"private_keys"`
	WelcomeMessage string   `yaml:"welcome_message"`
}

type FileSize uint64

func (f *FileSize) UnmarshalYAML(dt []byte) error {
	var s string
	if err := yaml.Unmarshal(dt, &s); err != nil {
		return err
	}
	parse, err := bytesize.Parse(s)
	if err != nil {
		return err
	}
	*f = FileSize(parse)
	return nil
}

type ConfigPreview struct {
	MaxUploadSize FileSize `yaml:"max_upload_size"`
}

type ConfigUser struct {
	Password   string   `yaml:"password"`
	PublicKeys []string `yaml:"public_keys"`
}

type ConfigPool struct {
	Path        string              `yaml:"path"`
	Permissions map[string]FilePerm `yaml:"permissions"`
	DefaultPerm FilePerm            `yaml:"permission"`
}

type FilePerm string

func (p FilePerm) IsRead() bool {
	return strings.Contains(string(p), "r")
}

func (p FilePerm) IsWrite() bool {
	return p.IsRead() && strings.Contains(string(p), "w")
}

func LoadConfig(filePath string) (*Config, error) {
	data, err := os.ReadFile(filePath)
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
		if name == "guest" {
			return nil, errors.New("guest user is retained")
		}
		if !nameRegexp.MatchString(name) {
			return nil, fmt.Errorf("invalid user name: %s", name)
		}
		if user.Password == "" && len(user.PublicKeys) == 0 {
			slog.Warn("password or public key is not defined.", "user", name)
		}
		if len(user.PublicKeys) != 0 {
			for _, key := range user.PublicKeys {
				_, _, _, _, err := ssh.ParseAuthorizedKey([]byte(key))
				if err != nil {
					return nil, fmt.Errorf("invalid public key(%s): %s", name, err)
				}
			}
		}
	}
	result.Users["guest"] = ConfigUser{
		Password:   "",
		PublicKeys: make([]string, 0),
	}
	for poolName, pool := range result.Pools {
		if !nameRegexp.MatchString(poolName) {
			return nil, fmt.Errorf("invalid pool name: %s", poolName)
		}
		if pool.Path == "" {
			return nil, fmt.Errorf("invalid pool path: %s", poolName)
		}
		if stat, err := os.Stat(pool.Path); err != nil || !stat.IsDir() {
			return nil, fmt.Errorf("invalid pool path %s: not exists or not dir", poolName)
		}
		if len(pool.Permissions) == 0 && !pool.DefaultPerm.IsRead() {
			slog.Warn("pool cannot be operated by any user.", "pool", poolName)
		}
		for name, permission := range pool.Permissions {
			if !nameRegexp.MatchString(name) {
				return nil, fmt.Errorf("invalid pool name: %s", name)
			}
			if _, ok := result.Users[name]; !ok {
				slog.Warn("the user does not exist", "user", name)
			}
			if permission == "" {
				return nil, fmt.Errorf("invalid permission (%s/%s)", poolName, name)
			}
		}
	}
	if result.Webdav.Enabled {
		if result.Webdav.Prefix == "" {
			result.Webdav.Prefix = "/dav"
		}
		result.Webdav.Prefix = "/" + strings.TrimSpace(strings.Trim(result.Webdav.Prefix, "/"))
		if result.Webdav.Prefix == "/" {
			return nil, errors.New("webdav not support prefix '/' or empty")
		}
	}
	if result.Preview.MaxUploadSize == 0 {
		result.Preview.MaxUploadSize = 1024 * 1024 * 1024
	}
	if result.SFTP.Enabled {
		if len(result.SFTP.Privatekeys) == 0 {
			return nil, errors.New("sftp need support private key , e.g. ssh-keygen -t rsa -f id_rsa -N ''")
		}
		for i, item := range result.SFTP.Privatekeys {
			if !strings.HasPrefix(item, "-----BEGIN OPENSSH PRIVATE KEY-----") {
				data, err := os.ReadFile(item)
				if err != nil {
					return nil, fmt.Errorf("invalid private key item %d: %s", i, item)
				}
				result.SFTP.Privatekeys[i] = string(data)
				if !strings.HasPrefix(string(data), "-----BEGIN OPENSSH PRIVATE KEY-----") {
					return nil, fmt.Errorf("invalid private key item %d: %s", i, item)
				}
			}
		}
		if result.SFTP.WelcomeMessage == "" {
			result.SFTP.WelcomeMessage = "Welcome to SFTP, %s !"
		}
	}
	return &result, nil
}
