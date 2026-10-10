//go:build webui

package main

import (
	"net/http"

	"github.com/omgitsmoe/coldcat/frontend"
	"github.com/omgitsmoe/coldcat/internal/httpapi"
)

func defaultAssetHandler(backend http.Handler) (http.Handler, error) {
	assets, err := frontend.Assets()
	if err != nil {
		return nil, err
	}

	return httpapi.WithAssetFS(backend, assets)
}
