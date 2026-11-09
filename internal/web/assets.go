package web

import (
	"bytes"
	"errors"
	"io/fs"
	"net/http"
	"path"
)

// contentTypes is fixed here because Go's mime package also reads the Windows
// registry, where other programs can map ".js" to something a browser refuses
// to run. A file type not listed here is not served.
var contentTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".svg":   "image/svg+xml",
	".woff2": "font/woff2",
}

func (s *Server) handleAsset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("path")
	if name == "" {
		name = "index.html"
	}
	contentType, known := contentTypes[path.Ext(name)]
	if !known || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	data, err := fs.ReadFile(s.deps.Assets, name)
	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.deps.Logger.Error("read panel file failed", "file", name, "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	if _, err := bytes.NewReader(data).WriteTo(w); err != nil {
		s.deps.Logger.Debug("send panel file failed", "file", name, "error", err)
	}
}
