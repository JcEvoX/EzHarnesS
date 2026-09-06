//go:build production

/*
production 模式：前端产物嵌入单二进制（wails3 build，-tags production）。
dev 模式不编译本文件，避免 frontend/dist 缺失报错。
*/
package main

import (
	"embed"
	"io/fs"
)

//go:embed frontend/dist
var dist embed.FS

/* distFS 返回以 frontend/dist 为根的静态资源。 */
func distFS() fs.FS {
	sub, err := fs.Sub(dist, "frontend/dist")
	if err != nil {
		return nil
	}
	return sub
}
