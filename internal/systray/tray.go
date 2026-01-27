package systray

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"desktime-tracker/internal/config"
	"desktime-tracker/internal/dialog"

	"fyne.io/systray"
	toast "git.sr.ht/~jackmordaunt/go-toast"
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
	activityStatus  string
	onAppCodeUpdate func(appCode, email string) // Callback to update app_code and email in database

	// Double-click detection
	lastClickTime time.Time
	clickMu       sync.Mutex

	// Menu items - stored for dynamic show/hide
	mUser         *systray.MenuItem
	mShowDesktime *systray.MenuItem
	mRefresh      *systray.MenuItem
	mLogout       *systray.MenuItem
	mLogin        *systray.MenuItem
	mExit         *systray.MenuItem
}

// NewSystemTray creates a new system tray manager
func NewSystemTray(cfg *config.Config) (*SystemTray, error) {
	st := &SystemTray{
		cfg: cfg,
	}

	// Load icons
	if err := st.loadIcons(); err != nil {
		logrus.Warnf("Failed to load tray icons: %v", err)
	}

	return st, nil
}

// SetAppCodeUpdateCallback sets the callback for when app_code is updated after login
func (st *SystemTray) SetAppCodeUpdateCallback(callback func(appCode, email string)) {
	st.onAppCodeUpdate = callback
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
		st.mShowDesktime = systray.AddMenuItem("Show Desktime", "Open Desktime dashboard")
		st.mRefresh = systray.AddMenuItem("Refresh", "Refresh settings from server")
		st.mLogout = systray.AddMenuItem("Logout", "Logout from Desktime")
		// Hide logout if setting is disabled
		if !st.cfg.User.Logout {
			st.mLogout.Hide()
		}
		// Create login item but hide it
		st.mLogin = systray.AddMenuItem("Login", "Login to Desktime")
		st.mLogin.Hide()
	} else {
		// Not logged in - create login item first (appears at top)
		st.mLogin = systray.AddMenuItem("Login", "Login to Desktime")
		// Create logged-in items but hide them
		st.mUser = systray.AddMenuItem(fmt.Sprintf("User: %s", st.cfg.User.Username), "Current user")
		st.mUser.Disable()
		st.mUser.Hide()
		st.mShowDesktime = systray.AddMenuItem("Show Desktime", "Open Desktime dashboard")
		st.mShowDesktime.Hide()
		st.mRefresh = systray.AddMenuItem("Refresh", "Refresh settings from server")
		st.mRefresh.Hide()
		st.mLogout = systray.AddMenuItem("Logout", "Logout from Desktime")
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
				logrus.Errorf("PANIC in ShowDesktime handler: %v", r)
			}
		}()
		for {
			logrus.Debug("ShowDesktime handler: waiting for click...")
			<-st.mShowDesktime.ClickedCh
			logrus.Info("ShowDesktime menu clicked - handler starting")
			st.handleShowDesktime()
			logrus.Info("ShowDesktime handler finished")
		}
	}()

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.Errorf("PANIC in Refresh handler: %v", r)
			}
		}()
		for {
			logrus.Debug("Refresh handler: waiting for click...")
			<-st.mRefresh.ClickedCh
			logrus.Info("Refresh menu clicked - handler starting")
			st.refreshSettings()
			logrus.Info("Refresh handler finished")
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

	// Heartbeat goroutine to log that systray is still alive
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			<-ticker.C
			logrus.Debug("Systray heartbeat - still running")
		}
	}()

	// Start periodic settings refresh (every 10 minutes)
	st.startSettingsRefresh()
}

// onExit is called when the system tray is exiting
func (st *SystemTray) onExit() {
	logrus.Info("System tray exiting")
}

