package systray

import (
	"context"
	"embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"ktracker/internal/config"
	"ktracker/internal/protocol"

	"fyne.io/systray"
	"github.com/ncruces/zenity"
	"github.com/sirupsen/logrus"
)

//go:embed icons/ktracker_green.ico icons/ktracker_red.ico
var embeddedIcons embed.FS

// SystemTray manages the system tray interface
type SystemTray struct {
	cfg             *config.Config
	running         bool
	isActive        bool
	greenIcon       []byte
	redIcon         []byte
	activityStatus   string
	greenIconPath    string // Temp file path for green icon (notifications)
	redIconPath      string // Temp file path for red icon (notifications)
	onAppCodeUpdate  func(appCode, email string) // Callback to update app_code and email in database

	// Double-click detection
	lastClickTime time.Time
	clickMu       sync.Mutex

	// Mutex for config and menu updates
	cfgMu sync.Mutex

	// Stop channel for background goroutines
	stopCh chan struct{}

	// Restart callback
	onRestart func()

	// App version for display
	version string

	// Menu items - stored for dynamic show/hide
	mUser         *systray.MenuItem
	mVersion      *systray.MenuItem
	mShowKTracker *systray.MenuItem
	mRestart      *systray.MenuItem
	mLogout       *systray.MenuItem
	mLogin        *systray.MenuItem
	mExit         *systray.MenuItem
}

// NewSystemTray creates a new system tray manager
func NewSystemTray(cfg *config.Config) (*SystemTray, error) {
	st := &SystemTray{
		cfg:    cfg,
		stopCh: make(chan struct{}),
	}

	// Load icons
	if err := st.loadIcons(); err != nil {
		logrus.Warnf("Failed to load tray icons: %v", err)
	}

	// Write icons to temp files for use in notifications
	st.writeNotificationIcons()

	return st, nil
}

// writeNotificationIcons writes embedded icons to temp files for zenity notifications
func (st *SystemTray) writeNotificationIcons() {
	tmpDir := os.TempDir()
	if len(st.greenIcon) > 0 {
		greenPath := filepath.Join(tmpDir, "ktracker_green.ico")
		if err := os.WriteFile(greenPath, st.greenIcon, 0644); err == nil {
			st.greenIconPath = greenPath
		}
	}
	if len(st.redIcon) > 0 {
		redPath := filepath.Join(tmpDir, "ktracker_red.ico")
		if err := os.WriteFile(redPath, st.redIcon, 0644); err == nil {
			st.redIconPath = redPath
		}
	}
}

// SetAppCodeUpdateCallback sets the callback for when app_code is updated after login
func (st *SystemTray) SetAppCodeUpdateCallback(callback func(appCode, email string)) {
	st.onAppCodeUpdate = callback
}

// SetVersion sets the app version for display in the tray menu
func (st *SystemTray) SetVersion(version string) {
	st.version = version
}

// SetRestartCallback sets the callback for when the user clicks Restart
func (st *SystemTray) SetRestartCallback(callback func()) {
	st.onRestart = callback
}


// loadIcons loads the green and red icons
func (st *SystemTray) loadIcons() error {
	var greenLoaded, redLoaded bool

	// Try embedded icons first (always available in the exe)
	if greenData, err := embeddedIcons.ReadFile("icons/ktracker_green.ico"); err == nil {
		st.greenIcon = greenData
		greenLoaded = true
		logrus.Infof("Loaded embedded green icon (size: %d bytes)", len(greenData))
	}

	if redData, err := embeddedIcons.ReadFile("icons/ktracker_red.ico"); err == nil {
		st.redIcon = redData
		redLoaded = true
		logrus.Infof("Loaded embedded red icon (size: %d bytes)", len(redData))
	}

	// If embedded icons loaded, we're done
	if greenLoaded && redLoaded {
		return nil
	}

	// Fallback to file system icons
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	exeDir := filepath.Dir(exePath)

	iconPaths := []string{
		filepath.Join(exeDir, "icons"),
		filepath.Join(".", "icons"),
		"icons",
	}

	for _, basePath := range iconPaths {
		if !greenLoaded {
			greenPath := filepath.Join(basePath, "ktracker_green.ico")
			if greenData, err := os.ReadFile(greenPath); err == nil {
				st.greenIcon = greenData
				greenLoaded = true
				logrus.Infof("Loaded green icon from: %s (size: %d bytes)", greenPath, len(greenData))
			}
		}

		if !redLoaded {
			redPath := filepath.Join(basePath, "ktracker_red.ico")
			if redData, err := os.ReadFile(redPath); err == nil {
				st.redIcon = redData
				redLoaded = true
				logrus.Infof("Loaded red icon from: %s (size: %d bytes)", redPath, len(redData))
			}
		}

		if greenLoaded && redLoaded {
			break
		}
	}

	if !greenLoaded || !redLoaded {
		return fmt.Errorf("failed to load icons")
	}

	return nil
}

