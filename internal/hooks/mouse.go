package hooks

import (
	"sync"
	"time"

	"desktime-tracker/internal/models"

	"github.com/sirupsen/logrus"
)

// ActivityCallback is called when activity is detected
type ActivityCallback func()

// MouseHook tracks mouse activity
type MouseHook struct {
	mouseInfo      *models.MouseInfo
	stats          *MouseStats
	mu             sync.RWMutex
	running        bool
	stopChannel    chan bool
	onActivity     ActivityCallback
	activityDetector *ActivityDetector  // Windows activity detector
}

// MouseStats holds mouse statistics
type MouseStats struct {
	TotalClicks    int
	TotalDistance  int
	LastActivity   time.Time
	SessionClicks  int
	SessionDistance int
	StartTime      time.Time
	mu             sync.RWMutex
}

// NewMouseHook creates a new mouse hook
func NewMouseHook() (*MouseHook, error) {
	mh := &MouseHook{
		mouseInfo: &models.MouseInfo{},
		stats: &MouseStats{
			StartTime: time.Now(),
		},
		stopChannel: make(chan bool, 1),
	}
	
	// Create Windows activity detector
	mh.activityDetector = NewActivityDetector()
	
	return mh, nil
}

// SetActivityCallback sets the callback for when activity is detected
func (mh *MouseHook) SetActivityCallback(callback ActivityCallback) {
	mh.mu.Lock()
	defer mh.mu.Unlock()
	mh.onActivity = callback
	
	// Also set on the Windows detector if available
	if mh.activityDetector != nil {
		mh.activityDetector.SetActivityCallback(callback)
	}
}

// Start starts the mouse hook
func (mh *MouseHook) Start() error {
	mh.mu.Lock()
	if mh.running {
		mh.mu.Unlock()
		return nil
	}
	mh.running = true
	mh.mu.Unlock()

	logrus.Info("Starting mouse hook")
	
	// Set activity callback on the detector
	if mh.activityDetector != nil && mh.onActivity != nil {
		mh.activityDetector.SetActivityCallback(mh.onActivity)
	}
	
	// Start Windows activity detector
	if mh.activityDetector != nil {
		if err := mh.activityDetector.Start(); err != nil {
			logrus.Errorf("Failed to start activity detector: %v", err)
			// Fallback to simulated tracking
			go mh.trackMouse()
		}
	} else {
		// Fallback to simulated tracking
		go mh.trackMouse()
	}
	
	return nil
}

// Stop stops the mouse hook
func (mh *MouseHook) Stop() {
	mh.mu.Lock()
	if !mh.running {
		mh.mu.Unlock()
		return
	}
	mh.running = false
	mh.mu.Unlock()

	logrus.Info("Stopping mouse hook")
	
	// Stop Windows activity detector
	if mh.activityDetector != nil {
		mh.activityDetector.Stop()
	}
	
	// Signal stop
	select {
	case mh.stopChannel <- true:
	default:
	}
}

// trackMouse tracks mouse movements and clicks (simplified implementation)
func (mh *MouseHook) trackMouse() {
	ticker := time.NewTicker(100 * time.Millisecond) // Track every 100ms
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			mh.updateMousePosition()
		case <-mh.stopChannel:
			return
		}
	}
}

// updateMousePosition updates mouse position and calculates movement
func (mh *MouseHook) updateMousePosition() {
	// In a real implementation, this would use Windows API to get actual mouse position
	// For now, we'll simulate some activity
	mh.mu.Lock()
	callback := mh.onActivity
	mh.mu.Unlock()

	currentTime := time.Now()
	
	// Simulate mouse movement (in real implementation, get actual cursor position)
	newX := mh.mouseInfo.X + 1 // Simplified simulation
	newY := mh.mouseInfo.Y + 1
	
	// Calculate distance moved
	mh.mu.Lock()
	if mh.mouseInfo.X != 0 || mh.mouseInfo.Y != 0 {
		deltaX := newX - mh.mouseInfo.X
		deltaY := newY - mh.mouseInfo.Y
		distance := int(float64(deltaX*deltaX + deltaY*deltaY))
		
		if distance > 0 {
			mh.mouseInfo.DistanceMoved += distance
			
			mh.stats.mu.Lock()
			mh.stats.TotalDistance += distance
			mh.stats.SessionDistance += distance
			mh.stats.LastActivity = currentTime
			mh.stats.mu.Unlock()
			
			// Call activity callback if movement detected
			if callback != nil {
				go callback()
			}
		}
	}
	
	mh.mouseInfo.LastX = mh.mouseInfo.X
	mh.mouseInfo.LastY = mh.mouseInfo.Y
	mh.mouseInfo.X = newX
	mh.mouseInfo.Y = newY
	mh.mu.Unlock()
}

// OnMouseClick simulates a mouse click event (would be called by actual hook)
func (mh *MouseHook) OnMouseClick() {
	mh.mu.Lock()
	callback := mh.onActivity
	mh.mouseInfo.ClickCount++
	mh.mu.Unlock()
	
	mh.stats.mu.Lock()
	mh.stats.TotalClicks++
	mh.stats.SessionClicks++
	mh.stats.LastActivity = time.Now()
	mh.stats.mu.Unlock()
	
	logrus.Debugf("Mouse click detected, total: %d", mh.mouseInfo.ClickCount)
	
	// Call activity callback
	if callback != nil {
		go callback()
	}
}

// GetStats returns mouse statistics
func (mh *MouseHook) GetStats() map[string]interface{} {
	mh.stats.mu.RLock()
	defer mh.stats.mu.RUnlock()

	return map[string]interface{}{
		"total_clicks":     mh.stats.TotalClicks,
		"total_distance":   mh.stats.TotalDistance,
		"session_clicks":   mh.stats.SessionClicks,
		"session_distance": mh.stats.SessionDistance,
		"last_activity":    mh.stats.LastActivity,
		"uptime_seconds":   int(time.Since(mh.stats.StartTime).Seconds()),
	}
}

// GetCurrentInfo returns current mouse information
func (mh *MouseHook) GetCurrentInfo() *models.MouseInfo {
	mh.mu.RLock()
	defer mh.mu.RUnlock()
	
	// Return a copy to avoid race conditions
	return &models.MouseInfo{
		X:             mh.mouseInfo.X,
		Y:             mh.mouseInfo.Y,
		LastX:         mh.mouseInfo.LastX,
		LastY:         mh.mouseInfo.LastY,
		ClickCount:    mh.mouseInfo.ClickCount,
		DistanceMoved: mh.mouseInfo.DistanceMoved,
	}
}

// ResetSessionStats resets session statistics
func (mh *MouseHook) ResetSessionStats() {
	mh.stats.mu.Lock()
	defer mh.stats.mu.Unlock()
	
	mh.stats.SessionClicks = 0
	mh.stats.SessionDistance = 0
	mh.stats.StartTime = time.Now()
}