// updateMenuVisibility shows/hides menu items based on login status and settings
func (st *SystemTray) updateMenuVisibility() {
	if st.mLogin == nil || st.mUser == nil {
		logrus.Warn("Menu items not initialized yet")
		return
	}

	if st.cfg.User.IsLoggedIn {
		// User is logged in - show logged-in items, hide login
		logrus.Infof("Updating menu: showing logged-in items (logout=%v, exit_app=%v)",
			st.cfg.User.Logout, st.cfg.User.ExitApp)
		st.mLogin.Hide()
		st.mUser.SetTitle(fmt.Sprintf("User: %s", st.cfg.User.Username))
		st.mUser.Show()
		st.mShowDesktime.Show()
		st.mRefresh.Show()
		// Show/hide logout based on setting
		if st.cfg.User.Logout {
			st.mLogout.Show()
		} else {
			st.mLogout.Hide()
		}
		// Show/hide exit based on setting
		if st.cfg.User.ExitApp {
			st.mExit.Show()
		} else {
			st.mExit.Hide()
		}
	} else {
		// User is not logged in - hide logged-in items, show login, hide exit
		logrus.Info("Updating menu: hiding logged-in items, showing login")
		st.mUser.Hide()
		st.mShowDesktime.Hide()
		st.mRefresh.Hide()
		st.mLogout.Hide()
		st.mExit.Hide() // Hide exit when not logged in
		// Force re-add by setting title first, then showing
		st.mLogin.SetTitle("Login")
		st.mLogin.Show()
		logrus.Info("mLogin.Show() called")
	}
}

// refreshSettings fetches the latest settings from the server and updates the menu
func (st *SystemTray) refreshSettings() {
	if !st.cfg.User.IsLoggedIn || st.cfg.User.Email == "" {
		return
	}

	logrus.Info("Refreshing settings from server...")

	// Call the API to get updated settings
	loginResp, err := st.sendLoginToAPI(st.cfg.User.Email)
	if err != nil {
		logrus.Warnf("Failed to refresh settings: %v", err)
		return
	}

	// Check if settings changed
	settingsChanged := st.cfg.User.Screenshots != loginResp.Screenshots ||
		st.cfg.User.Logout != loginResp.Logout ||
		st.cfg.User.ExitApp != loginResp.ExitApp

	if settingsChanged {
		logrus.Infof("Settings changed - Screenshots: %v->%v, Logout: %v->%v, ExitApp: %v->%v",
			st.cfg.User.Screenshots, loginResp.Screenshots,
			st.cfg.User.Logout, loginResp.Logout,
			st.cfg.User.ExitApp, loginResp.ExitApp)

		// Update settings
		st.cfg.User.Screenshots = loginResp.Screenshots
		st.cfg.User.Logout = loginResp.Logout
		st.cfg.User.ExitApp = loginResp.ExitApp

		// Save updated settings
		if err := st.cfg.SaveUserData(); err != nil {
			logrus.Warnf("Failed to save updated settings: %v", err)
		}

		// Update menu visibility based on new settings
		st.updateMenuVisibility()
	} else {
		logrus.Debug("Settings unchanged")
	}
}

// startSettingsRefresh starts a goroutine that periodically refreshes settings
func (st *SystemTray) startSettingsRefresh() {
	go func() {
		// Refresh settings every 10 minutes
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()

		for {
			<-ticker.C
			if st.cfg.User.IsLoggedIn {
				st.refreshSettings()
			}
		}
	}()
}

// showStatus shows the tracker status
func (st *SystemTray) showStatus() {
	// In a real implementation, this would show a status dialog
	logrus.Info("Status requested from system tray")

	// For now, just update the menu text
	// You would typically show a dialog or notification here
}

// showStatistics shows activity statistics
func (st *SystemTray) showStatistics() {
	// In a real implementation, this would show a statistics dialog
	logrus.Info("Statistics requested from system tray")
	
	// You would typically show a dialog with current statistics here
}

