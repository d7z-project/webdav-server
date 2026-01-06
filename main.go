package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	"golang.org/x/net/webdav"
	"gopkg.in/yaml.v3"
)

// Config 服务器配置
type Config struct {
	Address         string `yaml:"address"`           // 监听地址
	Port            int    `yaml:"port"`              // 监听端口
	DataDir         string `yaml:"data_dir"`          // 数据目录
	EnableHTTPS     bool   `yaml:"enable_https"`      // 启用HTTPS
	TLSCert         string `yaml:"tls_cert"`          // TLS证书路径
	TLSKey          string `yaml:"tls_key"`           // TLS密钥路径
	Username        string `yaml:"username"`          // 认证用户名
	Password        string `yaml:"password"`          // 认证密码
	ReadOnly        bool   `yaml:"read_only"`         // 只读模式
	EnableRateLimit bool   `yaml:"enable_rate_limit"` // 启用速率限制
	RateLimitRPS    int    `yaml:"rate_limit_rps"`    // 每秒请求数限制
}

// WebDAVServer WebDAV服务器
type WebDAVServer struct {
	config     *Config
	webdav     *webdav.Handler
	router     *chi.Mux
	httpServer *http.Server
}

// NewWebDAVServer 创建新的WebDAV服务器
func NewWebDAVServer(config *Config) (*WebDAVServer, error) {
	// 确保数据目录存在
	if err := os.MkdirAll(config.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %w", err)
	}

	// 创建WebDAV处理器
	handler := &webdav.Handler{
		FileSystem: webdav.Dir(config.DataDir),
		LockSystem: webdav.NewMemLS(),
	}

	// 创建chi路由器
	router := chi.NewRouter()

	server := &WebDAVServer{
		config: config,
		webdav: handler,
		router: router,
	}

	// 设置中间件和路由
	server.setupMiddleware()
	server.setupRoutes()

	return server, nil
}

// setupMiddleware 设置中间件
func (s *WebDAVServer) setupMiddleware() {
	// 基础中间件
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Logger)
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(60 * time.Second))

	// 速率限制
	if s.config.EnableRateLimit && s.config.RateLimitRPS > 0 {
		s.router.Use(httprate.LimitByIP(s.config.RateLimitRPS, 1*time.Second))
	}
}

// setupRoutes 设置路由
func (s *WebDAVServer) setupRoutes() {
	// 健康检查
	s.router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// WebDAV路由
	s.router.Group(func(r chi.Router) {
		// 认证中间件
		if s.config.Username != "" && s.config.Password != "" {
			r.Use(s.basicAuthMiddleware)
		}

		// WebDAV路由
		r.HandleFunc("/*", s.handleWebDAV)

		// 文件预览路由
		r.Get("/preview/*", s.handleFilePreview)
		r.Get("/preview/", s.handleDirectoryListing)
	})
}

