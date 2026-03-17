package service

import (
	"context"
	"sync"

	"ktracker/internal/activity"
	"ktracker/internal/buffer"
	"ktracker/internal/capture"
	"ktracker/internal/config"
	"ktracker/internal/hooks"
	"ktracker/internal/ipc"
	"ktracker/internal/messaging"
	"ktracker/internal/protocol"
	"ktracker/internal/storage"
	"ktracker/internal/systray"

	"github.com/ncruces/zenity"
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
	pipeServer      *ipc.PipeServer
	syncWorker      *buffer.SyncWorker
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
		// Set callback to get current activity info for screenshots
		screenshotMgr.SetGetCurrentActivityCallback(func() *capture.CurrentActivityInfo {
			currentActivity := actTracker.GetCurrentActivity()
			if currentActivity == nil {
				return nil
			}
			info := &capture.CurrentActivityInfo{
				AppName: currentActivity.AppName,
				AppIcon: currentActivity.AppIcon,
			}
			if currentActivity.BrowserURL != nil {
				info.BrowserURL = *currentActivity.BrowserURL
			}
			return info
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

	// Initialize named pipe server for protocol-based login
	pipeServer := ipc.NewPipeServer()
	pipeServer.SetLoginCallback(func(data protocol.LoginData) {
		tracker.handlePipeLogin(data)
	})
	tracker.pipeServer = pipeServer

	// Initialize sync worker for buffered activities
	if actBuffer := db.GetActivityBuffer(); actBuffer != nil {
		syncWorker := buffer.NewSyncWorker(actBuffer, cfg.API.BaseURL)
		tracker.syncWorker = syncWorker
		logrus.Info("Buffer sync worker initialized")
	}

	// Set logout callback to handle API failures
	db.SetLogoutCallback(func() {
		tracker.handleAPILogout()
	})

	// Set not logged in callback to notify user when data is not being tracked
	db.SetNotLoggedInCallback(func() {
		tracker.showRedNotification("❌ Please Login", "Your time is not being tracked. Click the tray icon to login.")
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

	// Start named pipe server for protocol-based login
	if t.pipeServer != nil {
		if err := t.pipeServer.Start(); err != nil {
			logrus.Errorf("Failed to start pipe server: %v", err)
		}
	}

	// Start buffer sync worker
	if t.syncWorker != nil {
		t.wg.Add(1)
		go func() {
			defer t.wg.Done()
			if err := t.syncWorker.Start(t.ctx); err != nil {
				logrus.Errorf("Sync worker error: %v", err)
			}
		}()
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
	if t.pipeServer != nil {
		t.pipeServer.Stop()
	}
	if t.syncWorker != nil {
		t.syncWorker.Stop()
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
			if msg.Email != "" {
				t.cfg.User.Email = msg.Email
			}
			t.cfg.User.Screenshots = msg.Screenshots == 1
			t.cfg.User.ScreenshotTime = msg.ScreenshotTime
			t.cfg.User.Logout = msg.Logout == 1
			t.cfg.User.ExitApp = msg.ExitApp == 1

			// Save user data
			if err := t.cfg.SaveUserData(); err != nil {
				logrus.Warnf("Failed to save user data after messaging login: %v", err)
			}

			// Update database with app_code and email
			t.db.SetAppCode(msg.AppCode)
			t.db.SetEmail(msg.Email)

			// Update system tray
			if t.systemTray != nil {
				t.systemTray.UpdateUserStatus(msg.Username, msg.AppCode, true)
				t.systemTray.UpdateMenuVisibility()
			}

			logrus.Infof("User logged in successfully: %s", msg.Username)

			// Show success notification
			t.showGreenNotification("✅ Welcome, "+msg.Username+"!", "You're all set — your time is now being tracked.")
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

		// Re-register protocol handler so the browser login flow works
		if err := protocol.RegisterProtocol(); err != nil {
			logrus.Warnf("Failed to re-register protocol after logout: %v", err)
		}

		logrus.Info("User logged out")

		// Show logout notification
		t.showRedNotification("👋 Logged Out", "You have been logged out successfully.")

	default:
		logrus.Warnf("Unknown login action: %s", msg.Action)
	}
}

// handlePipeLogin handles login data received via named pipe from a 2nd instance
func (t *Tracker) handlePipeLogin(data protocol.LoginData) {
	logrus.Infof("Processing pipe login: email=%s, app_code=%s", data.Email, data.AppCode)

	t.cfg.User.Email = data.Email
	t.cfg.User.Username = data.Email
	t.cfg.User.AppCode = data.AppCode
	t.cfg.User.IsLoggedIn = true
	t.cfg.User.Screenshots = data.Screenshots == 1
	t.cfg.User.ScreenshotTime = data.ScreenshotTime
	t.cfg.User.Logout = data.Logout == 1
	t.cfg.User.ExitApp = data.ExitApp == 1

	// Save user data
	if err := t.cfg.SaveUserData(); err != nil {
		logrus.Warnf("Failed to save user data after pipe login: %v", err)
	}

	// Update database
	t.db.SetAppCode(data.AppCode)
	t.db.SetEmail(data.Email)

	// Update system tray
	if t.systemTray != nil {
		t.systemTray.UpdateUserStatus(data.Email, data.AppCode, true)
		t.systemTray.UpdateMenuVisibility()
	}

	t.showGreenNotification("✅ Welcome back!", "You're all set — your time is now being tracked.")
}

// ApplyProtocolLogin applies login data from a protocol URL (cold start scenario)
func (t *Tracker) ApplyProtocolLogin(data *protocol.LoginData) {
	t.handlePipeLogin(*data)
}

// showGreenNotification shows a success notification with the green icon
func (t *Tracker) showGreenNotification(title, message string) {
	if t.systemTray != nil {
		t.systemTray.ShowGreenNotification(title, message)
	} else {
		zenity.Notify(message, zenity.Title(title), zenity.InfoIcon)
	}
}

// showRedNotification shows a warning notification with the red icon
func (t *Tracker) showRedNotification(title, message string) {
	if t.systemTray != nil {
		t.systemTray.ShowRedNotification(title, message)
	} else {
		zenity.Notify(message, zenity.Title(title), zenity.WarningIcon)
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

	// Re-register protocol handler so the browser login flow works
	if err := protocol.RegisterProtocol(); err != nil {
		logrus.Warnf("Failed to re-register protocol after API logout: %v", err)
	}

	logrus.Info("User logged out due to API failure - icon set to red")
}

// SetIdleTimeout updates the idle timeout for activity tracking
func (t *Tracker) SetIdleTimeout(seconds int) {
	if t.activityTracker != nil {
		t.activityTracker.SetIdleTimeout(seconds)
	}
}

// SetVersion sets the app version for display in the system tray
func (t *Tracker) SetVersion(version string) {
	if t.systemTray != nil {
		t.systemTray.SetVersion(version)
	}
}

// SetRestartCallback sets the callback for when the user clicks Restart in the tray menu
func (t *Tracker) SetRestartCallback(callback func()) {
	if t.systemTray != nil {
		t.systemTray.SetRestartCallback(callback)
	}
}