// handleTrayClick detects double-clicks on the tray icon
func (st *SystemTray) handleTrayClick() {
	logrus.Debug("Tray icon clicked")
	st.clickMu.Lock()
	defer st.clickMu.Unlock()

	now := time.Now()
	timeSinceLastClick := now.Sub(st.lastClickTime)
	// Windows default double-click time is typically 500ms
	if timeSinceLastClick < 500*time.Millisecond {
		// Double-click detected - open Desktime
		logrus.Info("Double-click detected on tray icon - opening Desktime")
		st.lastClickTime = time.Time{} // Reset to prevent triple-click
		go st.handleShowDesktime()
	} else {
		logrus.Debugf("Single click - waiting for double-click (time since last: %v)", timeSinceLastClick)
		st.lastClickTime = now
	}
}

// handleLogin shows the native login dialog
func (st *SystemTray) handleLogin() {
	// Check if dialog is already open to prevent multiple instances
	if dialog.IsDialogOpen() {
		logrus.Debug("Login dialog already open, ignoring click")
		return
	}

	// Loop until successful login or user cancels
	for {
		logrus.Info("Opening login dialog")

		// Show native Windows dialog (blocks until closed)
		email, ok := dialog.ShowEmailInputDialog()
		if !ok || email == "" {
			logrus.Info("Login cancelled or dialog already open")
			return
		}

		logrus.Infof("Email entered: %s, sending to API...", email)

		// Send email to PHP API and get app_code and settings
		loginResp, err := st.sendLoginToAPI(email)
		if err != nil {
			logrus.Errorf("Failed to get app code from API: %v", err)
			dialog.ShowError("KTracker", "Login failed: "+err.Error())
			// Continue loop to reopen login dialog
			continue
		}

		// Save email, app_code and settings to config
		st.cfg.User.Email = email
		st.cfg.User.Username = email
		st.cfg.User.AppCode = loginResp.AppCode
		st.cfg.User.IsLoggedIn = true
		st.cfg.User.Screenshots = loginResp.Screenshots
		st.cfg.User.Logout = loginResp.Logout
		st.cfg.User.ExitApp = loginResp.ExitApp

		// Save user data to AppData folder (not config.yaml)
		if err := st.cfg.SaveUserData(); err != nil {
			logrus.Warnf("Failed to save user data: %v", err)
		}

		// Update database app_code and email if callback is set
		if st.onAppCodeUpdate != nil {
			st.onAppCodeUpdate(loginResp.AppCode, email)
		}

		logrus.Infof("User logged in with email: %s, app_code: %s, screenshots: %v, logout: %v, exit_app: %v",
			email, loginResp.AppCode, loginResp.Screenshots, loginResp.Logout, loginResp.ExitApp)

		// Update the tray icon
		st.updateIcon()

		// Update menu visibility to show logged-in items
		st.updateMenuVisibility()

		st.ShowNotification("Login Successful", "Your time is being tracked")
		return
	}
}

// LoginResponse holds the response from the login API
type LoginResponse struct {
	AppCode     string
	Screenshots bool
	Logout      bool
	ExitApp     bool
}

