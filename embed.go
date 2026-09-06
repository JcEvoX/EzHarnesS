/*
前端产物嵌入单二进制（wails3 task build / dev.bat 都先 npm run build
产出 frontend/dist 再编译；目录缺失则 go build 直接报错）。
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