// Start starts the system tray
func (st *SystemTray) Start(ctx context.Context) error {
	logrus.Info("Starting system tray")
	st.running = true

	// Run systray in a blocking call
	systray.Run(st.onReady, st.onExit)
	
	return nil
}

// Stop stops the system tray
func (st *SystemTray) Stop() {
	if st.running {
		logrus.Info("Stopping system tray")
		close(st.stopCh) // Signal background goroutines to stop
		systray.Quit()
		st.running = false
	}
}

// onReady is called when the system tray is ready
func (st *SystemTray) onReady() {
	// Set initial icon based on login status
	st.updateIcon()

	systray.SetTitle("KTracker")
	systray.SetTooltip("KTracker Activity Tracker")

	// Set up double-click detection using SetOnTapped
	systray.SetOnTapped(st.handleTrayClick)

	logrus.Infof("Creating system tray menu - IsLoggedIn: %v, Username: %s", st.cfg.User.IsLoggedIn, st.cfg.User.Username)

	// Create menu items based on initial login status
	// We create all items but only show the appropriate ones
	if st.cfg.User.IsLoggedIn {
		// Logged in - create logged-in items first (they appear at top)
		st.mUser = systray.AddMenuItem(fmt.Sprintf("User: %s", st.cfg.User.Username), "Current user")
		st.mUser.Disable()
		st.mVersion = systray.AddMenuItem(fmt.Sprintf("v%s", st.version), "App version")
		st.mVersion.Disable()
		st.mShowKTracker = systray.AddMenuItem("Show Tracked Time", "Open  dashboard")
		st.mRestart = systray.AddMenuItem("Restart", "Restart the application")
		st.mLogout = systray.AddMenuItem("Logout", "Logout from KTracker")
		// Hide logout if setting is disabled
		if !st.cfg.User.Logout {
			st.mLogout.Hide()
		}
		// Create login item but hide it
		st.mLogin = systray.AddMenuItem("Login", "Login to KTracker")
		st.mLogin.Hide()
	} else {
		// Not logged in - create login item first (appears at top)
		st.mLogin = systray.AddMenuItem("Login", "Login to KTracker")
		// Create logged-in items but hide them
		st.mUser = systray.AddMenuItem(fmt.Sprintf("User: %s", st.cfg.User.Username), "Current user")
		st.mUser.Disable()
		st.mUser.Hide()
		st.mVersion = systray.AddMenuItem(fmt.Sprintf("v%s", st.version), "App version")
		st.mVersion.Disable()
		st.mVersion.Hide()
		st.mShowKTracker = systray.AddMenuItem("Show KTracker", "Open KTracker dashboard")
		st.mShowKTracker.Hide()
		st.mRestart = systray.AddMenuItem("Restart", "Restart the application")
		st.mRestart.Hide()
		st.mLogout = systray.AddMenuItem("Logout", "Logout from KTracker")
		st.mLogout.Hide()
	}

	// Add separator and Exit button
	systray.AddSeparator()
	st.mExit = systray.AddMenuItem("Quit KTracker", "Quit KTracker")
	// Hide Exit by default when not logged in, or when exit_app setting is disabled
	if !st.cfg.User.IsLoggedIn || !st.cfg.User.ExitApp {
		st.mExit.Hide()
	}

	// Handle menu clicks - all handlers run in goroutines with logging
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("PANIC in Login handler: %v", r)
			}
		}()
		for {
			logrus.Debug("Login handler: waiting for click...")
			<-st.mLogin.ClickedCh
			logrus.Info("Login menu clicked - handler starting")
			st.handleLogin()
			logrus.Info("Login handler finished")
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("PANIC in Show KTracker handler: %v", r)
			}
		}()
		for {
			logrus.Debug("ShowKTracker handler: waiting for click...")
			<-st.mShowKTracker.ClickedCh
			logrus.Info("ShowKTracker menu clicked - handler starting")
			st.handleShowKtracker()
			logrus.Info("ShowKTracker handler finished")
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("PANIC in Restart handler: %v", r)
			}
		}()
		for {
			logrus.Debug("Restart handler: waiting for click...")
			<-st.mRestart.ClickedCh
			logrus.Info("Restart menu clicked - handler starting")
			if st.onRestart != nil {
				st.onRestart()
			}
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("PANIC in Logout handler: %v", r)
			}
		}()
		for {
			logrus.Debug("Logout handler: waiting for click...")
			<-st.mLogout.ClickedCh
			logrus.Info("Logout menu clicked - handler starting")
			st.handleLogout()
			logrus.Info("Logout handler finished")
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("PANIC in Exit handler: %v", r)
			}
		}()
		for {
			logrus.Debug("Exit handler: waiting for click...")
			<-st.mExit.ClickedCh
			logrus.Info("Exit menu clicked - handler starting")
			st.handleExit()
		}
	}()
}

