package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"ktracker/internal/buffer"
	"ktracker/internal/config"
	"ktracker/internal/models"
	"ktracker/internal/utils"

	"github.com/sirupsen/logrus"
)

// Database handles data storage operations via HTTP API
type Database struct {
	apiURL                 string
	appCode                string
	email                  string
	appVersion             string    // App version to send with every request
	httpClient             *http.Client
	onLogoutCallback       func() // Callback to trigger logout when API returns failure
	onNotLoggedInCallback  func() // Callback when data is skipped due to no login
	lastNotLoggedInNotify  time.Time // Rate limit notifications
	activityBuffer         *buffer.ActivityBuffer // Local SQLite buffer for failed uploads
}

// NewDatabase creates a new database connection
func NewDatabase(cfg config.APIConfig) (*Database, error) {
	database := &Database{
		apiURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	// Initialize activity buffer for offline/failure resilience
	actBuffer, err := buffer.NewActivityBuffer()
	if err != nil {
		logrus.Warnf("Failed to initialize activity buffer (activities won't be buffered): %v", err)
	} else {
		database.activityBuffer = actBuffer
		logrus.Info("Activity buffer initialized for offline resilience")
	}

	logrus.Info("HTTP API client initialized successfully")
	return database, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	// Close activity buffer if initialized
	if d.activityBuffer != nil {
		if err := d.activityBuffer.Close(); err != nil {
			logrus.Warnf("Error closing activity buffer: %v", err)
		}
	}
	return nil
}

// GetActivityBuffer returns the activity buffer for use by sync worker
func (d *Database) GetActivityBuffer() *buffer.ActivityBuffer {
	return d.activityBuffer
}

// SetAppCode sets the app code for authentication
func (d *Database) SetAppCode(appCode string) {
	d.appCode = appCode
	logrus.Infof("App code updated for API requests: %s", appCode)
}

// SetEmail sets the email for API requests
func (d *Database) SetEmail(email string) {
	d.email = email
	logrus.Infof("Email updated for API requests: %s", email)
}

// SetAppVersion sets the app version for API requests
func (d *Database) SetAppVersion(version string) {
	d.appVersion = version
	logrus.Infof("App version set for API requests: %s", version)
}

// SetAPIURL updates the API URL for requests (can be changed remotely)
func (d *Database) SetAPIURL(url string) {
	if url != "" && url != d.apiURL {
		logrus.Infof("API URL updated: %s -> %s", d.apiURL, url)
		d.apiURL = url
	}
}

// GetAPIURL returns the current API URL
func (d *Database) GetAPIURL() string {
	return d.apiURL
}

// SetLogoutCallback sets the callback function to trigger logout on API failure
func (d *Database) SetLogoutCallback(callback func()) {
	d.onLogoutCallback = callback
}

// SetNotLoggedInCallback sets the callback function to notify when data is skipped due to no login
func (d *Database) SetNotLoggedInCallback(callback func()) {
	d.onNotLoggedInCallback = callback
}

// notifyNotLoggedIn calls the not logged in callback (rate limited to once per 5 minutes)
func (d *Database) notifyNotLoggedIn() {
	if d.onNotLoggedInCallback == nil {
		return
	}
	// Rate limit: only notify once every 30 minutes
	if time.Since(d.lastNotLoggedInNotify) >= 30*time.Minute {
		d.lastNotLoggedInNotify = time.Now()
		d.onNotLoggedInCallback()
	}
}

// triggerLogout calls the logout callback if set
func (d *Database) triggerLogout() {
	if d.onLogoutCallback != nil {
		logrus.Warn("Triggering logout due to API failure")
		d.onLogoutCallback()
	}
}

// bufferActivity stores an activity in the local SQLite buffer for later retry
func (d *Database) bufferActivity(payload map[string]interface{}) {
	if d.activityBuffer == nil {
		logrus.Warn("Activity buffer not available - activity will be lost")
		return
	}

	if err := d.activityBuffer.BufferActivity(d.email, payload); err != nil {
		logrus.Errorf("Failed to buffer activity: %v", err)
	} else {
		logrus.Info("Activity buffered for later sync")
	}
}

// InsertActivity inserts an activity record via HTTP POST
func (d *Database) InsertActivity(activity *models.Activity) error {
	// Don't send activity if email is not set (user not logged in)
	if d.email == "" {
		logrus.Debug("Skipping activity insert - email not set (user not logged in)")
		d.notifyNotLoggedIn()
		return nil
	}

	// Log the times being sent
	logrus.Infof("SENDING ACTIVITY: app=%s | start=%s (%d) | end=%s (%d) | duration=%ds",
		activity.AppName,
		activity.StartTime.Format("15:04:05"), activity.StartTime.UTC().Unix(),
		activity.EndTime.Format("15:04:05"), activity.EndTime.UTC().Unix(),
		activity.DurationSeconds)

	payload := map[string]interface{}{
		"function":    "submit_activity",
		"email":       d.email,
		"app_version": d.appVersion,
		"data": map[string]interface{}{
			"app_name":         activity.AppName,
			"window_title":     activity.WindowTitle,
			"browser_url":      activity.BrowserURL,
			"browser_title":    activity.BrowserTitle,
			"start_time":       activity.StartTime.UTC().Unix(),
			"end_time":         activity.EndTime.UTC().Unix(),
			"timezone":         utils.GetSystemTimezone(),
			"duration_seconds": activity.DurationSeconds,
			"is_active":        activity.IsActive,
			"mouse_clicks":     activity.MouseClicks,
			"keystrokes":       activity.Keystrokes,
			"mouse_distance":   activity.MouseDistance,
			"app_icon":         activity.AppIcon, // Base64-encoded PNG icon
		},
	}

	// Use encoder to prevent escaping of special characters like &
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return fmt.Errorf("failed to marshal activity data: %w", err)
	}
	jsonData := bytes.TrimSpace(buf.Bytes())

	// Debug: Log request details
	logrus.Info("========== ACTIVITY API REQUEST ==========")
	logrus.Infof("URL: %s", d.apiURL)
	logrus.Infof("Method: POST")
	logrus.Infof("Content-Type: application/json")
	logrus.Infof("Request Body: %s", string(jsonData))
	logrus.Info("==========================================")

	resp, err := d.httpClient.Post(d.apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		logrus.Errorf("Failed to send activity data: %v", err)
		// Buffer the activity for later retry
		d.bufferActivity(payload)
		return nil // Return success to caller - activity is buffered
	}
	defer resp.Body.Close()

	// Read response body
	body, _ := io.ReadAll(resp.Body)

	// Debug: Log response details
	logrus.Info("========== ACTIVITY API RESPONSE ==========")
	logrus.Infof("Status: %s (%d)", resp.Status, resp.StatusCode)
	logrus.Info("Response Headers:")
	for key, values := range resp.Header {
		for _, value := range values {
			logrus.Infof("  %s: %s", key, value)
		}
	}
	logrus.Infof("Response Body: %s", string(body))
	logrus.Info("===========================================")

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		logrus.Warnf("Failed to insert activity, status: %d", resp.StatusCode)
		// Buffer the activity for later retry
		d.bufferActivity(payload)
		return nil // Return success to caller - activity is buffered
	}

	// Parse JSON response to check success status
	var apiResponse struct {
		Success   bool   `json:"success"`
		Message   string `json:"message"`
		Error     string `json:"error"`
		ErrorCode string `json:"error_code"`
	}

	if err := json.Unmarshal(body, &apiResponse); err != nil {
		logrus.Warnf("Failed to parse activity response: %v", err)
		return nil
	}

	// If API returns success=false, check if it's an auth error or transient error
	if !apiResponse.Success {
		logrus.Errorf("Activity submission failed - API returned success=false. Error: %s, ErrorCode: %s", apiResponse.Error, apiResponse.ErrorCode)

		// Only trigger logout for authentication errors, buffer for other errors
		if apiResponse.ErrorCode == "invalid_credentials" || apiResponse.ErrorCode == "unauthorized" {
			d.triggerLogout()
		} else {
			// Buffer the activity for later retry (server-side error, rate limit, etc.)
			d.bufferActivity(payload)
		}
		return nil
	}

	logrus.Info("Activity submitted successfully")
	return nil
}

