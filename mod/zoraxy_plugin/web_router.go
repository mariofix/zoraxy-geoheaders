package zoraxy_plugin

import (
	"embed"
	"html"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

/*
	Web UI Router for Zoraxy Plugins
*/

type PluginUiRouter struct {
	pluginID         string
	embedFS          *embed.FS
	fsSubpath        string
	rootURLPath      string
	devWebRoot       string
	enableDevMode    bool
	terminateHandler func()
}

func NewPluginEmbedUIRouter(pluginID string, embedFS *embed.FS, fsSubpath string, rootURLPath string) *PluginUiRouter {
	if !strings.HasPrefix(rootURLPath, "/") {
		rootURLPath = "/" + rootURLPath
	}
	if !strings.HasSuffix(rootURLPath, "/") && rootURLPath != "/" {
		rootURLPath = rootURLPath + "/"
	}

	return &PluginUiRouter{
		pluginID:      pluginID,
		embedFS:       embedFS,
		fsSubpath:     strings.TrimPrefix(fsSubpath, "/"),
		rootURLPath:   rootURLPath,
		enableDevMode: false,
	}
}

func (p *PluginUiRouter) SetDevWebRoot(webRoot string) {
	if info, err := os.Stat(webRoot); err == nil && info.IsDir() {
		p.devWebRoot = webRoot
		p.enableDevMode = true
	}
}

func (p *PluginUiRouter) RegisterTerminateHandler(handler func(), mux *http.ServeMux) {
	p.terminateHandler = handler
	termPath := filepath.ToSlash(filepath.Join(p.rootURLPath, "terminate"))
	if !strings.HasPrefix(termPath, "/") {
		termPath = "/" + termPath
	}
	mux.HandleFunc(termPath, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
		if p.terminateHandler != nil {
			go p.terminateHandler()
		}
	})
}

func (p *PluginUiRouter) HandleFunc(pattern string, handler http.HandlerFunc, mux *http.ServeMux) {
	pattern = strings.TrimPrefix(pattern, "/")
	fullPath := filepath.ToSlash(filepath.Join(p.rootURLPath, pattern))
	if !strings.HasPrefix(fullPath, "/") {
		fullPath = "/" + fullPath
	}
	mux.HandleFunc(fullPath, handler)
}

func (p *PluginUiRouter) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqPath := r.URL.Path
		trimmed := strings.TrimPrefix(reqPath, p.rootURLPath)
		trimmed = strings.TrimPrefix(trimmed, "/")

		// Extract only base filename to prevent path traversal
		baseName := filepath.Base(trimmed)
		if baseName == "" || baseName == "." || baseName == "/" {
			baseName = "index.html"
		}

		// Try dev directory if enabled
		if p.enableDevMode && p.devWebRoot != "" {
			diskPath := filepath.Join(p.devWebRoot, baseName)
			if info, err := os.Stat(diskPath); err == nil && !info.IsDir() {
				content, err := os.ReadFile(diskPath)
				if err == nil {
					p.serveBytes(w, r, baseName, content)
					return
				}
			}
		}

		// Fallback to embedded filesystem
		if p.embedFS != nil {
			subFS, err := fs.Sub(p.embedFS, p.fsSubpath)
			if err == nil {
				content, err := fs.ReadFile(subFS, baseName)
				if err == nil {
					p.serveBytes(w, r, baseName, content)
					return
				}
			}
		}

		http.NotFound(w, r)
	})
}

func (p *PluginUiRouter) serveBytes(w http.ResponseWriter, r *http.Request, filename string, content []byte) {
	csrfToken := r.Header.Get("X-Zoraxy-Csrf")
	if csrfToken == "" {
		csrfToken = r.URL.Query().Get("csrf")
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".html", ".htm":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		htmlStr := string(content)
		if csrfToken != "" {
			htmlStr = strings.ReplaceAll(htmlStr, "{{.csrfToken}}", html.EscapeString(csrfToken))
			htmlStr = strings.ReplaceAll(htmlStr, "{{csrf_token}}", html.EscapeString(csrfToken))
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(htmlStr))
	case ".css":
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	case ".js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	case ".json":
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	case ".png":
		w.Header().Set("Content-Type", "image/png")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	case ".svg":
		w.Header().Set("Content-Type", "image/svg+xml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(content)
	}
}
