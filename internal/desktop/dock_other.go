//go:build !darwin

package desktop

// SetDockVisible 在非 macOS 平台无程序坞概念，空实现。
func SetDockVisible(bool) {}
