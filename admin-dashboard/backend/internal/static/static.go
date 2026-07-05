package static

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed static/dist
var embedded embed.FS

type Handler struct {
	dist fs.FS
}

func NewHandler() Handler {
	dist, err := fs.Sub(embedded, "static/dist")
	if err != nil {
		return Handler{dist: embedded}
	}
	return Handler{dist: dist}
}

func NewHandlerFromFS(dist fs.FS) Handler {
	return Handler{dist: dist}
}

func (h Handler) ServeAsset(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/assets/")
	h.serveFile(w, r, "assets/"+path, "public, max-age=31536000, immutable")
}

func (h Handler) ServeFavicon(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, "favicon.svg", "public, max-age=86400")
}

func (h Handler) ServeThemeInit(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, "theme-init.js", "no-cache")
}

func (h Handler) ServeIndex(w http.ResponseWriter, r *http.Request) {
	h.serveFile(w, r, "index.html", "no-cache")
}

func (h Handler) HasIndex() bool {
	_, err := fs.Stat(h.dist, "index.html")
	return err == nil
}

func (h Handler) serveFile(w http.ResponseWriter, r *http.Request, path, cacheControl string) {
	data, err := fs.ReadFile(h.dist, path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if strings.HasSuffix(path, ".html") {
		contentType = "text/html; charset=utf-8"
	}
	info, err := fs.Stat(h.dist, path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cacheControl)
	http.ServeContent(w, r, path, info.ModTime(), bytes.NewReader(data))
}
