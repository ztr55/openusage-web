package web

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// embeddedStatic contains the dashboard shell and the small set of runtime
// assets copied by `make website-build`. The fallback page keeps a plain Go
// build useful even when the frontend has not been built yet.
//
//go:embed static
var embeddedStatic embed.FS

func (s *Server) serveEmbeddedStatic(w http.ResponseWriter, r *http.Request) {
	if !safeStaticRequestPath(r.URL.Path) {
		http.NotFound(w, r)
		return
	}

	relative := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if relative == "." || relative == "" {
		relative = "index.html"
	}

	data, name, err := readEmbeddedStatic(relative)
	if err != nil {
		// Client-side dashboard routes all use the same shell. Asset paths are
		// never allowed to fall through to HTML because that hides broken builds.
		if strings.HasPrefix(relative, "assets/") || strings.HasPrefix(relative, "icons/") || strings.HasPrefix(relative, "brand/") {
			http.NotFound(w, r)
			return
		}
		data, name, err = readEmbeddedStatic("index.html")
		if err != nil {
			data, name, err = readEmbeddedStatic("fallback.html")
		}
	}
	if err != nil {
		http.NotFound(w, r)
		return
	}

	contentType := mime.TypeByExtension(path.Ext(name))
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func readEmbeddedStatic(relative string) ([]byte, string, error) {
	relative = path.Clean(strings.TrimPrefix(relative, "/"))
	if relative == "." || relative == "" || strings.HasPrefix(relative, "../") {
		return nil, "", fs.ErrNotExist
	}
	name := path.Join("static", relative)
	data, err := fs.ReadFile(embeddedStatic, name)
	return data, relative, err
}
