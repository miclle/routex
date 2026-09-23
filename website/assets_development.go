//go:build development

package website

import (
	"embed"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"

	"github.com/fox-gonic/fox"
	"github.com/fox-gonic/fox/httperrors"
)

//go:embed public/*
var embedFS embed.FS

var (
	origin      *url.URL
	proxyRoutes = []string{"/", "static/*filepath"}
)

func init() {
	var err error
	origin, err = url.Parse(devServerURLFromEnvironment())
	if err != nil {
		fmt.Printf("Fail to parse url: %+v", err)
		os.Exit(1)
	}

	entries, err := embedFS.ReadDir("public")
	if err != nil {
		fmt.Printf("Fail to read public dir: %+v", err)
		os.Exit(1)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fp := entry.Name()
		proxyRoutes = append(proxyRoutes, fp)
	}
}

func devServerURLFromEnvironment() string {
	if devServerURL := os.Getenv("ROUTEX_VITE_DEV_SERVER_URL"); devServerURL != "" {
		return devServerURL
	}
	port := os.Getenv("ROUTEX_VITE_PORT")
	if port == "" {
		port = "5173"
	}
	return fmt.Sprintf("http://localhost:%s", port)
}

func newDevelopmentProxy(target *url.URL) *httputil.ReverseProxy {
	return &httputil.ReverseProxy{
		Rewrite: func(req *httputil.ProxyRequest) {
			req.SetURL(target)
			req.Out.Host = req.In.Host
			req.SetXForwarded()
			req.Out.Header.Set("X-Origin-Host", target.Host)
		},
	}
}

// EmbedAssets proxies requests to the Vite dev server in development mode.
func EmbedAssets(router *fox.Engine) {
	proxy := newDevelopmentProxy(origin)

	proxyHandler := func(c *fox.Context) {
		proxy.ServeHTTP(c.Writer, c.Request)
	}

	for _, path := range proxyRoutes {
		router.GET(path, proxyHandler)
		router.HEAD(path, proxyHandler)
	}

	router.NotFound(func(c *fox.Context) any {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			return httperrors.ErrNotFound
		}

		if strings.HasPrefix(c.Request.URL.Path, "/api") {
			return httperrors.ErrNotFound
		}

		c.Logger.Debugf("NotFound, use proxy: %s", c.Request.URL)
		proxy.ServeHTTP(c.Writer, c.Request)

		return nil
	})
}
