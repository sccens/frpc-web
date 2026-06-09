//go:build !embed

package webui

import "net/http"

func FileSystem() http.FileSystem {
	return nil
}
