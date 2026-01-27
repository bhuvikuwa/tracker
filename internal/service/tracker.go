package service

import (
	"context"
	"sync"

	"desktime-tracker/internal/activity"
	"desktime-tracker/internal/capture"
	"desktime-tracker/internal/config"
	"desktime-tracker/internal/hooks"
	"desktime-tracker/internal/messaging"
	"desktime-tracker/internal/storage"
	"desktime-tracker/internal/systray"

	toast "git.sr.ht/~jackmordaunt/go-toast"
	"github.com/sirupsen/logrus"
)

// Tracker is the main service tracker
type Tracker struct {
	ctx             context.Context
	cancel          context.CancelFunc
	cfg             *config.Config
	db              *storage.Database
	activityTracker *activity.Tracker
	mouseHook       *hooks.MouseHook
	keyboardHook    *hooks.KeyboardHook
	screenshotMgr   *capture.ScreenshotManager
	systemTray      *systray.SystemTray
	messagingServer *messaging.MessagingServer
	wg              sync.WaitGroup
	running         bool
	mu              sync.RWMutex
}

// NewTracker creates a new tracker instance
func NewTracker(ctx context.Context, cfg *config.Config, db *storage.Database) (*Tracker, error) {
	ctx, cancel := context.WithCancel(ctx)
	
	tracker := &Tracker{
		ctx:    ctx,
		cancel: cancel,
		cfg:    cfg,
		db:     db,
	}

	// Initialize activity tracker
	actTracker, err := activity.NewTracker(cfg, db)
	if err != nil {
		return nil, err
	}
	tracker.activityTracker = actTracker

	// Set up activity callback for system tray updates
	actTracker.SetActivityCallback(func(isActive bool) {
		tracker.UpdateTrayIcon(isActive)
	})

	// Initialize activity hooks if enabled
	if cfg.Tracking.ActivityInterval > 0 {
		// Create a single shared activity detector for both mouse and keyboard
		activityCallback := func() {
			actTracker.OnUserActivity()
			logrus.Debugf("User activity detected")
		}

		// Initialize mouse hook
		mouseHook, err := hooks.NewMouseHook()
		if err != nil {
			logrus.Warnf("Failed to initialize mouse hook: %v", err)
		} else {
			mouseHook.SetActivityCallback(activityCallback)
			tracker.mouseHook = mouseHook
		}

		// Initialize keyboard hook
		keyboardHook, err := hooks.NewKeyboardHook()
		if err != nil {
			logrus.Warnf("Failed to initialize keyboard hook: %v", err)
		} else {
			keyboardHook.SetActivityCallback(activityCallback)
			tracker.keyboardHook = keyboardHook
		}

		// Pass hooks to activity tracker so it can collect stats
		actTracker.SetInputHooks(tracker.mouseHook, tracker.keyboardHook)
	}

	// Initialize screenshot manager
	screenshotMgr, err := capture.NewScreenshotManager(cfg, db)
	if err != nil {
		logrus.Warnf("Failed to initialize screenshot manager: %v", err)
	} else {
		// Set callback to check if user is active before taking screenshots
		screenshotMgr.SetIsUserActiveCallback(func() bool {
			return actTracker.IsUserActive()
		})
		tracker.screenshotMgr = screenshotMgr
	}

	// Initialize system tray if not running as service
	if !IsWindowsService() {
		systemTray, err := systray.NewSystemTray(cfg)
		if err != nil {
			logrus.Warnf("Failed to initialize system tray: %v", err)
		} else {
			tracker.systemTray = systemTray
			// Set callback to update database app_code and email when user logs in via tray
			systemTray.SetAppCodeUpdateCallback(func(appCode, email string) {
				db.SetAppCode(appCode)
				db.SetEmail(email)
				logrus.Infof("App code and email updated in database after tray login: %s, %s", appCode, email)
			})
		}
	}

	// Initialize messaging server
	messagingServer := messaging.NewMessagingServer(cfg)
	messagingServer.SetCallback(func(msg messaging.LoginMessage) {
		tracker.handleLoginMessage(msg)
	})
	tracker.messagingServer = messagingServer


	// Set logout callback to handle API failures
	db.SetLogoutCallback(func() {
		tracker.handleAPILogout()
	})

	// Set not logged in callback to notify user when data is not being tracked
	db.SetNotLoggedInCallback(func() {
		tracker.showNotification("Please Login", "Your time is not being tracked")
	})

	// Set initial app_code and email if user is already logged in
	if cfg.User.IsLoggedIn && cfg.User.AppCode != "" {
		db.SetAppCode(cfg.User.AppCode)
		db.SetEmail(cfg.User.Email)
		logrus.Infof("Initialized with existing login: %s (email: %s)", cfg.User.Username, cfg.User.Email)
	}

	return tracker, nil
}

