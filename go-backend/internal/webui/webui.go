// Package webui 把前端构建产物嵌入二进制，单个文件即可运行。
//
// 构建前把 frontend/dist 复制到本目录的 dist/（make build、Dockerfile、Release 流程都会做）。
// 没有嵌入前端时（直接 go build，只有 .gitkeep），回退到工作目录下的 frontend_dist/，便于开发。
package webui

import (
	"embed"
	"io/fs"
	"os"
)

//go:embed all:dist
var dist embed.FS

// FS 返回前端文件：优先嵌入的，其次 dir 目录；都没有时返回 nil。
func FS(dir string) (fsys fs.FS, embedded bool) {
	if sub, err := fs.Sub(dist, "dist"); err == nil && hasIndex(sub) {
		return sub, true
	}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		if d := os.DirFS(dir); hasIndex(d) {
			return d, false
		}
	}
	return nil, false
}

func hasIndex(f fs.FS) bool {
	st, err := fs.Stat(f, "index.html")
	return err == nil && !st.IsDir()
}
