package ksapp

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// spaStaticHandler 返回一个 http.Handler，提供 dir 下的静态文件服务；
// 当请求路径对应的文件不存在（或是目录）时，fallback 到 dir/index.html
// （SPA 前端路由语义，对齐 Python _SPAStaticFiles 的 404→index 兜底行为）。
//
// 用于 App.MountStaticRoot。
func spaStaticHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	indexPath := filepath.Join(dir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := r.URL.Path
		if reqPath == "/" || reqPath == "" {
			http.ServeFile(w, r, indexPath)
			return
		}
		// 规范化到本地路径，防穿越（filepath.Join 自动 Clean）。
		rel := strings.TrimPrefix(reqPath, "/")
		full := filepath.Join(dir, filepath.FromSlash(rel))
		// 越界检查（Clean 之后仍以 dir 为前缀）
		absDir, errDir := filepath.Abs(dir)
		absFull, errFull := filepath.Abs(full)
		if errDir != nil || errFull != nil || !strings.HasPrefix(absFull, absDir) {
			http.ServeFile(w, r, indexPath)
			return
		}
		info, err := os.Stat(full)
		if err == nil && !info.IsDir() {
			fs.ServeHTTP(w, r)
			return
		}
		// 文件不存在 / 是目录 → SPA fallback 到 index.html
		http.ServeFile(w, r, indexPath)
	})
}
