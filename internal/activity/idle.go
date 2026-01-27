package activity

import (
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32DLL           = windows.NewLazySystemDLL("user32.dll")
	procGetLastInputInfo = user32DLL.NewProc("GetLastInputInfo")
)

// LASTINPUTINFO structure for Windows API
type LASTINPUTINFO struct {
	CbSize uint32
	DwTime uint32
}

// IdleDetector detects when the system is idle
type IdleDetector struct {
	idleTimeout    time.Duration
	lastInputTime  time.Time
	lastCheckTime  time.Time
}

// NewIdleDetector creates a new idle detector
func NewIdleDetector(idleTimeout time.Duration) *IdleDetector {
	return &IdleDetector{
		idleTimeout:   idleTimeout,
		lastInputTime: time.Now(),
		lastCheckTime: time.Now(),
	}
}

// IsIdle returns true if the system has been idle for longer than the timeout
func (id *IdleDetector) IsIdle() bool {
	currentTime := time.Now()
	lastInput := id.getLastInputTime()
	
	// Update our tracking
	if !lastInput.Equal(id.lastInputTime) {
		id.lastInputTime = lastInput
	}
	
	id.lastCheckTime = currentTime
	
	// Check if idle time exceeds timeout
	idleDuration := currentTime.Sub(lastInput)
	return idleDuration >= id.idleTimeout
}

// GetIdleDuration returns how long the system has been idle
func (id *IdleDetector) GetIdleDuration() time.Duration {
	return time.Since(id.getLastInputTime())
}

// GetLastActivityTime returns the time of last user activity
func (id *IdleDetector) GetLastActivityTime() time.Time {
	return id.getLastInputTime()
}

// getLastInputTime gets the last input time from Windows API
func (id *IdleDetector) getLastInputTime() time.Time {
	var lastInputInfo LASTINPUTINFO
	lastInputInfo.CbSize = uint32(unsafe.Sizeof(lastInputInfo))
	
	ret, _, _ := procGetLastInputInfo.Call(uintptr(unsafe.Pointer(&lastInputInfo)))
	if ret == 0 {
		// Failed to get last input info, return current time
		return time.Now()
	}
	
	// GetTickCount to get current tick count
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procGetTickCount := kernel32.NewProc("GetTickCount")
	currentTick, _, _ := procGetTickCount.Call()
	
	// Calculate idle time in milliseconds
	idleTime := uint32(currentTick) - lastInputInfo.DwTime
	
	// Convert to time.Time
	return time.Now().Add(-time.Duration(idleTime) * time.Millisecond)
}

// SetIdleTimeout updates the idle timeout
func (id *IdleDetector) SetIdleTimeout(timeout time.Duration) {
	id.idleTimeout = timeout
}