// onExit is called when the system tray is exiting
func (st *SystemTray) onExit() {
	logrus.Info("System tray exiting")
}

// UpdateMenuVisibility shows/hides menu items based on login status and settings
func (st *SystemTray) UpdateMenuVisibility() {
	if st.mLogin == nil || st.mUser == nil {
		logrus.Warn("Menu items not initialized yet")
		return
	}

	// Read config values with lock
	st.cfgMu.Lock()
	isLoggedIn := st.cfg.User.IsLoggedIn
	username := st.cfg.User.Username
	showLogout := st.cfg.User.Logout
	showExitApp := st.cfg.User.ExitApp
	st.cfgMu.Unlock()

	if isLoggedIn {
		// User is logged in - show logged-in items, hide login
		logrus.Infof("Updating menu: showing logged-in items (logout=%v, exit_app=%v)",
			showLogout, showExitApp)
		st.mLogin.Hide()
		st.mUser.SetTitle(fmt.Sprintf("User: %s", username))
		st.mUser.Show()
		if st.mVersion != nil {
			st.mVersion.Show()
		}
		st.mShowKTracker.Show()
		st.mRestart.Show()
		// Show/hide logout based on setting
		if showLogout {
			st.mLogout.Show()
		} else {
			st.mLogout.Hide()
		}
		// Show/hide exit based on setting
		if showExitApp {
			st.mExit.Show()
		} else {
			st.mExit.Hide()
		}
	} else {
		// User is not logged in - hide logged-in items, show login, hide exit
		logrus.Info("Updating menu: hiding logged-in items, showing login")
		st.mUser.Hide()
		if st.mVersion != nil {
			st.mVersion.Hide()
		}
		st.mShowKTracker.Hide()
		st.mRestart.Hide()
		st.mLogout.Hide()
		st.mExit.Hide() // Hide exit when not logged in
		// Force re-add by setting title first, then showing
		st.mLogin.SetTitle("Login")
		st.mLogin.Show()
		logrus.Info("mLogin.Show() called")
	}
}


// handleTrayClick detects clicks on the tray icon
func (st *SystemTray) handleTrayClick() {
	logrus.Debug("Tray icon clicked")
	st.clickMu.Lock()
	defer st.clickMu.Unlock()

	// If user is not logged in, open login dialog on any click
	if !st.cfg.User.IsLoggedIn {
		logrus.Info("User not logged in - opening login dialog")
		go st.handleLogin()
		return
	}

	// User is logged in - detect double-click to open KTracker
	now := time.Now()
	timeSinceLastClick := now.Sub(st.lastClickTime)
	// Windows default double-click time is typically 500ms
	if timeSinceLastClick < 500*time.Millisecond {
		// Double-click detected - open KTracker
		logrus.Info("Double-click detected on tray icon - opening KTracker")
		st.lastClickTime = time.Time{} // Reset to prevent triple-click
		go st.handleShowKtracker()
	} else {
		logrus.Debugf("Single click - waiting for double-click (time since last: %v)", timeSinceLastClick)
		st.lastClickTime = now
	}
}

// handleLogin opens the web login page in the default browser
func (st *SystemTray) handleLogin() {
	loginURL := st.cfg.Website.BaseURL + "/?from=ktracker"
	logrus.Infof("Opening web login page: %s", loginURL)
	st.openURL(loginURL)
	st.ShowGreenNotification("🔗 Login", "Please complete login in your browser")
}

// handleShowKTracker opens the KTracker dashboard with app code
func (st *SystemTray) handleShowKtracker() {
	if st.cfg.User.AppCode != "" {
		ktrackerURL := fmt.Sprintf("%s/?code=%s", st.cfg.Website.BaseURL, st.cfg.User.AppCode)
		logrus.Infof("Opening KTracker dashboard: %s", ktrackerURL)
		st.openURL(ktrackerURL)
	} else {
		logrus.Warn("Cannot open KTracker - no app code available")
	}
}

