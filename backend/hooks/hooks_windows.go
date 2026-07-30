//go:build windows

package hooks

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	whKeyboardLL  = 13
	whMouseLL     = 14
	hcAction      = 0
	wmKeyDown     = 0x0100
	wmSysKeyDown  = 0x0104
	wmLButtonDown = 0x0201
	wmRButtonDown = 0x0204
	vkF9          = 0x78
)

type point struct {
	X int32
	Y int32
}

type msg struct {
	HWnd     windows.Handle
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       point
	LPrivate uint32
}

type kbdllhookstruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type msllhookstruct struct {
	Pt          point
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type apmEvent struct {
	at time.Time
}

type APMTracker struct {
	current  atomic.Int64
	hooks    []uintptr
	stop     chan struct{}
	stopOnce sync.Once
	mu       sync.Mutex
	events   []apmEvent
}

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW      = user32.NewProc("GetWindowTextW")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
	hotkeyHandlerMu         sync.RWMutex
	keyboardHandler         func()
	mouseHandlerMu          sync.RWMutex
	mouseHandler            func()
	keyboardProcPtr         = windows.NewCallback(lowLevelKeyboardProc)
	mouseProcPtr            = windows.NewCallback(lowLevelMouseProc)
)

func RegisterLowLevelKeyboardHook(handler func()) (uintptr, error) {
	hotkeyHandlerMu.Lock()
	keyboardHandler = handler
	hotkeyHandlerMu.Unlock()
	return setWindowsHook(whKeyboardLL, keyboardProcPtr)
}

func RegisterLowLevelMouseHook(handler func()) (uintptr, error) {
	mouseHandlerMu.Lock()
	mouseHandler = handler
	mouseHandlerMu.Unlock()
	return setWindowsHook(whMouseLL, mouseProcPtr)
}

func UnhookWindowsHookEx(hook uintptr) error {
	if hook == 0 {
		return nil
	}
	r1, _, err := procUnhookWindowsHookEx.Call(hook)
	if r1 == 0 {
		return err
	}
	return nil
}

func GetForegroundWindowTitle() (string, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return "", nil
	}
	buf := make([]uint16, 512)
	n, _, err := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return "", err
	}
	return windows.UTF16ToString(buf[:n]), nil
}

func StartAPMTracker() *APMTracker {
	tracker := &APMTracker{stop: make(chan struct{})}
	kbHook, _ := RegisterLowLevelKeyboardHook(func() { tracker.recordEvent() })
	mouseHook, _ := RegisterLowLevelMouseHook(func() { tracker.recordEvent() })
	tracker.hooks = []uintptr{kbHook, mouseHook}

	go tracker.run()
	return tracker
}

func (t *APMTracker) CurrentAPM() int {
	if t == nil {
		return 0
	}
	return int(t.current.Load())
}

func (t *APMTracker) Stop() {
	if t == nil {
		return
	}
	t.stopOnce.Do(func() {
		close(t.stop)
		for _, hook := range t.hooks {
			_ = UnhookWindowsHookEx(hook)
		}
	})
}

func StartMessageLoop() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			return
		}
	}
}

func (t *APMTracker) recordEvent() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.events = append(t.events, apmEvent{at: time.Now()})
	t.mu.Unlock()
}

func (t *APMTracker) run() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-t.stop:
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-60 * time.Second)
			t.mu.Lock()
			kept := t.events[:0]
			for _, ev := range t.events {
				if ev.at.After(cutoff) {
					kept = append(kept, ev)
				}
			}
			t.events = kept
			t.current.Store(int64(len(t.events)))
			t.mu.Unlock()
		}
	}
}

func setWindowsHook(hookID int, callback uintptr) (uintptr, error) {
	module, _, _ := procGetModuleHandleW.Call(0)
	hook, _, err := procSetWindowsHookExW.Call(uintptr(hookID), callback, module, 0)
	if hook == 0 {
		return 0, err
	}
	return hook, nil
}

func lowLevelKeyboardProc(code int, wParam uintptr, lParam uintptr) uintptr {
	if code == hcAction && (wParam == wmKeyDown || wParam == wmSysKeyDown) {
		kbd := (*kbdllhookstruct)(unsafe.Pointer(lParam))
		if kbd != nil && kbd.VkCode == vkF9 {
			hotkeyHandlerMu.RLock()
			handler := keyboardHandler
			hotkeyHandlerMu.RUnlock()
			if handler != nil {
				handler()
			}
		}
	}
	next, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, lParam)
	return next
}

func lowLevelMouseProc(code int, wParam uintptr, lParam uintptr) uintptr {
	if code == hcAction && (wParam == wmLButtonDown || wParam == wmRButtonDown) {
		mouseHandlerMu.RLock()
		handler := mouseHandler
		mouseHandlerMu.RUnlock()
		if handler != nil {
			handler()
		}
	}
	next, _, _ := procCallNextHookEx.Call(0, uintptr(code), wParam, lParam)
	return next
}
