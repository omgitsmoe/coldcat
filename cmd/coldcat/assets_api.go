//go:build !webui

package main

import "net/http"

func defaultAssetHandler(backend http.Handler) (http.Handler, error) {
	return backend, nil
}
