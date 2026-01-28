# Simple WebDAV Server

> This project is part of Dragon's Zone HomeLab

A simple WebDAV and SFTP server written in Go. Supports multi-user, storage pools, and basic permission management.

## Description

This is a lightweight WebDAV server implementation designed to provide simple and efficient file sharing services. It supports the WebDAV protocol and optional SFTP service, making it ideal for use in HomeLab environments for individuals or small teams.

Key Features:
-   **WebDAV Support**: Standard WebDAV protocol support.
-   **SFTP Support**: Optional SFTP service.
-   **Multi-User Management**: Configuration-based multi-user authentication.
-   **Storage Pools**: Flexible storage path mapping and permission control.
-   **Fail2ban Integration**: Friendly log format for easy integration with Fail2ban to prevent brute force attacks.

## Usage

### Build

Ensure you have Go 1.25 or higher installed.

```bash
go build -o webdav-server main.go
```

### Run

Run with the default configuration file `config.yml`:

```bash
./webdav-server
```

Or specify a configuration file path:

```bash
./webdav-server -config /path/to/your/config.yaml
```

Enable debug mode:

```bash
./webdav-server -debug
```

## Configuration

The configuration file is usually named `config.yaml`. Below is a configuration example and its explanation:

```yaml
# HTTP server bind address
bind: 127.0.0.1:8080

# User definitions
users:
  admin:
    password: 123456
  user1:
    password: password123

# Storage pool definitions
pools:
  # Data pool name
  data:
    # Local filesystem path
    path: /var/lib/webdav-server
    # User-specific permissions (rw: read-write, r: read-only)
    permissions:
      admin: rw
      user1: r
    # Default permission
    permission: r

# WebDAV settings
webdav:
  enabled: true
  prefix: /dav

# SFTP settings (optional)
sftp:
  enabled: true
  bind: 127.0.0.1:8022
```

## Fail2ban Configuration

The server logs `|security| Login failed.` formatted logs for fail2ban monitoring.

### filter.d/webdav-server.conf

```ini
[Definition]
failregex = \|security\| Login failed.*remote=<HOST>
ignoreregex =
```

### jail.local

```ini
[webdav-server]
enabled = true
# Please adjust the port according to your actual situation
port = 8080,8022
filter = webdav-server
# Ensure systemd service or stdout is redirected to this log file
logpath = /var/log/webdav-server.log
maxretry = 3
bantime = 3600
findtime = 600
```

## License

This project is licensed under the Apache-2.0 License. See the [LICENSE](LICENSE) file for details.
