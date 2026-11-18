// Package webui embeds the Vite-built SPA (web/dist) for the control API.
// Populate dist via `make build` (npm run build → copy). A minimal
// dist/index.html is committed so `go test`/`go build` work without npm.
package webui

import "embed"

// DistFS is the embedded frontend tree; root entry is "dist/...".
//
//go:embed all:dist
var DistFS embed.FS
