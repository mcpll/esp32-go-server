//go:build !embed_ui

package static

import "embed"

// FS 未启用 embed_ui 时为空，开发阶段不挂载前端静态资源
var FS = embed.FS{}