// Start starts the tracker
func (t *Tracker) Start() error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = true
	t.mu.Unlock()

	logrus.Info("Starting KTracker")

	// Start mouse hook
	if t.mouseHook != nil {
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			if err := t.mouseHook.Start(); err != nil {
				logrus.Errorf("Mouse hook error: %v", err)
			}
		}()
	}

	// Start keyboard hook
	if t.keyboardHook != nil {
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			if err := t.keyboardHook.Start(); err != nil {
				logrus.Errorf("Keyboard hook error: %v", err)
			}
		}()
	}

	// Start activity tracker
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		if err := t.activityTracker.Start(t.ctx); err != nil {
			logrus.Errorf("Activity tracker error: %v", err)
		}
	}()

	// Start screenshot manager
	if t.screenshotMgr != nil {
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			if err := t.screenshotMgr.Start(t.ctx); err != nil {
				logrus.Errorf("Screenshot manager error: %v", err)
			}
		}()
	}

	// Start system tray
	if t.systemTray != nil {
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			if err := t.systemTray.Start(t.ctx); err != nil {
				logrus.Errorf("System tray error: %v", err)
			}
		}()
	}

	// Start messaging server
	if t.messagingServer != nil {
		if err := t.messagingServer.Start(); err != nil {
			logrus.Errorf("Failed to start messaging server: %v", err)
		}
	}

	// Note: Periodic cleanup and data aggregation should be handled by the PHP backend

	logrus.Info("All KTracker components started successfully")
	return nil
}

// Stop stops the tracker
func (t *Tracker) Stop() {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return
	}
	t.running = false
	t.mu.Unlock()

	logrus.Info("Stopping KTracker")

	// Cancel context to signal all goroutines to stop
	t.cancel()

	// Stop individual components
	if t.mouseHook != nil {
		t.mouseHook.Stop()
	}
	if t.keyboardHook != nil {
		t.keyboardHook.Stop()
	}
	if t.systemTray != nil {
		t.systemTray.Stop()
	}
	if t.messagingServer != nil {
		t.messagingServer.Stop()
	}

	// Wait for all goroutines to finish
	t.wg.Wait()

	logrus.Info("KTracker stopped successfully")
}

// IsRunning returns whether the tracker is running
func (t *Tracker) IsRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}


// GetStats returns current tracker statistics
func (t *Tracker) GetStats() map[string]interface{} {
	stats := make(map[string]interface{})
	
	t.mu.RLock()
	stats["running"] = t.running
	t.mu.RUnlock()
	
	if t.activityTracker != nil {
		stats["activity_tracker"] = t.activityTracker.GetStats()
	}
	
	if t.mouseHook != nil {
		stats["mouse_stats"] = t.mouseHook.GetStats()
	}
	
	if t.keyboardHook != nil {
		stats["keyboard_stats"] = t.keyboardHook.GetStats()
	}
	
	return stats
}

// UpdateTrayIcon updates the system tray icon based on activity status
func (t *Tracker) UpdateTrayIcon(isActive bool) {
	if t.systemTray != nil {
		t.systemTray.SetActive(isActive)
	}
}

// handleLoginMessage handles login/logout messages from the web application
func (t *Tracker) handleLoginMessage(msg messaging.LoginMessage) {
	logrus.Infof("Received login message: %+v", msg)

	switch msg.Action {
	case "login":
		if msg.Username != "" && msg.AppCode != "" {
			t.cfg.User.Username = msg.Username
			t.cfg.User.AppCode = msg.AppCode
			t.cfg.User.IsLoggedIn = true

			// Update database with app_code
			t.db.SetAppCode(msg.AppCode)

			// Update system tray
			if t.systemTray != nil {
				t.systemTray.UpdateUserStatus(msg.Username, msg.AppCode, true)
			}

			logrus.Infof("User logged in successfully: %s", msg.Username)

			// Show success notification
			t.showNotification("Login Successful", "Logged in as "+msg.Username+". Tracking started.")
		} else {
			logrus.Warn("Invalid login message - missing username or app code")
		}

	case "logout":
		t.cfg.User.Username = ""
		t.cfg.User.AppCode = ""
		t.cfg.User.IsLoggedIn = false

		// Clear app_code from database
		t.db.SetAppCode("")

		// Update system tray
		if t.systemTray != nil {
			t.systemTray.UpdateUserStatus("", "", false)
		}

		logrus.Info("User logged out")

		// Show logout notification
		t.showNotification("Logged Out", "You have been logged out. Tracking paused.")

	default:
		logrus.Warnf("Unknown login action: %s", msg.Action)
	}
}

// showNotification shows a system notification using go-toast (Windows)
func (t *Tracker) showNotification(title, message string) {
	n := toast.Notification{
		AppID: "KTracker",
		Title: title,
		Body:  message,
	}
	if err := n.Push(); err != nil {
		logrus.Debugf("Failed to show notification: %v", err)
	}
}

// handleAPILogout handles logout triggered by API failure (e.g., invalid app_code)
func (t *Tracker) handleAPILogout() {
	logrus.Warn("API returned failure - logging out user")

	// Clear user data in config
	t.cfg.User.Username = ""
	t.cfg.User.Email = ""
	t.cfg.User.AppCode = ""
	t.cfg.User.IsLoggedIn = false

	// Save config to file
	if err := t.cfg.Save(""); err != nil {
		logrus.Errorf("Failed to save config after API logout: %v", err)
	}

	// Clear app_code in database
	t.db.SetAppCode("")

	// Update system tray icon to red
	if t.systemTray != nil {
		t.systemTray.UpdateUserStatus("", "", false)
	}

	logrus.Info("User logged out due to API failure - icon set to red")
}

