package main

import (
	"strings"
)

type Config struct {
	// 绑定端口
	Bind string `yaml:"bind"`
	// 映射池
	Pools map[string]ConfigPool `yaml:"pools"`
	// 用户表
	Users map[string]ConfigUser `yaml:"users"`
	// LDAP 映射
	LDAP *ConfigLDAPAuth `yaml:"ldap,omitempty"`

	Webdav ConfigWebdav `yaml:"webdav"`
	SFTP   ConfigSFTP   `yaml:"sftp"`
	NFS    ConfigNFS    `yaml:"nfs"`
}

type ConfigWebdav struct {
	Enabled bool   `yaml:"enabled"`
	Prefix  string `yaml:"prefix"`
}
type ConfigSFTP struct {
	Enabled bool   `yaml:"enabled"`
	Bind    string `yaml:"bind"`
}
type ConfigNFS struct {
	Enabled bool   `yaml:"enabled"`
	Bind    string `yaml:"bind"`
}

type ConfigLDAPAuth struct {
	URL          string `yaml:"url"`
	BindUser     string `yaml:"bind_user"`
	BindPassword string `yaml:"bind_password"`
	BaseDN       string `yaml:"base_dn"`
	Search       string `yaml:"search"`
	NameEntry    string `yaml:"name_entry"`
}

type ConfigUser struct {
	Password  string `yaml:"password"`
	PublicKey string `yaml:"public_key"`
}

type ConfigPool struct {
	Path        string                        `yaml:"path"`
	Permissions map[string][]ConfigPermission `yaml:"permissions"`
	DefaultPerm FilePerm                      `yaml:"permission"`
}
type ConfigPermission struct {
	Prefix     string   `yaml:"prefix"`
	Permission FilePerm `yaml:"permission"`
}

type FilePerm string

func (p FilePerm) IsRead() bool {
	return strings.Contains(string(p), "r")
}
func (p FilePerm) IsWrite() bool {
	return p.IsRead() && strings.Contains(string(p), "w")
}
func (p FilePerm) IsPreview() bool {
	return p.IsRead() && strings.Contains(string(p), "p")
}
