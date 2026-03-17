package activity

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"ktracker/internal/config"
	"ktracker/internal/models"
	"ktracker/internal/storage"
	"ktracker/pkg/windows"

	"github.com/sirupsen/logrus"
)

// MouseHookInterface defines the interface for mouse hooks
type MouseHookInterface interface {
	GetStats() map[string]interface{}
	ResetSessionStats()
}

// KeyboardHookInterface defines the interface for keyboard hooks
type KeyboardHookInterface interface {
	GetStats() map[string]interface{}
	ResetSessionStats()
}

// ActivityCallback is called when activity status changes
type ActivityCallback func(isActive bool)

// Tracker handles activity tracking
type Tracker struct {
	cfg           *config.Config
	db            *storage.Database
	windowTracker *windows.WindowTracker
	browserMgr    *BrowserManager
	idleDetector    *IdleDetector
	meetingDetector *MeetingDetector
	currentApp      *models.Activity
	mu            sync.RWMutex
	stats         *Stats
	lastActivity  time.Time
	onActivityChange ActivityCallback
	isActive      bool
	mouseHook     MouseHookInterface
	keyboardHook  KeyboardHookInterface
}

// Stats holds tracker statistics
type Stats struct {
	StartTime     time.Time
	TotalSessions int
	TotalDuration time.Duration
	mu            sync.RWMutex
}

// NewTracker creates a new activity tracker
func NewTracker(cfg *config.Config, db *storage.Database) (*Tracker, error) {
	windowTracker, err := windows.NewWindowTracker()
	if err != nil {
		return nil, fmt.Errorf("failed to create window tracker: %w", err)
	}

	browserMgr := NewBrowserManager(cfg)
	
	idleDetector := NewIdleDetector(time.Duration(cfg.Tracking.IdleTimeout) * time.Second)

	meetingDetector := NewMeetingDetector()

	return &Tracker{
		cfg:             cfg,
		db:              db,
		windowTracker:   windowTracker,
		browserMgr:      browserMgr,
		idleDetector:    idleDetector,
		meetingDetector: meetingDetector,
		lastActivity:  time.Now().Add(-15 * time.Second), // Start as inactive (last activity 15 seconds ago)
		isActive:      false, // Start as inactive
		stats: &Stats{
			StartTime: time.Now(),
		},
	}, nil
}

// SetActivityCallback sets the callback function for activity status changes
func (t *Tracker) SetActivityCallback(callback ActivityCallback) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onActivityChange = callback
}

// SetInputHooks sets the mouse and keyboard hooks for activity tracking
func (t *Tracker) SetInputHooks(mouseHook MouseHookInterface, keyboardHook KeyboardHookInterface) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.mouseHook = mouseHook
	t.keyboardHook = keyboardHook
}

// OnUserActivity should be called when mouse/keyboard activity is detected
func (t *Tracker) OnUserActivity() {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	previousActivity := t.lastActivity
	t.lastActivity = time.Now()
	
	// If was inactive and now has activity, update status immediately
	if !t.isActive {
		t.isActive = true
		if t.onActivityChange != nil {
			go t.onActivityChange(true)
		}
		logrus.Infof("User activity detected - status changed to ACTIVE (was idle for %v)", time.Since(previousActivity))
	}
}

// Start starts the activity tracker
func (t *Tracker) Start(ctx context.Context) error {
	logrus.Info("Starting activity tracker")

	ticker := time.NewTicker(time.Duration(t.cfg.Tracking.ActivityInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			t.trackCurrentActivity()
		case <-ctx.Done():
			t.finalizePendingActivity()
			t.browserMgr.Cleanup()
			logrus.Info("Activity tracker stopped")
			return nil
		}
	}
}

