package frontend

import (
	"context"
	"embed"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

	"github.com/usememos/memos/internal/profile"
	"github.com/usememos/memos/internal/util"
	"github.com/usememos/memos/store"
)

//go:embed dist/*
var embeddedFiles embed.FS

type FrontendService struct {
	Profile *profile.Profile
	Store   *store.Store
}

func NewFrontendService(profile *profile.Profile, store *store.Store) *FrontendService {
	return &FrontendService{
		Profile: profile,
		Store:   store,
	}
}

func (*FrontendService) Serve(_ context.Context, e *echo.Echo) {
	skipper := func(c *echo.Context) bool {
		requestPath := c.Request().URL.Path
		// Skip API routes.
		if util.HasPrefixes(requestPath, "/api", "/memos.api.v1") {
			return true
		}
		// Any SPA route that ultimately resolves to index.html must not be cached,
		// otherwise browsers/CDNs can keep stale HTML that references removed chunk filenames.
		if shouldDisableFrontendCache(requestPath) {
			c.Response().Header().Set(echo.HeaderCacheControl, "no-cache, no-store, must-revalidate")
			c.Response().Header().Set("Pragma", "no-cache")
			c.Response().Header().Set("Expires", "0")
			return false
		}
		// Set Cache-Control header for static assets.
		// Since Vite generates content-hashed filenames (e.g., index-BtVjejZf.js),
		// we can cache aggressively but use immutable to prevent revalidation checks.
		// For frequently redeployed instances, use shorter max-age (1 hour) to avoid
		// serving stale assets after redeployment.
		c.Response().Header().Set(echo.HeaderCacheControl, "public, max-age=3600, immutable") // 1 hour
		return false
	}

	// Route to serve the main app with HTML5 fallback for SPA behavior.
	e.Use(middleware.StaticWithConfig(middleware.StaticConfig{
		Filesystem: getFileSystem("dist"),
		HTML5:      true, // Enable fallback to index.html
		Skipper:    skipper,
	}))
}

func shouldDisableFrontendCache(requestPath string) bool {
	if requestPath == "" || requestPath == "/" {
		return true
	}

	ext := strings.ToLower(filepath.Ext(requestPath))
	if ext == "" || ext == ".html" {
		return true
	}
	return false
}

func getFileSystem(path string) fs.FS {
	sub, err := fs.Sub(embeddedFiles, path)
	if err != nil {
		panic(err)
	}
	return sub
}
