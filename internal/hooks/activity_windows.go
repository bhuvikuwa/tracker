// +build windows

package hooks

import (
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
)

const (
	WH_KEYBOARD_LL = 13
	WH_MOUSE_LL    = 14
	WM_KEYDOWN     = 0x0100
	WM_SYSKEYDOWN  = 0x0104
	WM_MOUSEMOVE   = 0x0200
	WM_LBUTTONDOWN = 0x0201
	WM_RBUTTONDOWN = 0x0204
	WM_MBUTTONDOWN = 0x0207
	WM_MOUSEWHEEL  = 0x020A
)

type POINT struct {
	X, Y int32
}

type MSLLHOOKSTRUCT struct {
	Pt          POINT
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procSetWindowsHookEx    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetMessage          = user32.NewProc("GetMessageW")
	procPostThreadMessage   = user32.NewProc("PostThreadMessageW")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procGetModuleHandle     = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadId  = kernel32.NewProc("GetCurrentThreadId")
)

const WM_QUIT = 0x0012

// ActivityDetector tracks real mouse and keyboard activity on Windows
type ActivityDetector struct {
	mouseHook     uintptr
	keyboardHook  uintptr
	threadID      uint32 // OS thread ID of the hook goroutine (for posting WM_QUIT)
	lastActivity  int64 // Unix nano timestamp (atomic)
	onActivity    atomic.Value // stores ActivityCallback
	running       bool
	mu            sync.RWMutex
	lastMouseX    int32 // atomic
	lastMouseY    int32 // atomic
	mouseDistance int32 // atomic
	clickCount    int32 // atomic
	keyCount      int32 // atomic
}

// NewActivityDetector creates a new Windows activity detector
func NewActivityDetector() *ActivityDetector {
	ad := &ActivityDetector{}
	atomic.StoreInt64(&ad.lastActivity, time.Now().UnixNano())
	return ad
}

// SetActivityCallback sets the callback for activity detection
func (ad *ActivityDetector) SetActivityCallback(callback ActivityCallback) {
	ad.onActivity.Store(callback)
}

// Start starts the activity detection
func (ad *ActivityDetector) Start() error {
	ad.mu.Lock()
	if ad.running {
		ad.mu.Unlock()
		return nil
	}
	ad.running = true
	ad.mu.Unlock()

	logrus.Info("Starting Windows activity detector")
	
	// Start hook in a separate goroutine
	go ad.installHooks()
	
	return nil
}

// Stop stops the activity detection
func (ad *ActivityDetector) Stop() {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	if !ad.running {
		return
	}

	ad.running = false

	if ad.mouseHook != 0 {
		procUnhookWindowsHookEx.Call(ad.mouseHook)
		ad.mouseHook = 0
	}

	if ad.keyboardHook != 0 {
		procUnhookWindowsHookEx.Call(ad.keyboardHook)
		ad.keyboardHook = 0
	}

	// Post WM_QUIT to the hook thread to unblock GetMessage
	if ad.threadID != 0 {
		procPostThreadMessage.Call(uintptr(ad.threadID), WM_QUIT, 0, 0)
	}

	logrus.Info("Stopped Windows activity detector")
}