// sendLoginToAPI sends login email to PHP endpoint and retrieves app_code and settings
func (st *SystemTray) sendLoginToAPI(email string) (*LoginResponse, error) {
	if st.cfg.API.BaseURL == "" {
		return nil, fmt.Errorf("API URL not configured")
	}

	// Use API base URL directly
	apiURL := st.cfg.API.BaseURL

	// Prepare request data with function=get_app_code
	data := map[string]string{
		"function": "get_app_code",
		"email":    email,
	}

	// Use encoder to prevent escaping of special characters like &
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(data); err != nil {
		return nil, fmt.Errorf("failed to marshal data: %w", err)
	}
	jsonData := bytes.TrimSpace(buf.Bytes())

	// Log request details
	logrus.Info("========== LOGIN API REQUEST ==========")
	logrus.Infof("URL: %s", apiURL)
	logrus.Infof("Method: POST")
	logrus.Infof("Content-Type: application/json")
	logrus.Infof("Request Body: %s", string(jsonData))
	logrus.Info("========================================")

	// Create request to capture headers
	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "KTracker/1.0")

	// Log request headers
	logrus.Info("Request Headers:")
	for key, values := range req.Header {
		for _, value := range values {
			logrus.Infof("  %s: %s", key, value)
		}
	}

	// Send request
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logrus.Errorf("Request failed: %v", err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Log response details
	logrus.Info("========== LOGIN API RESPONSE ==========")
	logrus.Infof("Status: %s (%d)", resp.Status, resp.StatusCode)
	logrus.Info("Response Headers:")
	for key, values := range resp.Header {
		for _, value := range values {
			logrus.Infof("  %s: %s", key, value)
		}
	}
	logrus.Infof("Response Body: %s", string(body))
	logrus.Info("=========================================")

	// Parse JSON response
	var response struct {
		Success     bool   `json:"success"`
		AppCode     string `json:"app_code"`
		Screenshots int    `json:"screenshots"`
		Logout      int    `json:"logout"`
		ExitApp     int    `json:"exit_app"`
		Message     string `json:"message"`
		Error       string `json:"error"`
		ErrorCode   string `json:"error_code"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		logrus.Errorf("Failed to parse JSON response: %v", err)
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Log parsed response
	logrus.Info("========== PARSED RESPONSE ==========")
	logrus.Infof("Success: %v", response.Success)
	logrus.Infof("AppCode: %s", response.AppCode)
	logrus.Infof("Message: %s", response.Message)
	logrus.Infof("Error: %s", response.Error)
	logrus.Infof("ErrorCode: %s", response.ErrorCode)
	logrus.Info("======================================")

	// Check for errors
	if !response.Success {
		if response.ErrorCode == "USER_NOT_FOUND" {
			return nil, fmt.Errorf("user not found or inactive")
		}
		return nil, fmt.Errorf("%s", response.Error)
	}

	logrus.Infof("LOGIN SUCCESS - App code retrieved: %s, Screenshots: %d, Logout: %d, ExitApp: %d",
		response.AppCode, response.Screenshots, response.Logout, response.ExitApp)

	return &LoginResponse{
		AppCode:     response.AppCode,
		Screenshots: response.Screenshots == 1,
		Logout:      response.Logout == 1,
		ExitApp:     response.ExitApp == 1,
	}, nil
}

// handleShowDesktime opens the Desktime dashboard with app code
func (st *SystemTray) handleShowDesktime() {
	if st.cfg.User.AppCode != "" {
		desktimeURL := fmt.Sprintf("%s/?code=%s", st.cfg.Website.BaseURL, st.cfg.User.AppCode)
		logrus.Infof("Opening Desktime dashboard: %s", desktimeURL)
		st.openURL(desktimeURL)
	} else {
		logrus.Warn("Cannot open Desktime - no app code available")
	}
}

// handleLogout clears user data
func (st *SystemTray) handleLogout() {
	logrus.Info("User requested logout")

	// Clear user data in config
	st.cfg.User.Username = ""
	st.cfg.User.Email = ""
	st.cfg.User.AppCode = ""
	st.cfg.User.IsLoggedIn = false

	// Save cleared user data to AppData folder
	if err := st.cfg.SaveUserData(); err != nil {
		logrus.Errorf("Failed to save user data after logout: %v", err)
	}

	// Clear app_code and email in database
	if st.onAppCodeUpdate != nil {
		st.onAppCodeUpdate("", "")
	}

	// Update icon to red
	st.updateIcon()

	// Update menu visibility to show login item
	st.updateMenuVisibility()

	logrus.Info("User logged out successfully")
	st.ShowNotification("Logged Out", "You have been logged out")

	// Show login dialog after logout
	st.handleLogin()
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

// ShowNotification shows a system notification using go-toast (Windows)
func (st *SystemTray) ShowNotification(title, message string) {
	n := toast.Notification{
		AppID: "KTracker",
		Title: title,
		Body:  message,
	}
	if err := n.Push(); err != nil {
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