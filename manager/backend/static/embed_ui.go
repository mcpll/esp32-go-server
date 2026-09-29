//go:build embed_ui

package static

import "embed"

//go:embed dist/*
var FS embed.FS
