package activity

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

var idleLogFile *os.File

func init() {
	// Create idle debug log file
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		appData = os.Getenv("APPDATA")
	}
	if appData != "" {
		logPath := filepath.Join(appData, "KTracker", "idle_debug.log")
		os.MkdirAll(filepath.Dir(logPath), 0755)
		var err error
		idleLogFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			logrus.Warnf("Failed to create idle debug log: %v", err)
		}
	}
}

func logIdle(format string, args ...interface{}) {
	if idleLogFile != nil {
		msg := fmt.Sprintf(format, args...)
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		idleLogFile.WriteString(fmt.Sprintf("%s | %s\n", timestamp, msg))
	}
}

var (
	user32DLL            = windows.NewLazySystemDLL("user32.dll")
	procGetLastInputInfo = user32DLL.NewProc("GetLastInputInfo")
	kernel32DLL          = windows.NewLazySystemDLL("kernel32.dll")
	procGetTickCount     = kernel32DLL.NewProc("GetTickCount")
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
	logrus.Infof("IdleDetector created with timeout: %v", idleTimeout)
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
	isIdle := idleDuration >= id.idleTimeout

	// Log every call to debug file (all in seconds)
	logIdle("lastInput=%s | idleDuration=%ds | idleTimeout=%ds | isIdle=%v",
		lastInput.Format("15:04:05"), int(idleDuration.Seconds()), int(id.idleTimeout.Seconds()), isIdle)

	return isIdle
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
		// Failed to get last input info, return current time (user considered active)
		logrus.Warn("GetLastInputInfo failed, assuming user is active")
		return time.Now()
	}

	// GetTickCount to get current tick count
	currentTick, _, _ := procGetTickCount.Call()

	// Calculate idle time in milliseconds (uint32 handles overflow correctly)
	idleTime := uint32(currentTick) - lastInputInfo.DwTime

	// Convert to time.Time
	return time.Now().Add(-time.Duration(idleTime) * time.Millisecond)
}

// SetIdleTimeout updates the idle timeout
func (id *IdleDetector) SetIdleTimeout(timeout time.Duration) {
	logrus.Infof("IdleDetector timeout updated: %v -> %v", id.idleTimeout, timeout)
	id.idleTimeout = timeout
}