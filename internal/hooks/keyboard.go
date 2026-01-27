package hooks

import (
	"sync"
	"time"

	"desktime-tracker/internal/models"

	"github.com/sirupsen/logrus"
)

// KeyboardHook tracks keyboard activity
type KeyboardHook struct {
	keyboardInfo *models.KeyboardInfo
	stats        *KeyboardStats
	mu           sync.RWMutex
	running      bool
	stopChannel  chan bool
	onActivity   ActivityCallback
}

// KeyboardStats holds keyboard statistics
type KeyboardStats struct {
	TotalKeystrokes   int
	SessionKeystrokes int
	LastActivity      time.Time
	StartTime         time.Time
	mu                sync.RWMutex
}

// NewKeyboardHook creates a new keyboard hook
func NewKeyboardHook() (*KeyboardHook, error) {
	return &KeyboardHook{
		keyboardInfo: &models.KeyboardInfo{},
		stats: &KeyboardStats{
			StartTime: time.Now(),
		},
		stopChannel: make(chan bool, 1),
	}, nil
}

// SetActivityCallback sets the callback for when activity is detected
func (kh *KeyboardHook) SetActivityCallback(callback ActivityCallback) {
	kh.mu.Lock()
	defer kh.mu.Unlock()
	kh.onActivity = callback
}

// Start starts the keyboard hook
func (kh *KeyboardHook) Start() error {
	kh.mu.Lock()
	if kh.running {
		kh.mu.Unlock()
		return nil
	}
	kh.running = true
	kh.mu.Unlock()

	logrus.Info("Starting keyboard hook")
	
	// Start keyboard tracking
	go kh.trackKeyboard()
	
	return nil
}

// Stop stops the keyboard hook
func (kh *KeyboardHook) Stop() {
	kh.mu.Lock()
	if !kh.running {
		kh.mu.Unlock()
		return
	}
	kh.running = false
	kh.mu.Unlock()

	logrus.Info("Stopping keyboard hook")
	
	// Signal stop
	select {
	case kh.stopChannel <- true:
	default:
	}
}

// trackKeyboard tracks keyboard activity (simplified implementation)
func (kh *KeyboardHook) trackKeyboard() {
	ticker := time.NewTicker(500 * time.Millisecond) // Check every 500ms
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			kh.simulateKeyboardActivity()
		case <-kh.stopChannel:
			return
		}
	}
}

// simulateKeyboardActivity simulates keyboard activity for demo purposes
func (kh *KeyboardHook) simulateKeyboardActivity() {
	// In a real implementation, this would be triggered by actual keyboard events
	// For demo purposes, we'll occasionally increment the keystroke count
	if time.Now().Second()%10 == 0 { // Simulate activity every 10 seconds
		kh.OnKeyPress()
	}
}

// OnKeyPress handles a key press event (would be called by actual hook)
func (kh *KeyboardHook) OnKeyPress() {
	kh.mu.Lock()
	callback := kh.onActivity
	kh.mu.Unlock()

	currentTime := time.Now()
	
	kh.mu.Lock()
	kh.keyboardInfo.KeystrokeCount++
	kh.keyboardInfo.LastActivity = currentTime
	kh.mu.Unlock()
	
	kh.stats.mu.Lock()
	kh.stats.TotalKeystrokes++
	kh.stats.SessionKeystrokes++
	kh.stats.LastActivity = currentTime
	kh.stats.mu.Unlock()
	
	logrus.Debugf("Key press detected, total: %d", kh.keyboardInfo.KeystrokeCount)
	
	// Call activity callback
	if callback != nil {
		go callback()
	}
}

// GetStats returns keyboard statistics
func (kh *KeyboardHook) GetStats() map[string]interface{} {
	kh.stats.mu.RLock()
	defer kh.stats.mu.RUnlock()

	return map[string]interface{}{
		"total_keystrokes":   kh.stats.TotalKeystrokes,
		"session_keystrokes": kh.stats.SessionKeystrokes,
		"last_activity":      kh.stats.LastActivity,
		"uptime_seconds":     int(time.Since(kh.stats.StartTime).Seconds()),
	}
}

// GetCurrentInfo returns current keyboard information
func (kh *KeyboardHook) GetCurrentInfo() *models.KeyboardInfo {
	kh.mu.RLock()
	defer kh.mu.RUnlock()
	
	// Return a copy to avoid race conditions
	return &models.KeyboardInfo{
		KeystrokeCount: kh.keyboardInfo.KeystrokeCount,
		LastActivity:   kh.keyboardInfo.LastActivity,
	}
}

// ResetSessionStats resets session statistics
func (kh *KeyboardHook) ResetSessionStats() {
	kh.stats.mu.Lock()
	defer kh.stats.mu.Unlock()
	
	kh.stats.SessionKeystrokes = 0
	kh.stats.StartTime = time.Now()
}