// basicAuthMiddleware 基本认证中间件
func (s *WebDAVServer) basicAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != s.config.Username || password != s.config.Password {
			w.Header().Set("WWW-Authenticate", `Basic realm="WebDAV Server"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// handleWebDAV 处理WebDAV请求
func (s *WebDAVServer) handleWebDAV(w http.ResponseWriter, r *http.Request) {
	// 检查只读模式
	if s.config.ReadOnly && !isReadMethod(r.Method) {
		http.Error(w, "服务器处于只读模式", http.StatusForbidden)
		return
	}

	// 处理WebDAV请求
	s.webdav.ServeHTTP(w, r)
}

// isReadMethod 检查是否为只读方法
func isReadMethod(method string) bool {
	readMethods := []string{"GET", "HEAD", "OPTIONS", "PROPFIND"}
	for _, m := range readMethods {
		if method == m {
			return true
		}
	}
	return false
}

// handleFilePreview 处理文件预览
func (s *WebDAVServer) handleFilePreview(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	if path == "" {
		path = "/"
	}

	fullPath := filepath.Join(s.config.DataDir, path)

	// 检查文件是否存在
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "文件不存在", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 如果是目录，重定向到目录列表
	if info.IsDir() {
		http.Redirect(w, r, "/preview/"+path+"/", http.StatusFound)
		return
	}

	// 打开文件
	file, err := os.Open(fullPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// 根据文件类型设置Content-Type
	contentType := getContentType(fullPath)
	w.Header().Set("Content-Type", contentType)

	// 对于文本文件，直接显示
	if isTextFile(fullPath) {
		content, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(content)
		return
	}

	// 对于其他文件，提供下载
	http.ServeContent(w, r, filepath.Base(fullPath), info.ModTime(), file)
}

// handleDirectoryListing 处理目录列表
func (s *WebDAVServer) handleDirectoryListing(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	if path == "" {
		path = "/"
	}

	fullPath := filepath.Join(s.config.DataDir, path)

	// 检查路径是否存在且是目录
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "目录不存在", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !info.IsDir() {
		http.Error(w, "不是目录", http.StatusBadRequest)
		return
	}

	// 读取目录内容
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// 生成HTML目录列表
	var html strings.Builder
	html.WriteString("<!DOCTYPE html><html><head><title>目录列表: " + path + "</title>")
	html.WriteString("<style>")
	html.WriteString("body { font-family: Arial, sans-serif; margin: 20px; }")
	html.WriteString("h1 { color: #333; }")
	html.WriteString("ul { list-style-type: none; padding: 0; }")
	html.WriteString("li { padding: 5px 0; }")
	html.WriteString("a { color: #0066cc; text-decoration: none; }")
	html.WriteString("a:hover { text-decoration: underline; }")
	html.WriteString(".file { color: #666; }")
	html.WriteString(".dir { color: #009900; font-weight: bold; }")
	html.WriteString(".file-size { color: #999; font-size: 0.9em; margin-left: 10px; }")
	html.WriteString(".file-time { color: #999; font-size: 0.9em; margin-left: 10px; }")
	html.WriteString("</style>")
	html.WriteString("</head><body>")
	html.WriteString("<h1>目录: " + path + "</h1>")
	html.WriteString("<ul>")

	// 上级目录链接
	if path != "/" {
		parentPath := filepath.Dir(path)
		if parentPath == "." {
			parentPath = "/"
		}
		html.WriteString("<li><a href=\"/preview/" + parentPath + "\" class=\"dir\">../</a></li>")
	}

	// 目录内容
	for _, entry := range entries {
		name := entry.Name()
		isDir := entry.IsDir()
		itemPath := filepath.Join(path, name)

		// 获取文件信息
		fileInfo, _ := entry.Info()
		fileSize := ""
		fileTime := ""

		if !isDir && fileInfo != nil {
			fileSize = formatFileSize(fileInfo.Size())
			fileTime = fileInfo.ModTime().Format("2006-01-02 15:04")
		}

		if isDir {
			html.WriteString("<li><a href=\"/preview/" + itemPath + "/\" class=\"dir\">" + name + "/</a>")
			html.WriteString("<span class=\"file-time\">" + fileTime + "</span></li>")
		} else {
			html.WriteString("<li><a href=\"/preview/" + itemPath + "\" class=\"file\">" + name + "</a>")
			html.WriteString("<span class=\"file-size\">" + fileSize + "</span>")
			html.WriteString("<span class=\"file-time\">" + fileTime + "</span></li>")
		}
	}

	html.WriteString("</ul>")
	html.WriteString("</body></html>")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html.String()))
}

// formatFileSize 格式化文件大小
func formatFileSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%d B", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// getContentType 获取文件Content-Type
func getContentType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".html", ".htm":
		return "text/html"
	case ".css":
		return "text/css"
	case ".js":
		return "application/javascript"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".txt", ".md", ".go", ".py", ".java", ".c", ".cpp", ".h":
		return "text/plain"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".pdf":
		return "application/pdf"
	case ".zip":
		return "application/zip"
	case ".tar":
		return "application/x-tar"
	case ".gz":
		return "application/gzip"
	default:
		return "application/octet-stream"
	}
}

// isTextFile 检查是否为文本文件
func isTextFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	textExtensions := []string{
		".txt", ".md", ".go", ".py", ".java", ".c", ".cpp", ".h",
		".html", ".htm", ".css", ".js", ".json", ".xml", ".yaml", ".yml",
		".sh", ".bash", ".zsh", ".conf", ".ini", ".toml",
	}
	for _, te := range textExtensions {
		if ext == te {
			return true
		}
	}
	return false
}

// Start 启动服务器
func (s *WebDAVServer) Start() error {
	addr := fmt.Sprintf("%s:%d", s.config.Address, s.config.Port)

	s.httpServer = &http.Server{
		Addr:    addr,
		Handler: s.router,
	}

	log.Printf("WebDAV服务器启动中，监听地址: %s", addr)
	log.Printf("数据目录: %s", s.config.DataDir)
	log.Printf("WebDAV访问地址: http://%s/", addr)
	log.Printf("文件预览地址: http://%s/preview/", addr)
	log.Printf("健康检查地址: http://%s/health", addr)

	if s.config.Username != "" && s.config.Password != "" {
		log.Printf("已启用基本认证，用户名: %s", s.config.Username)
	}

	if s.config.EnableRateLimit && s.config.RateLimitRPS > 0 {
		log.Printf("已启用速率限制: %d 请求/秒", s.config.RateLimitRPS)
	}

	var err error
	if s.config.EnableHTTPS {
		log.Printf("启用HTTPS模式")
		err = s.httpServer.ListenAndServeTLS(s.config.TLSCert, s.config.TLSKey)
	} else {
		err = s.httpServer.ListenAndServe()
	}

	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("服务器启动失败: %w", err)
	}

	return nil
}

// Stop 停止服务器
func (s *WebDAVServer) Stop() error {
	if s.httpServer == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	return s.httpServer.Shutdown(ctx)
}

// loadConfig 加载配置文件
func loadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		// 如果配置文件不存在，使用默认配置
		if os.IsNotExist(err) {
			return &Config{
				Address:         "0.0.0.0",
				Port:            8080,
				DataDir:         "/var/lib/webdav-server/data",
				ReadOnly:        false,
				EnableRateLimit: true,
				RateLimitRPS:    100,
			}, nil
		}
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	// 设置默认值
	if config.Address == "" {
		config.Address = "0.0.0.0"
	}
	if config.Port == 0 {
		config.Port = 8080
	}
	if config.DataDir == "" {
		config.DataDir = "/var/lib/webdav-server/data"
	}
	if config.RateLimitRPS == 0 {
		config.RateLimitRPS = 100
	}

	return &config, nil
}

// saveConfig 保存配置文件
func saveConfig(config *Config, configPath string) error {
	data, err := yaml.Marshal(config)
	if err != nil {
		return err
	}

	// 确保配置目录存在
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}

	return os.WriteFile(configPath, data, 0644)
}

func main() {
	// 配置文件路径
	configPath := "/etc/webdav-server/config.yaml"
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// 尝试当前目录
		configPath = "config.yaml"
	}

	// 加载配置
	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("加载配置文件失败: %v", err)
	}

	// 如果配置文件不存在，创建默认配置文件
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := saveConfig(config, configPath); err != nil {
			log.Printf("创建默认配置文件失败: %v", err)
		} else {
			log.Printf("已创建默认配置文件: %s", configPath)
		}
	}

	// 创建服务器
	server, err := NewWebDAVServer(config)
	if err != nil {
		log.Fatalf("创建WebDAV服务器失败: %v", err)
	}

	// 设置信号处理
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	// 优雅关闭
	go func() {
		<-ctx.Done()
		log.Println("收到关闭信号，正在优雅关闭服务器...")
		if err := server.Stop(); err != nil {
			log.Printf("服务器关闭失败: %v", err)
		}
	}()

	// 启动服务器
	log.Println("启动WebDAV服务器...")
	if err := server.Start(); err != nil {
		log.Fatalf("服务器运行失败: %v", err)
	}

	log.Println("服务器已停止")
}
