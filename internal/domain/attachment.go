/*
attachment 是用户上传附件的传输载体：前端把文件读成 base64 经
HTTP 送入，服务层落盘到工作目录 tmp/ 后只保留路径——base64 不进
上下文、不入会话历史（模型经 <upload_file> 记录按需 read_file）。
*/
package domain

/* Attachment 是一条待落盘的用户附件。 */
type Attachment struct {
	Name     string // 原始文件名（落盘时做净化处理）
	MimeType string // 前端探测的 MIME（记录用，落盘按魔数无关）
	Data     string // base64 编码内容（不 含 data: 前缀）
}
