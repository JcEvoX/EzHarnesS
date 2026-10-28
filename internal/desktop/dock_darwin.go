package desktop

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void ezSetDockVisible(int visible) {
	void (^apply)(void) = ^{
		if (visible) {
			[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
			[NSApp activateIgnoringOtherApps:YES];
		} else {
			[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
		}
	};
	if ([NSThread isMainThread]) {
		apply();
	} else {
		dispatch_sync(dispatch_get_main_queue(), apply);
	}
}
*/
import "C"

// SetDockVisible 切换 macOS 程序坞图标：仅托盘常驻时用 Accessory 策略
// 隐藏图标，窗口恢复前切回 Regular。
func SetDockVisible(visible bool) {
	v := C.int(0)
	if visible {
		v = 1
	}
	C.ezSetDockVisible(v)
}
