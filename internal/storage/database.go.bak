package storage

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"desktime-tracker/internal/config"
	"desktime-tracker/internal/models"
	"desktime-tracker/internal/utils"

	"github.com/sirupsen/logrus"
)

// Database handles data storage operations via HTTP API
type Database struct {
	apiURL                 string
	appCode                string
	email                  string
	httpClient             *http.Client
	onLogoutCallback       func() // Callback to trigger logout when API returns failure
	onNotLoggedInCallback  func() // Callback when data is skipped due to no login
	lastNotLoggedInNotify  time.Time // Rate limit notifications
}

// NewDatabase creates a new database connection
func NewDatabase(cfg config.APIConfig) (*Database, error) {
	database := &Database{
		apiURL: cfg.BaseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	logrus.Info("HTTP API client initialized successfully")
	return database, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	// Nothing to close for HTTP client
	return nil
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

// InsertActivity inserts an activity record via HTTP POST
func (d *Database) InsertActivity(activity *models.Activity) error {
	// Don't send activity if email is not set (user not logged in)
	if d.email == "" {
		logrus.Debug("Skipping activity insert - email not set (user not logged in)")
		d.notifyNotLoggedIn()
		return nil
	}

	payload := map[string]interface{}{
		"function": "submit_activity",
		"email":    d.email,
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
		return fmt.Errorf("failed to send activity data: %w", err)
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
		return nil // Don't return error, just skip
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

	// If API returns success=false, trigger logout
	if !apiResponse.Success {
		logrus.Errorf("Activity submission failed - API returned success=false. Error: %s, ErrorCode: %s", apiResponse.Error, apiResponse.ErrorCode)
		d.triggerLogout()
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
func (d *Database) InsertScreenshot(screenshotBase64 string, displayIndex int, totalDisplays int) (*ScreenshotUploadResult, error) {
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
		"screenshot":     screenshotBase64,
		"captured_at":    time.Now().UTC().Unix(),
		"timezone":       utils.GetSystemTimezone(),
		"display_index":  displayIndex,
		"total_displays": totalDisplays,
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

// GetRecentActivity gets recent activity records (not implemented for HTTP API)
func (d *Database) GetRecentActivity(userID int, limit int) ([]models.Activity, error) {
	logrus.Warn("GetRecentActivity not implemented for HTTP API")
	return []models.Activity{}, nil
}

// GetRecentScreenshots gets recent screenshot records (not implemented for HTTP API)
func (d *Database) GetRecentScreenshots(userID int, limit int) ([]models.Screenshot, error) {
	logrus.Warn("GetRecentScreenshots not implemented for HTTP API")
	return []models.Screenshot{}, nil
}

// GetDailyActivitySummary gets daily activity summary (not implemented for HTTP API)
func (d *Database) GetDailyActivitySummary(userID int, date string) (map[string]interface{}, error) {
	logrus.Warn("GetDailyActivitySummary not implemented for HTTP API")
	return make(map[string]interface{}), nil
}

// CleanupOldData removes old data based on retention policy (not implemented for HTTP API)
func (d *Database) CleanupOldData(retentionDays int) error {
	logrus.Debug("CleanupOldData not applicable for HTTP API")
	return nil
}
