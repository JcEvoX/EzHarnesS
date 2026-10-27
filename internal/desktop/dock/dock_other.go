//go:build !darwin

package dock

// SetVisible 在非 macOS 平台无程序坞概念，空实现。
func SetVisible(bool) {}
