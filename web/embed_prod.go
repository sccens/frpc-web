//go:build embed

package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var dist embed.FS

func FileSystem() http.FileSystem {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	return http.FS(sub)
}
