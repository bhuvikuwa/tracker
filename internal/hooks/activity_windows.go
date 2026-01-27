// +build windows

package hooks

import (
	"sync"
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
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procGetModuleHandle     = kernel32.NewProc("GetModuleHandleW")
)

// ActivityDetector tracks real mouse and keyboard activity on Windows
type ActivityDetector struct {
	mouseHook     uintptr
	keyboardHook  uintptr
	lastActivity  time.Time
	onActivity    ActivityCallback
	running       bool
	mu            sync.RWMutex
	lastMouseX    int32
	lastMouseY    int32
	mouseDistance int
	clickCount    int
	keyCount      int
}

// NewActivityDetector creates a new Windows activity detector
func NewActivityDetector() *ActivityDetector {
	return &ActivityDetector{
		lastActivity: time.Now(),
	}
}

// SetActivityCallback sets the callback for activity detection
func (ad *ActivityDetector) SetActivityCallback(callback ActivityCallback) {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.onActivity = callback
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
	
	logrus.Info("Stopped Windows activity detector")
}

// installHooks installs the Windows hooks
func (ad *ActivityDetector) installHooks() {
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
func (ad *ActivityDetector) mouseProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		info := (*MSLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		
		ad.mu.Lock()
		callback := ad.onActivity
		
		switch wParam {
		case WM_MOUSEMOVE:
			// Calculate distance moved
			if ad.lastMouseX != 0 || ad.lastMouseY != 0 {
				deltaX := info.Pt.X - ad.lastMouseX
				deltaY := info.Pt.Y - ad.lastMouseY
				distance := int(deltaX*deltaX + deltaY*deltaY)
				
				if distance > 100 { // Only count significant movements
					ad.mouseDistance += distance
					ad.lastActivity = time.Now()
					
					if callback != nil {
						go callback()
					}
				}
			}
			ad.lastMouseX = info.Pt.X
			ad.lastMouseY = info.Pt.Y
			
		case WM_LBUTTONDOWN, WM_RBUTTONDOWN, WM_MBUTTONDOWN:
			ad.clickCount++
			ad.lastActivity = time.Now()
			
			if callback != nil {
				go callback()
			}
			logrus.Debugf("Mouse click detected, total: %d", ad.clickCount)
			
		case WM_MOUSEWHEEL:
			ad.lastActivity = time.Now()
			
			if callback != nil {
				go callback()
			}
		}
		
		ad.mu.Unlock()
	}
	
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

// keyboardProc processes keyboard events
func (ad *ActivityDetector) keyboardProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		if wParam == WM_KEYDOWN || wParam == WM_SYSKEYDOWN {
			ad.mu.Lock()
			ad.keyCount++
			ad.lastActivity = time.Now()
			callback := ad.onActivity
			ad.mu.Unlock()
			
			if callback != nil {
				go callback()
			}
			
			logrus.Debugf("Key press detected, total: %d", ad.keyCount)
		}
	}
	
	ret, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return ret
}

// GetLastActivity returns the last activity time
func (ad *ActivityDetector) GetLastActivity() time.Time {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	return ad.lastActivity
}

// GetStats returns activity statistics
func (ad *ActivityDetector) GetStats() map[string]interface{} {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	
	return map[string]interface{}{
		"clicks":        ad.clickCount,
		"keys":          ad.keyCount,
		"distance":      ad.mouseDistance,
		"last_activity": ad.lastActivity,
		"is_active":     time.Since(ad.lastActivity) < 10*time.Second,
	}
}