// handleLogout clears user data
func (st *SystemTray) handleLogout() {
	logrus.Info("User requested logout")

	// Clear user data in config (with lock)
	st.cfgMu.Lock()
	st.cfg.User.Username = ""
	st.cfg.User.Email = ""
	st.cfg.User.AppCode = ""
	st.cfg.User.IsLoggedIn = false

	// Save cleared user data to AppData folder
	if err := st.cfg.SaveUserData(); err != nil {
		logrus.Errorf("Failed to save user data after logout: %v", err)
	}
	st.cfgMu.Unlock()

	// Clear app_code and email in database
	if st.onAppCodeUpdate != nil {
		st.onAppCodeUpdate("", "")
	}

	// Update icon to red
	st.updateIcon()

	// Update menu visibility to show login item
	st.UpdateMenuVisibility()

	// Re-register protocol handler so the browser login flow works
	if err := protocol.RegisterProtocol(); err != nil {
		logrus.Warnf("Failed to re-register protocol after logout: %v", err)
	}

	logrus.Info("User logged out successfully")
	st.ShowRedNotification("👋 Logged Out", "You have been logged out successfully.")
}

// handleExit terminates the application
func (st *SystemTray) handleExit() {
	logrus.Info("User requested exit")
	systray.Quit()
	os.Exit(0)
}

// openURL opens a URL in the default browser
func (st *SystemTray) openURL(url string) {
	var cmd *exec.Cmd
	
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	default:
		logrus.Errorf("Unsupported operating system: %s", runtime.GOOS)
		return
	}
	
	if err := cmd.Start(); err != nil {
		logrus.Errorf("Failed to open URL %s: %v", url, err)
	}
}

// ShowNotification shows a system notification using zenity (default info icon)
func (st *SystemTray) ShowNotification(title, message string) {
	if err := zenity.Notify(message, zenity.Title(title), zenity.InfoIcon); err != nil {
		logrus.Debugf("Failed to show notification: %v", err)
	}
}

// ShowGreenNotification shows a success notification with the green KTracker icon
func (st *SystemTray) ShowGreenNotification(title, message string) {
	opts := []zenity.Option{zenity.Title(title)}
	if st.greenIconPath != "" {
		opts = append(opts, zenity.Icon(st.greenIconPath))
	} else {
		opts = append(opts, zenity.InfoIcon)
	}
	if err := zenity.Notify(message, opts...); err != nil {
		logrus.Debugf("Failed to show notification: %v", err)
	}
}

// ShowRedNotification shows a warning/error notification with the red KTracker icon
func (st *SystemTray) ShowRedNotification(title, message string) {
	opts := []zenity.Option{zenity.Title(title)}
	if st.redIconPath != "" {
		opts = append(opts, zenity.Icon(st.redIconPath))
	} else {
		opts = append(opts, zenity.WarningIcon)
	}
	if err := zenity.Notify(message, opts...); err != nil {
		logrus.Debugf("Failed to show notification: %v", err)
	}
}

// UpdateStatus updates the system tray status
func (st *SystemTray) UpdateStatus(status string) {
	if st.running {
		tooltip := fmt.Sprintf("KTracker Activity Tracker - %s", status)
		systray.SetTooltip(tooltip)
	}
}

// SetActive sets the activity status and updates the icon
func (st *SystemTray) SetActive(isActive bool) {
	// Always keep active when logged in - just update icon based on login status
	st.updateIcon()
}

// UpdateUserStatus updates the user login status and refreshes the menu
func (st *SystemTray) UpdateUserStatus(username, appCode string, isLoggedIn bool) {
	st.cfg.User.Username = username
	st.cfg.User.AppCode = appCode
	st.cfg.User.IsLoggedIn = isLoggedIn

	// Update the icon to reflect login status change
	st.updateIcon()

	logrus.Infof("User status updated - Login: %v, User: %s", isLoggedIn, username)
}

// updateIcon updates the system tray icon based on login status only
func (st *SystemTray) updateIcon() {
	if !st.running {
		return
	}

	// Green when logged in, red when not logged in
	if st.cfg.User.IsLoggedIn {
		if len(st.greenIcon) > 0 {
			systray.SetIcon(st.greenIcon)
		}
	} else {
		if len(st.redIcon) > 0 {
			systray.SetIcon(st.redIcon)
		}
	}
}

// IsRunning returns whether the system tray is running
func (st *SystemTray) IsRunning() bool {
	return st.running
}