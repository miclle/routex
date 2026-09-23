//go:build !development

package website

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"os"
	"path"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
	"github.com/fox-gonic/fox/render"
)

//go:embed build/*
var embedFS embed.FS

var assets []string

const (
	htmlCacheControl  = "no-cache"
	assetCacheControl = "public, max-age=31536000, immutable"
)

func init() {
	entries, err := embedFS.ReadDir("build")
	if err != nil {
		fmt.Printf("Fail to read build dir: %+v", err)
		os.Exit(1)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fp := entry.Name()
		assets = append(assets, fp)
	}
}

// EmbedAssets serves the embedded SPA assets in production mode.
func EmbedAssets(router *fox.Engine) {
	tmpl := template.Must(template.New("").ParseFS(embedFS, "build/*.html"))

	homepage := render.HTML{
		Template: tmpl,
		Name:     "index.html",
		Data:     map[string]string{},
	}

	registerPageRoute(router, "/", homepage, htmlCacheControl)

	// handle the assets files
	registerAssetRoutes(router)

	for _, asset := range assets {
		router.StaticFileFS(asset, path.Join("build", asset), http.FS(embedFS))
	}

	router.NotFound(func(c *fox.Context) any {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			return httperrors.ErrNotFound
		}

		if isAPIPath(c.Request.URL.Path) {
			return httperrors.ErrNotFound
		}

		filepath := "public" + c.Request.URL.Path

		file, err := embedFS.Open(filepath)
		if errors.Is(err, fs.ErrNotExist) {
			c.Header("Cache-Control", htmlCacheControl)
			return homepage
		}

		info, err := file.Stat()
		if err != nil {
			return fmt.Errorf("stat embedded file: %w", err)
		}

		return render.Reader{
			ContentLength: info.Size(),
			Reader:        file,
		}
	})
}

func registerPageRoute(router *fox.Engine, route string, response any, cacheControl string) {
	handler := func(c *fox.Context) any {
		if cacheControl != "" {
			c.Header("Cache-Control", cacheControl)
		}
		return response
	}

	router.GET(route, handler)
	router.HEAD(route, handler)
}

func registerAssetRoutes(router *fox.Engine) {
	fs := StaticFS("build/assets")
	fileServer := http.StripPrefix("/assets", http.FileServer(fs))

	handler := func(c *fox.Context) {
		c.Header("Cache-Control", assetCacheControl)
		fileServer.ServeHTTP(c.Writer, c.Request)
	}

	router.GET("/assets/*filepath", handler)
	router.HEAD("/assets/*filepath", handler)
}

// resource is an interface that provides static file.
type resource struct {
	prefix string
	fs     embed.FS
}

// Open implements the interface required by http.FS.
func (r *resource) Open(name string) (fs.File, error) {
	name = path.Join(r.prefix, name)
	return r.fs.Open(name)
}

// StaticFS returns a static http file system from the embedded FS.
func StaticFS(prefix string) http.FileSystem {
	return http.FS(&resource{prefix: prefix, fs: embedFS})
}