// ScreenshotUploadResult contains the result of a screenshot upload
type ScreenshotUploadResult struct {
	Success      bool
	ScreenshotID int
	FilePath     string
	CapturedAt   string
	Error        string
}

// InsertScreenshot uploads a screenshot as base64 JSON (like app_icon)
// Returns ScreenshotUploadResult with success status
// displayIndex: 0-based index of the display (0 = primary, 1 = secondary, etc.)
// totalDisplays: total number of displays being captured
// appName: current active application name
// browserURL: current browser URL (if screenshot is from a browser)
// appIcon: base64 encoded app icon
func (d *Database) InsertScreenshot(screenshotBase64 string, displayIndex int, totalDisplays int, appName string, browserURL string, appIcon string) (*ScreenshotUploadResult, error) {
	result := &ScreenshotUploadResult{Success: false}

	// Don't send screenshot if email is not set (user not logged in)
	if d.email == "" {
		logrus.Debug("Skipping screenshot insert - email not set (user not logged in)")
		d.notifyNotLoggedIn()
		return result, nil
	}

	// Create JSON payload
	payload := map[string]interface{}{
		"function":       "submit_screenshot",
		"email":          d.email,
		"app_version":    d.appVersion,
		"screenshot":     screenshotBase64,
		"captured_at":    time.Now().UTC().Unix(),
		"timezone":       utils.GetSystemTimezone(),
		"display_index":  displayIndex,
		"total_displays": totalDisplays,
		"app_name":       appName,
		"browser_url":    browserURL,
		"app_icon":       appIcon,
	}

	// Encode JSON
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return result, fmt.Errorf("failed to marshal screenshot data: %w", err)
	}
	jsonData := bytes.TrimSpace(buf.Bytes())

	// Debug: Log request details
	logrus.Info("========== SCREENSHOT API REQUEST ==========")
	logrus.Infof("URL: %s", d.apiURL)
	logrus.Infof("Method: POST")
	logrus.Infof("Content-Type: application/json")
	logrus.Infof("Email: %s", d.email)
	logrus.Infof("Screenshot base64 length: %d", len(screenshotBase64))
	logrus.Info("============================================")

	// Send request
	resp, err := d.httpClient.Post(d.apiURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		logrus.Errorf("Failed to send screenshot: %v", err)
		return result, fmt.Errorf("failed to send screenshot: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	responseBody, _ := io.ReadAll(resp.Body)

	// Debug: Log response details
	logrus.Info("========== SCREENSHOT API RESPONSE ==========")
	logrus.Infof("Status: %s (%d)", resp.Status, resp.StatusCode)
	logrus.Info("Response Headers:")
	for key, values := range resp.Header {
		for _, value := range values {
			logrus.Infof("  %s: %s", key, value)
		}
	}
	logrus.Infof("Response Body: %s", string(responseBody))
	logrus.Info("=============================================")

	// Parse JSON response
	var apiResponse struct {
		Success      bool   `json:"success"`
		Message      string `json:"message"`
		ScreenshotID int    `json:"screenshot_id"`
		FilePath     string `json:"file_path"`
		CapturedAt   string `json:"captured_at"`
		Error        string `json:"error"`
		ErrorCode    string `json:"error_code"`
	}

	if err := json.Unmarshal(responseBody, &apiResponse); err != nil {
		return result, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for API errors - just log and skip, don't return error
	if !apiResponse.Success {
		result.Error = apiResponse.Error
		logrus.Warnf("Screenshot upload failed: %s", apiResponse.Error)
		return result, nil // Don't return error, just skip
	}

	// Success
	result.Success = true
	result.ScreenshotID = apiResponse.ScreenshotID
	result.FilePath = apiResponse.FilePath
	result.CapturedAt = apiResponse.CapturedAt

	logrus.Infof("Screenshot uploaded successfully: ID=%d, Path=%s", result.ScreenshotID, result.FilePath)

	return result, nil
}

