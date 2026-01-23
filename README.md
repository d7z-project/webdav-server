# 简易的 webdav 服务器

## Fail2ban 配置

服务器日志中会打印 `|security| Login failed.` 格式的日志用于 fail2ban 监控。

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
# 请根据实际情况调整端口
port = 8080,8022
filter = webdav-server
# 请确保 systemd service 或者 stdout 重定向到了此日志文件
logpath = /var/log/webdav-server.log
maxretry = 3
bantime = 3600
findtime = 600
```