// trackCurrentActivity tracks the currently active application
func (t *Tracker) trackCurrentActivity() {
	// Get active window information
	windowInfo, err := t.windowTracker.GetActiveWindow()
	if err != nil {
		logrus.Debugf("Failed to get active window: %v", err)
		return
	}

	if windowInfo == nil {
		return
	}

	// Skip tracking for Windows lock screen and system apps that indicate user is away
	processNameLower := strings.ToLower(windowInfo.ProcessName)
	if processNameLower == "lockapp.exe" || processNameLower == "logonui.exe" {
		logrus.Infof("LOCK SCREEN detected: %s - finalizing activity", windowInfo.ProcessName)
		// Finalize any current activity since user locked the system
		if t.currentApp != nil {
			t.mu.Lock()
			lastActiveTime := t.lastActivity
			t.mu.Unlock()
			logrus.Infof("FINALIZING due to LOCK SCREEN: app=%s, lastActivity=%v",
				t.currentApp.AppName, lastActiveTime.Format("15:04:05"))
			t.finalizeActivityAtTime(lastActiveTime)
		}
		return
	}

	// Log current active window for debugging
	logrus.Debugf("Active window: %s - %s", windowInfo.ProcessName, windowInfo.WindowTitle)

	// Get browser information BEFORE idle check (needed for meeting detection)
	var browserURL, browserTitle string
	if t.browserMgr.IsBrowser(windowInfo.ProcessName) {
		url, title := t.browserMgr.GetBrowserInfo(windowInfo.Handle, int32(windowInfo.ProcessID), windowInfo.ProcessName, windowInfo.WindowTitle)
		browserURL = url
		browserTitle = title
	}

	// Check if the user is in a meeting — if so, skip idle detection entirely
	isMeeting := t.meetingDetector.IsMeeting(windowInfo.ProcessName, windowInfo.WindowTitle, browserURL)

	var idleDuration time.Duration
	var isUserIdle, isUserActive bool

	if isMeeting {
		// Meeting detected — force active, skip idle check
		isUserIdle = false
		isUserActive = true
		logrus.Debugf("Meeting detected (%s), skipping idle check", windowInfo.ProcessName)
	} else {
		// Normal idle detection
		idleDuration = t.idleDetector.GetIdleDuration()
		isUserIdle = t.idleDetector.IsIdle()
		isUserActive = !isUserIdle
	}

	t.meetingDetector.SetWasMeeting(isMeeting)

	// Debug: Log idle check every time (to trace the issue)
	logrus.Debugf("IdleCheck: duration=%v, isIdle=%v, isMeeting=%v, hasCurrentApp=%v", idleDuration, isUserIdle, isMeeting, t.currentApp != nil)

	// Check if activity status has changed
	t.mu.Lock()
	if t.isActive != isUserActive {
		t.isActive = isUserActive
		if t.onActivityChange != nil {
			go t.onActivityChange(isUserActive)
		}
		if isUserActive {
			logrus.Infof("Activity status changed to ACTIVE")
		} else {
			logrus.Infof("Activity status changed to IDLE (no input for %v)", idleDuration)
		}
	}
	t.mu.Unlock()

	// If user is idle (no recent input), finalize current activity and STOP
	// Do not track anything until user provides input again
	if !isUserActive {
		if t.currentApp != nil {
			// End activity at the time of last user input, not now
			lastActiveTime := t.idleDetector.GetLastActivityTime()
			logrus.Infof("FINALIZING due to IDLE: app=%s, idleDuration=%v, lastActiveTime=%v",
				t.currentApp.AppName, idleDuration, lastActiveTime.Format("15:04:05"))
			t.finalizeActivityAtTime(lastActiveTime)
		}
		// STOP - don't track anything until user provides input
		return
	}

	currentTime := time.Now()

	t.mu.Lock()
	defer t.mu.Unlock()

	// Check if URL changed for browser windows
	urlChanged := false
	if t.currentApp != nil && t.browserMgr.IsBrowser(windowInfo.ProcessName) && browserURL != "" {
		currentURL := ""
		if t.currentApp.BrowserURL != nil {
			currentURL = *t.currentApp.BrowserURL
		}
		urlChanged = (currentURL != browserURL)
	}

	// Track if we're ending an activity due to window change (for seamless transition)
	var previousActivityEndTime time.Time
	endingDueToWindowChange := t.currentApp != nil && (t.currentApp.AppName != windowInfo.ProcessName ||
		t.currentApp.WindowTitle != windowInfo.WindowTitle ||
		urlChanged)

	// Check if we need to end the previous activity
	if endingDueToWindowChange {
		previousActivityEndTime = currentTime
		logrus.Infof("WINDOW CHANGE: ending %s at %s, starting %s",
			t.currentApp.AppName, currentTime.Format("15:04:05"), windowInfo.ProcessName)
		t.endCurrentActivity(currentTime, true)
	}

	// Start new activity if different from current
	if t.currentApp == nil || t.currentApp.AppName != windowInfo.ProcessName ||
		t.currentApp.WindowTitle != windowInfo.WindowTitle ||
		urlChanged {
		// Determine start time for new activity:
		// - If previous activity just ended (window change while working): use previous EndTime (no gap)
		// - If starting fresh after idle period: use current time (gap shows actual idle time)
		startTimeForNewActivity := currentTime
		if !previousActivityEndTime.IsZero() {
			// Seamless transition - new activity starts exactly when previous ended
			startTimeForNewActivity = previousActivityEndTime
			logrus.Infof("NEW ACTIVITY (seamless): %s starting at %s", windowInfo.ProcessName, startTimeForNewActivity.Format("15:04:05"))
		} else {
			logrus.Infof("NEW ACTIVITY (fresh): %s starting at %s (currentApp was nil)", windowInfo.ProcessName, startTimeForNewActivity.Format("15:04:05"))
		}
		t.startNewActivity(windowInfo, browserURL, browserTitle, startTimeForNewActivity, isUserActive, isMeeting)
	}

	// Update current activity end time (only if user is active)
	if t.currentApp != nil && isUserActive {
		t.currentApp.EndTime = currentTime
		t.currentApp.DurationSeconds = int(currentTime.Sub(t.currentApp.StartTime).Seconds())
		t.currentApp.IsActive = true

		// Keep IsMeeting flag up to date (user may join a call mid-activity)
		if isMeeting {
			t.currentApp.IsMeeting = true
		}

		// Flush activity every 1 minute to prevent data loss.
		// Sends the current chunk and starts a new one for the same app.
		if t.currentApp.DurationSeconds >= 60 {
			logrus.Infof("1-MIN FLUSH: app=%s, duration=%ds, isMeeting=%v",
				t.currentApp.AppName, t.currentApp.DurationSeconds, t.currentApp.IsMeeting)
			t.endCurrentActivity(currentTime, true)
			t.startNewActivity(windowInfo, browserURL, browserTitle, currentTime, isUserActive, isMeeting)
		}
	}
}

