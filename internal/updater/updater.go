package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// RemoteConfig represents the configuration received from the server
type RemoteConfig struct {
	Success bool   `json:"success"`
	Config  Config `json:"config"`
}

// Config holds the remote configuration values
type Config struct {
	// Version info
	LatestVersion string `json:"latest_version"`
	DownloadURL   string `json:"download_url"`
	ForceUpdate   bool   `json:"force_update"`
	MinVersion    string `json:"min_version"`

	// URL settings
	APIBaseURL string `json:"api_base_url"`
	BaseURL    string `json:"base_url"` // Website base URL

	// Tracking settings
	IdleTimeout int `json:"idle_timeout"`
}

// Updater handles checking for updates and downloading new versions
type Updater struct {
	configURL      string
	currentVersion string
	email          string
	httpClient     *http.Client
}

// NewUpdater creates a new Updater instance
func NewUpdater(configURL, currentVersion string) *Updater {
	return &Updater{
		configURL:      configURL,
		currentVersion: currentVersion,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// SetEmail sets the user email to include in config requests
func (u *Updater) SetEmail(email string) {
	u.email = email
}

// FetchRemoteConfig fetches the remote configuration from the server
func (u *Updater) FetchRemoteConfig() (*Config, error) {
	fetchURL := u.configURL
	if u.email != "" {
		fetchURL += "?email=" + u.email
	}
	logrus.Infof("Fetching remote config from: %s", fetchURL)

	resp, err := u.httpClient.Get(fetchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch remote config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote config returned status: %d", resp.StatusCode)
	}

	var remoteConfig RemoteConfig
	if err := json.NewDecoder(resp.Body).Decode(&remoteConfig); err != nil {
		return nil, fmt.Errorf("failed to decode remote config: %w", err)
	}

	if !remoteConfig.Success {
		return nil, fmt.Errorf("remote config returned success=false")
	}

	logrus.Infof("Remote config fetched successfully. Latest version: %s", remoteConfig.Config.LatestVersion)
	return &remoteConfig.Config, nil
}

// CheckForUpdate checks if an update is available
// Returns: (updateAvailable, forceUpdate, error)
func (u *Updater) CheckForUpdate() (bool, bool, *Config, error) {
	config, err := u.FetchRemoteConfig()
	if err != nil {
		return false, false, nil, err
	}

	updateAvailable := compareVersions(config.LatestVersion, u.currentVersion) > 0
	forceUpdate := config.ForceUpdate || compareVersions(u.currentVersion, config.MinVersion) < 0

	if updateAvailable {
		logrus.Infof("Update available: %s -> %s (force: %v)", u.currentVersion, config.LatestVersion, forceUpdate)
	} else {
		logrus.Info("App is up to date")
	}

	return updateAvailable, forceUpdate, config, nil
}

// DownloadUpdate downloads the new exe to a temp location
// Returns the path to the downloaded file
func (u *Updater) DownloadUpdate(downloadURL string) (string, error) {
	logrus.Infof("Downloading update from: %s", downloadURL)

	// Get the AppData directory for downloads
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		appData = os.Getenv("APPDATA")
	}
	if appData == "" {
		return "", fmt.Errorf("could not find AppData folder")
	}

	// Create update directory
	updateDir := filepath.Join(appData, "KTracker", "update")
	if err := os.MkdirAll(updateDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create update directory: %w", err)
	}

	// Download file
	resp, err := u.httpClient.Get(downloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download update: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned status: %d", resp.StatusCode)
	}

	// Create temp file for download
	newExePath := filepath.Join(updateDir, "KTracker_new.exe")
	out, err := os.Create(newExePath)
	if err != nil {
		return "", fmt.Errorf("failed to create download file: %w", err)
	}
	defer out.Close()

	// Copy with progress logging
	written, err := io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(newExePath)
		return "", fmt.Errorf("failed to write download: %w", err)
	}

	logrus.Infof("Downloaded %d bytes to: %s", written, newExePath)
	return newExePath, nil
}

// ApplyUpdate replaces the running exe using rename-in-place (no CMD window needed).
// Windows allows renaming a running executable, so we rename the current exe,
// move the new one into place, launch it, and exit. The new instance cleans up
// the old renamed exe on startup via CleanupOldExe().
func (u *Updater) ApplyUpdate(newExePath string) error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	exeDir := filepath.Dir(exePath)
	oldExePath := filepath.Join(exeDir, "KTracker_old.exe")

	// Remove any leftover old exe from a previous update
	os.Remove(oldExePath)

	// Rename running exe (Windows allows this)
	if err := os.Rename(exePath, oldExePath); err != nil {
		return fmt.Errorf("failed to rename current exe: %w", err)
	}

	// Move new exe into place
	if err := os.Rename(newExePath, exePath); err != nil {
		// Rollback: restore old exe
		os.Rename(oldExePath, exePath)
		return fmt.Errorf("failed to move new exe into place: %w", err)
	}

	// Launch the new exe
	cmd := exec.Command(exePath)
	cmd.Dir = exeDir
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start new exe: %w", err)
	}

	logrus.Info("New version launched, exiting current process...")
	return nil
}

// CleanupOldExe removes the old exe left over from a previous update.
// Call this on startup.
func CleanupOldExe() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	oldExePath := filepath.Join(filepath.Dir(exePath), "KTracker_old.exe")
	os.Remove(oldExePath)
}

// PerformUpdate checks for updates and applies them if available
// Returns true if the app should exit for update
func (u *Updater) PerformUpdate() (bool, error) {
	updateAvailable, forceUpdate, config, err := u.CheckForUpdate()
	if err != nil {
		logrus.Warnf("Failed to check for updates: %v", err)
		return false, err
	}

	if !updateAvailable {
		return false, nil
	}

	// Download the update
	newExePath, err := u.DownloadUpdate(config.DownloadURL)
	if err != nil {
		logrus.Errorf("Failed to download update: %v", err)
		if forceUpdate {
			return false, fmt.Errorf("forced update failed: %w", err)
		}
		return false, err
	}

	// Apply the update
	if err := u.ApplyUpdate(newExePath); err != nil {
		logrus.Errorf("Failed to apply update: %v", err)
		os.Remove(newExePath)
		if forceUpdate {
			return false, fmt.Errorf("forced update failed: %w", err)
		}
		return false, err
	}

	logrus.Info("Update applied successfully, app will restart...")
	return true, nil
}

// compareVersions compares two version strings
// Returns: 1 if v1 > v2, -1 if v1 < v2, 0 if equal
func compareVersions(v1, v2 string) int {
	// Remove 'v' prefix if present
	v1 = strings.TrimPrefix(v1, "v")
	v2 = strings.TrimPrefix(v2, "v")

	parts1 := strings.Split(v1, ".")
	parts2 := strings.Split(v2, ".")

	// Pad shorter version with zeros
	for len(parts1) < len(parts2) {
		parts1 = append(parts1, "0")
	}
	for len(parts2) < len(parts1) {
		parts2 = append(parts2, "0")
	}

	for i := 0; i < len(parts1); i++ {
		var n1, n2 int
		fmt.Sscanf(parts1[i], "%d", &n1)
		fmt.Sscanf(parts2[i], "%d", &n2)

		if n1 > n2 {
			return 1
		}
		if n1 < n2 {
			return -1
		}
	}

	return 0
}

// GetCurrentVersion returns the current version
func (u *Updater) GetCurrentVersion() string {
	return u.currentVersion
}
