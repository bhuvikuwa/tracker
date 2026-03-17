package capture

import (
	"bytes"
	"context"
	"encoding/base64"
	"image/png"
	"time"

	"ktracker/internal/config"
	"ktracker/internal/storage"

	"github.com/kbinani/screenshot"
	"github.com/sirupsen/logrus"
)

// CurrentActivityInfo contains the current activity information for screenshots
type CurrentActivityInfo struct {
	AppName    string
	BrowserURL string
	AppIcon    string
}

// ScreenshotManager manages screenshot capture
type ScreenshotManager struct {
	cfg                *config.Config
	db                 *storage.Database
	isUserActive       func() bool                 // Callback to check if user is active
	getCurrentActivity func() *CurrentActivityInfo // Callback to get current activity info
}

// NewScreenshotManager creates a new screenshot manager
func NewScreenshotManager(cfg *config.Config, db *storage.Database) (*ScreenshotManager, error) {
	return &ScreenshotManager{
		cfg: cfg,
		db:  db,
	}, nil
}

// SetIsUserActiveCallback sets the callback to check if user is active
func (sm *ScreenshotManager) SetIsUserActiveCallback(callback func() bool) {
	sm.isUserActive = callback
}

// SetGetCurrentActivityCallback sets the callback to get current activity info
func (sm *ScreenshotManager) SetGetCurrentActivityCallback(callback func() *CurrentActivityInfo) {
	sm.getCurrentActivity = callback
}

// getScreenshotInterval returns the current screenshot interval
// Uses User.ScreenshotTime from server if set, otherwise defaults to 300 seconds
func (sm *ScreenshotManager) getScreenshotInterval() time.Duration {
	// Use server-provided interval if available and valid
	if sm.cfg.User.ScreenshotTime > 0 {
		return time.Duration(sm.cfg.User.ScreenshotTime) * time.Second
	}
	// Default to 300 seconds (5 minutes) if not set by server
	return 300 * time.Second
}

// Start starts the screenshot capture process
func (sm *ScreenshotManager) Start(ctx context.Context) error {
	logrus.Info("Starting screenshot manager")

	// Take initial screenshot
	if err := sm.captureAndSendScreenshot(); err != nil {
		logrus.Errorf("Failed to capture initial screenshot: %v", err)
	}

	for {
		// Get current interval (can change dynamically via login/refresh)
		interval := sm.getScreenshotInterval()
		logrus.Debugf("Next screenshot in %v", interval)

		select {
		case <-time.After(interval):
			if err := sm.captureAndSendScreenshot(); err != nil {
				logrus.Errorf("Failed to capture screenshot: %v", err)
			}
		case <-ctx.Done():
			logrus.Info("Screenshot manager stopped")
			return nil
		}
	}
}

// captureAndSendScreenshot captures screenshot from each display separately and sends to API
func (sm *ScreenshotManager) captureAndSendScreenshot() error {
	// Skip screenshot if user is idle
	if sm.isUserActive != nil && !sm.isUserActive() {
		logrus.Debug("Skipping screenshot - user is idle")
		return nil
	}

	// Get number of active displays
	numDisplays := screenshot.NumActiveDisplays()
	logrus.Debugf("Detected %d active display(s)", numDisplays)

	if numDisplays == 0 {
		logrus.Warn("No active displays detected")
		return nil
	}

	// Capture and send each display separately
	for i := 0; i < numDisplays; i++ {
		bounds := screenshot.GetDisplayBounds(i)

		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			logrus.Warnf("Failed to capture display %d: %v", i, err)
			continue
		}

		// Encode to PNG in memory
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			logrus.Warnf("Failed to encode display %d screenshot: %v", i, err)
			continue
		}

		// Convert to base64
		base64Data := base64.StdEncoding.EncodeToString(buf.Bytes())

		logrus.Infof("Display %d screenshot captured: %dx%d, base64 length: %d",
			i, bounds.Dx(), bounds.Dy(), len(base64Data))

		// Get current activity info for the screenshot
		var appName, browserURL, appIcon string
		if sm.getCurrentActivity != nil {
			if activityInfo := sm.getCurrentActivity(); activityInfo != nil {
				appName = activityInfo.AppName
				browserURL = activityInfo.BrowserURL
				appIcon = activityInfo.AppIcon
			}
		}

		// Send to API with display index and activity info
		result, err := sm.db.InsertScreenshot(base64Data, i, numDisplays, appName, browserURL, appIcon)
		if err != nil {
			logrus.Errorf("Failed to upload display %d screenshot: %v", i, err)
			continue
		}

		if result != nil && result.Success {
			logrus.Infof("Display %d screenshot uploaded successfully: ID=%d", i, result.ScreenshotID)
		}
	}

	return nil
}