// startNewActivity starts tracking a new activity
func (t *Tracker) startNewActivity(windowInfo *models.WindowInfo, browserURL, browserTitle string,
	startTime time.Time, isActive bool, isMeeting bool) {

	t.currentApp = &models.Activity{
		UserID:          1, // Default user ID
		AppName:         windowInfo.ProcessName,
		WindowTitle:     windowInfo.WindowTitle,
		StartTime:       startTime,
		EndTime:         startTime,
		DurationSeconds: 0,
		IsActive:        isActive,
		IsMeeting:       isMeeting,
		AppIcon:         windowInfo.AppIcon, // Copy app icon from window info
	}

	if browserURL != "" {
		t.currentApp.BrowserURL = &browserURL
	}
	if browserTitle != "" {
		t.currentApp.BrowserTitle = &browserTitle
	}

	// Update stats
	t.stats.mu.Lock()
	t.stats.TotalSessions++
	t.stats.mu.Unlock()

	logrus.Infof("Started tracking: %s - %s", windowInfo.ProcessName, windowInfo.WindowTitle)
}

// endCurrentActivity ends the current activity and saves it to database
func (t *Tracker) endCurrentActivity(endTime time.Time, wasActive bool) {
	if t.currentApp == nil {
		return
	}

	t.currentApp.EndTime = endTime
	t.currentApp.DurationSeconds = int(endTime.Sub(t.currentApp.StartTime).Seconds())

	// Get mouse and keyboard stats from hooks
	if t.mouseHook != nil {
		mouseStats := t.mouseHook.GetStats()
		if sessionClicks, ok := mouseStats["session_clicks"].(int); ok {
			t.currentApp.MouseClicks = sessionClicks
		}
		if sessionDistance, ok := mouseStats["session_distance"].(int); ok {
			t.currentApp.MouseDistance = sessionDistance
		}
	}

	if t.keyboardHook != nil {
		keyboardStats := t.keyboardHook.GetStats()
		if sessionKeystrokes, ok := keyboardStats["session_keystrokes"].(int); ok {
			t.currentApp.Keystrokes = sessionKeystrokes
		}
	}

	// Only save activities that lasted more than 2 seconds
	if t.currentApp.DurationSeconds >= 2 {
		if err := t.db.InsertActivity(t.currentApp); err != nil {
			logrus.Errorf("Failed to insert activity: %v", err)
		} else {
			logrus.Infof("Saved activity: %s for %d seconds (clicks: %d, keys: %d, distance: %d)",
				t.currentApp.AppName, t.currentApp.DurationSeconds,
				t.currentApp.MouseClicks, t.currentApp.Keystrokes, t.currentApp.MouseDistance)

			// Update stats
			t.stats.mu.Lock()
			t.stats.TotalDuration += time.Duration(t.currentApp.DurationSeconds) * time.Second
			t.stats.mu.Unlock()

			// Reset session stats in hooks after saving
			if t.mouseHook != nil {
				t.mouseHook.ResetSessionStats()
			}
			if t.keyboardHook != nil {
				t.keyboardHook.ResetSessionStats()
			}
		}
	}

	t.currentApp = nil
}