// installHooks installs the Windows hooks
func (ad *ActivityDetector) installHooks() {
	// CRITICAL: Lock this goroutine to the current OS thread.
	// Windows hooks require the message pump to run on the same thread
	// that installed the hooks. Without this, Go may migrate the goroutine
	// to a different OS thread, causing hooks to stop working or hang.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Store thread ID so Stop() can post WM_QUIT to unblock GetMessage
	tid, _, _ := procGetCurrentThreadId.Call()
	ad.mu.Lock()
	ad.threadID = uint32(tid)
	ad.mu.Unlock()

	// Get module handle
	hMod, _, _ := procGetModuleHandle.Call(0)
	
	// Install mouse hook
	mouseHook, _, err := procSetWindowsHookEx.Call(
		WH_MOUSE_LL,
		syscall.NewCallback(ad.mouseProc),
		hMod,
		0,
	)
	
	if mouseHook == 0 {
		logrus.Errorf("Failed to install mouse hook: %v", err)
	} else {
		ad.mu.Lock()
		ad.mouseHook = mouseHook
		ad.mu.Unlock()
		logrus.Info("Mouse hook installed successfully")
	}
	
	// Install keyboard hook
	keyboardHook, _, err := procSetWindowsHookEx.Call(
		WH_KEYBOARD_LL,
		syscall.NewCallback(ad.keyboardProc),
		hMod,
		0,
	)
	
	if keyboardHook == 0 {
		logrus.Errorf("Failed to install keyboard hook: %v", err)
	} else {
		ad.mu.Lock()
		ad.keyboardHook = keyboardHook
		ad.mu.Unlock()
		logrus.Info("Keyboard hook installed successfully")
	}
	
	// Message loop
	var msg struct {
		HWND   uintptr
		Msg    uint32
		WParam uintptr
		LParam uintptr
		Time   uint32
		Pt     POINT
	}
	
	for ad.running {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 || ret == ^uintptr(0) {
			break
		}
	}
}

// mouseProc processes mouse events
// IMPORTANT: This is a low-level hook callback - must return quickly to avoid mouse hanging
func (ad *ActivityDetector) mouseProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		info := (*MSLLHOOKSTRUCT)(unsafe.Pointer(lParam))

		// Load callback once - no mutex, use atomic
		var callback ActivityCallback
		if cb := ad.onActivity.Load(); cb != nil {
			callback = cb.(ActivityCallback)
		}

		switch wParam {
		case WM_MOUSEMOVE:
			// Calculate distance moved using atomic loads
			lastX := atomic.LoadInt32(&ad.lastMouseX)
			lastY := atomic.LoadInt32(&ad.lastMouseY)

			if lastX != 0 || lastY != 0 {
				deltaX := info.Pt.X - lastX
				deltaY := info.Pt.Y - lastY
				distance := int32(deltaX*deltaX + deltaY*deltaY)

				if distance > 100 { // Only count significant movements
					atomic.AddInt32(&ad.mouseDistance, distance)
					atomic.StoreInt64(&ad.lastActivity, time.Now().UnixNano())

					if callback != nil {
						go callback()
					}
				}
			}
			atomic.StoreInt32(&ad.lastMouseX, info.Pt.X)
			atomic.StoreInt32(&ad.lastMouseY, info.Pt.Y)

		case WM_LBUTTONDOWN, WM_RBUTTONDOWN, WM_MBUTTONDOWN:
			atomic.AddInt32(&ad.clickCount, 1)
			atomic.StoreInt64(&ad.lastActivity, time.Now().UnixNano())

			if callback != nil {
				go callback()
			}

		case WM_MOUSEWHEEL:
			atomic.StoreInt64(&ad.lastActivity, time.Now().UnixNano())

			if callback != nil {
				go callback()
			}
		}
	}

	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

// keyboardProc processes keyboard events
// IMPORTANT: This is a low-level hook callback - must return quickly to avoid keyboard hanging
func (ad *ActivityDetector) keyboardProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		if wParam == WM_KEYDOWN || wParam == WM_SYSKEYDOWN {
			atomic.AddInt32(&ad.keyCount, 1)
			atomic.StoreInt64(&ad.lastActivity, time.Now().UnixNano())

			// Load callback using atomic
			if cb := ad.onActivity.Load(); cb != nil {
				go cb.(ActivityCallback)()
			}
		}
	}

	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

// GetLastActivity returns the last activity time
func (ad *ActivityDetector) GetLastActivity() time.Time {
	nanos := atomic.LoadInt64(&ad.lastActivity)
	return time.Unix(0, nanos)
}

// GetStats returns activity statistics
func (ad *ActivityDetector) GetStats() map[string]interface{} {
	lastActivityTime := ad.GetLastActivity()

	return map[string]interface{}{
		"clicks":        atomic.LoadInt32(&ad.clickCount),
		"keys":          atomic.LoadInt32(&ad.keyCount),
		"distance":      atomic.LoadInt32(&ad.mouseDistance),
		"last_activity": lastActivityTime,
		"is_active":     time.Since(lastActivityTime) < 10*time.Second,
	}
}