package main

import (
	"strings"
)

type Config struct {
	// 绑定端口
	Bind string `yaml:"bind"`
	// 映射池
	Pools map[string]Pool `yaml:"pools"`
	// 用户表
	Users map[string]User `yaml:"users"`
	// LDAP 映射
	LDAP *LDAPAuth `yaml:"ldap"`
}

type LDAPAuth struct {
	URL          string `yaml:"url"`
	BindUser     string `yaml:"bind_user"`
	BindPassword string `yaml:"bind_password"`
	BaseDN       string `yaml:"base_dn"`
	Search       string `yaml:"search"`
	NameEntry    string `yaml:"name_entry"`
}

type User struct {
	Password  string `yaml:"password"`
	PublicKey string `yaml:"public_key"`
}

type Pool struct {
	Path        string                  `yaml:"path"`
	Permissions map[string][]Permission `yaml:"permissions,perms"`
	DefaultPerm FilePerm                `yaml:"permission"`
}
type Permission struct {
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
	return p.IsRead() && strings.Contains(string(p), "x")
}