// finalizePendingActivity finalizes any pending activity before shutdown
func (t *Tracker) finalizePendingActivity() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.currentApp != nil {
		t.endCurrentActivity(time.Now(), true)
	}
}

// finalizeActivityAtTime finalizes activity at a specific time (used when user goes idle)
func (t *Tracker) finalizeActivityAtTime(endTime time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.currentApp != nil {
		t.endCurrentActivity(endTime, true)
		logrus.Infof("Activity ended at last input time: %s", endTime.Format("15:04:05"))
	}
}

// GetStats returns tracker statistics
func (t *Tracker) GetStats() map[string]interface{} {
	t.stats.mu.RLock()
	defer t.stats.mu.RUnlock()

	uptime := time.Since(t.stats.StartTime)
	
	return map[string]interface{}{
		"start_time":      t.stats.StartTime,
		"uptime_seconds":  int(uptime.Seconds()),
		"total_sessions":  t.stats.TotalSessions,
		"total_duration":  int(t.stats.TotalDuration.Seconds()),
		"last_activity":   t.lastActivity,
	}
}

// IsUserActive returns whether the user is currently active (not idle)
func (t *Tracker) IsUserActive() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.isActive
}

// GetLastActivity returns the time of last user activity
func (t *Tracker) GetLastActivity() time.Time {
	return t.lastActivity
}

// GetCurrentActivity returns the current activity being tracked
func (t *Tracker) GetCurrentActivity() *models.Activity {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.currentApp == nil {
		return nil
	}

	// Return a copy to avoid race conditions
	activity := *t.currentApp
	return &activity
}

// SetIdleTimeout updates the idle timeout duration
func (t *Tracker) SetIdleTimeout(seconds int) {
	if seconds > 0 {
		t.idleDetector.SetIdleTimeout(time.Duration(seconds) * time.Second)
		logrus.Infof("Idle timeout updated to %d seconds", seconds)
	}
}