package preview

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"code.d7z.net/packages/webdav-server/assets"
	"code.d7z.net/packages/webdav-server/common"
	"github.com/go-chi/chi/v5"
	"github.com/spf13/afero"
)

type TemplateData struct {
	Path    string
	User    string
	Dirs    []os.FileInfo
	IsGuest bool
}

func WithPreview(ctx *common.FsContext) func(r chi.Router) {
	return func(r chi.Router) {
		r.Route("/", func(r chi.Router) {
			r.Get("/*", handleGet(ctx))
			r.Post("/*", handlePost(ctx))
		})
	}
}

func handleGet(ctx *common.FsContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fs, err := ctx.LoadWebFS(r, true)
		if err != nil {
			username, _, _ := r.BasicAuth()
			if username == "" {
				username = "guest"
			}
			slog.Warn("|security| Login failed.", "source", "preview", "remote", r.RemoteAddr, "user", username, "err", err)
			if errors.Is(err, common.NoAuthorizedError) {
				w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
				http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		slog.Info("|preview| Access.", "path", r.URL.Path, "remote", r.RemoteAddr, "user", fs.User)
		p := strings.TrimPrefix(r.URL.Path, "/preview/")
		stat, err := fs.Stat(p)
		if err != nil {
			http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
			return
		}
		if stat.IsDir() {
			dir, err := afero.ReadDir(fs, p)
			if err != nil {
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				return
			}
			slices.SortFunc(dir, func(a, b os.FileInfo) int {
				if a.IsDir() == b.IsDir() {
					return strings.Compare(a.Name(), b.Name())
				} else if a.IsDir() {
					return -1
				}
				return 1
			})
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = assets.ZPreview.Execute(w, TemplateData{
				Path:    p,
				User:    fs.User,
				Dirs:    dir,
				IsGuest: fs.User == "guest",
			})
		} else {
			file, err := fs.OpenFile(p, os.O_RDONLY, os.ModePerm)
			if err != nil {
				http.Error(w, http.StatusText(http.StatusNotFound), http.StatusNotFound)
				slog.Warn("open file err", "err", err)
				return
			}
			defer file.Close()
			http.ServeContent(w, r, file.Name(), stat.ModTime(), file)
		}
	}
}

func handlePost(ctx *common.FsContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/preview")
		fs, err := ctx.LoadWebFS(r, false)
		if err != nil {
			username, _, _ := r.BasicAuth()
			if username == "" {
				username = "guest"
			}
			slog.Warn("|security| Login failed.", "source", "preview_upload", "remote", r.RemoteAddr, "user", username, "err", err)
			http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
			return
		}

		if r.URL.Query().Has("mkdir") {
			handleMkdir(w, r, fs, p)
			return
		}
		if r.URL.Query().Has("rename") {
			handleRename(w, r, fs, p)
			return
		}
		if r.URL.Query().Has("delete") {
			handleDelete(w, r, fs, p)
			return
		}

		handleUpload(w, r, fs, p, int64(ctx.Config.Preview.MaxUploadSize))
	}
}

func handleMkdir(w http.ResponseWriter, r *http.Request, fs *common.AuthFS, p string) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "参数错误", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") {
		http.Error(w, "名称非法", http.StatusBadRequest)
		return
	}
	target := filepath.Join(p, name)
	if _, err := fs.Stat(target); err == nil {
		http.Error(w, "目录已存在", http.StatusConflict)
		return
	}
	if err := fs.Mkdir(target, os.ModePerm); err != nil {
		slog.Warn("mkdir failed", "err", err)
		http.Error(w, "创建失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("|preview| Mkdir.", "path", target, "remote", r.RemoteAddr, "user", fs.User)
	w.WriteHeader(http.StatusCreated)
}

func handleRename(w http.ResponseWriter, r *http.Request, fs *common.AuthFS, p string) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "参数错误", http.StatusBadRequest)
		return
	}
	oldName := r.FormValue("oldName")
	newName := r.FormValue("newName")
	if oldName == "" || newName == "" {
		http.Error(w, "参数缺失", http.StatusBadRequest)
		return
	}
	if strings.Contains(newName, "/") || strings.Contains(newName, "\\") {
		http.Error(w, "名称非法", http.StatusBadRequest)
		return
	}

	oldPath := filepath.Join(p, oldName)
	newPath := filepath.Join(p, newName)

	if err := fs.Rename(oldPath, newPath); err != nil {
		slog.Warn("rename failed", "err", err)
		http.Error(w, "重命名失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("|preview| Rename.", "old", oldPath, "new", newPath, "remote", r.RemoteAddr, "user", fs.User)
	w.WriteHeader(http.StatusOK)
}

func handleDelete(w http.ResponseWriter, r *http.Request, fs *common.AuthFS, p string) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "参数错误", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	if name == "" {
		http.Error(w, "参数缺失", http.StatusBadRequest)
		return
	}
	target := filepath.Join(p, name)
	if err := fs.RemoveAll(target); err != nil {
		slog.Warn("delete failed", "err", err)
		http.Error(w, "删除失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	slog.Info("|preview| Delete.", "path", target, "remote", r.RemoteAddr, "user", fs.User)
	w.WriteHeader(http.StatusOK)
}

func handleUpload(w http.ResponseWriter, r *http.Request, fs *common.AuthFS, p string, maxSize int64) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSize)
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		http.Error(w, "文件过大或解析错误", http.StatusRequestEntityTooLarge)
		return
	}

	override := r.FormValue("force") == "true"
	file, handler, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "获取文件失败", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	destPath := filepath.Join(p, handler.Filename)
	stat, err := fs.Stat(destPath)
	if err == nil {
		if stat.IsDir() {
			http.Error(w, "目录无法上传内容", http.StatusBadRequest)
			return
		}
		if !override {
			http.Error(w, "文件已存在", http.StatusBadRequest)
			return
		}
	}
	destFile, err := fs.OpenFile(filepath.Join(destPath), os.O_WRONLY|os.O_CREATE, os.ModePerm)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	defer destFile.Close()
	if _, err = io.Copy(destFile, file); err != nil {
		slog.Warn("upload copy failed", "err", err)
		http.Error(w, "上传失败", http.StatusInternalServerError)
		return
	}
	slog.Info("|preview| Upload.", "path", destPath, "remote", r.RemoteAddr, "user", fs.User)
	w.WriteHeader(http.StatusOK)
}
