//go:build darwin
// +build darwin

package activity

import (
	"os/exec"
	"strings"
	"sync"
)

// URLExtractorUIA uses AppleScript to get browser URLs on macOS
type URLExtractorUIA struct {
	mu          sync.Mutex
	initialized bool
}

// NewURLExtractorUIA creates a new AppleScript based URL extractor for macOS
func NewURLExtractorUIA() *URLExtractorUIA {
	return &URLExtractorUIA{}
}

// Initialize initializes the URL extractor (no-op on macOS, AppleScript doesn't need init)
func (u *URLExtractorUIA) Initialize() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.initialized = true
	return nil
}

// Cleanup releases resources (no-op on macOS)
func (u *URLExtractorUIA) Cleanup() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.initialized = false
}

// GetBrowserURL gets the URL from a browser window using AppleScript
func (u *URLExtractorUIA) GetBrowserURL(pid int32, processName string) string {
	u.mu.Lock()
	defer u.mu.Unlock()

	if !u.initialized {
		return ""
	}

	processName = strings.ToLower(processName)

	var script string
	switch {
	case strings.Contains(processName, "google chrome"):
		script = u.getChromeScript()
	case strings.Contains(processName, "safari"):
		script = u.getSafariScript()
	case strings.Contains(processName, "firefox"):
		script = u.getFirefoxScript()
	case strings.Contains(processName, "microsoft edge"):
		script = u.getEdgeScript()
	case strings.Contains(processName, "brave"):
		script = u.getBraveScript()
	case strings.Contains(processName, "opera"):
		script = u.getOperaScript()
	case strings.Contains(processName, "vivaldi"):
		script = u.getVivaldiScript()
	case strings.Contains(processName, "arc"):
		script = u.getArcScript()
	default:
		// Try Chrome-based script as fallback
		script = u.getChromeScript()
	}

	if script == "" {
		return ""
	}

	url := u.runAppleScript(script)
	return normalizeURL(url)
}

// runAppleScript executes an AppleScript and returns the result
func (u *URLExtractorUIA) runAppleScript(script string) string {
	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// getChromeScript returns AppleScript for Google Chrome
func (u *URLExtractorUIA) getChromeScript() string {
	return `
		tell application "Google Chrome"
			if (count of windows) > 0 then
				return URL of active tab of front window
			end if
		end tell
		return ""
	`
}

// getSafariScript returns AppleScript for Safari
func (u *URLExtractorUIA) getSafariScript() string {
	return `
		tell application "Safari"
			if (count of windows) > 0 then
				return URL of front document
			end if
		end tell
		return ""
	`
}

// getFirefoxScript returns AppleScript for Firefox
// Note: Firefox has limited AppleScript support, this may not work in all cases
func (u *URLExtractorUIA) getFirefoxScript() string {
	return `
		tell application "System Events"
			tell process "Firefox"
				if (count of windows) > 0 then
					-- Try to get URL from toolbar
					try
						set urlField to text field 1 of toolbar 1 of window 1
						return value of urlField
					end try
				end if
			end tell
		end tell
		return ""
	`
}

// getEdgeScript returns AppleScript for Microsoft Edge
func (u *URLExtractorUIA) getEdgeScript() string {
	return `
		tell application "Microsoft Edge"
			if (count of windows) > 0 then
				return URL of active tab of front window
			end if
		end tell
		return ""
	`
}

// getBraveScript returns AppleScript for Brave Browser
func (u *URLExtractorUIA) getBraveScript() string {
	return `
		tell application "Brave Browser"
			if (count of windows) > 0 then
				return URL of active tab of front window
			end if
		end tell
		return ""
	`
}

// getOperaScript returns AppleScript for Opera
func (u *URLExtractorUIA) getOperaScript() string {
	return `
		tell application "Opera"
			if (count of windows) > 0 then
				return URL of active tab of front window
			end if
		end tell
		return ""
	`
}

// getVivaldiScript returns AppleScript for Vivaldi
func (u *URLExtractorUIA) getVivaldiScript() string {
	return `
		tell application "Vivaldi"
			if (count of windows) > 0 then
				return URL of active tab of front window
			end if
		end tell
		return ""
	`
}

// getArcScript returns AppleScript for Arc Browser
func (u *URLExtractorUIA) getArcScript() string {
	return `
		tell application "Arc"
			if (count of windows) > 0 then
				return URL of active tab of front window
			end if
		end tell
		return ""
	`
}

// normalizeURL ensures the URL has a proper protocol
func normalizeURL(url string) string {
	url = strings.TrimSpace(url)

	if url == "" {
		return ""
	}

	// Already has protocol
	if strings.HasPrefix(url, "http://") ||
		strings.HasPrefix(url, "https://") ||
		strings.HasPrefix(url, "file://") ||
		strings.HasPrefix(url, "about:") ||
		strings.HasPrefix(url, "chrome://") ||
		strings.HasPrefix(url, "edge://") {
		return url
	}

	// Add https:// by default
	if strings.Contains(url, ".") && !strings.Contains(url, " ") {
		return "https://" + url
	}

	return url
}
