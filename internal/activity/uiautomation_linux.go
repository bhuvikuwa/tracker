//go:build linux
// +build linux

package activity

import (
	"os/exec"
	"strings"
	"sync"
)

// URLExtractorUIA uses various Linux tools to get browser URLs
type URLExtractorUIA struct {
	mu          sync.Mutex
	initialized bool
	hasXdotool  bool
	hasXprop    bool
	hasWmctrl   bool
	hasGdbus    bool
}

// NewURLExtractorUIA creates a new URL extractor for Linux
func NewURLExtractorUIA() *URLExtractorUIA {
	return &URLExtractorUIA{}
}

// Initialize checks for available tools on Linux
func (u *URLExtractorUIA) Initialize() error {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Check for available tools
	u.hasXdotool = u.commandExists("xdotool")
	u.hasXprop = u.commandExists("xprop")
	u.hasWmctrl = u.commandExists("wmctrl")
	u.hasGdbus = u.commandExists("gdbus")

	u.initialized = true
	return nil
}

// commandExists checks if a command is available
func (u *URLExtractorUIA) commandExists(cmd string) bool {
	_, err := exec.LookPath(cmd)
	return err == nil
}

// Cleanup releases resources (no-op on Linux)
func (u *URLExtractorUIA) Cleanup() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.initialized = false
}

// GetBrowserURL gets the URL from a browser window using Linux tools
func (u *URLExtractorUIA) GetBrowserURL(pid int32, processName string) string {
	u.mu.Lock()
	defer u.mu.Unlock()

	if !u.initialized {
		return ""
	}

	processName = strings.ToLower(processName)

	// Try different methods based on browser
	var url string

	switch {
	case strings.Contains(processName, "chrome") || strings.Contains(processName, "chromium"):
		url = u.getChromeURL()
	case strings.Contains(processName, "firefox"):
		url = u.getFirefoxURL()
	case strings.Contains(processName, "brave"):
		url = u.getBraveURL()
	case strings.Contains(processName, "opera"):
		url = u.getOperaURL()
	case strings.Contains(processName, "vivaldi"):
		url = u.getVivaldiURL()
	case strings.Contains(processName, "microsoft-edge") || strings.Contains(processName, "msedge"):
		url = u.getEdgeURL()
	default:
		// Try generic approach using accessibility
		url = u.getGenericBrowserURL()
	}

	return normalizeURL(url)
}

// getChromeURL gets URL from Chrome/Chromium using remote debugging or D-Bus
func (u *URLExtractorUIA) getChromeURL() string {
	// Try using xdotool to get window name which often contains URL info
	if u.hasXdotool {
		url := u.getURLFromWindowName("chrome")
		if url != "" {
			return url
		}
	}

	// Try accessibility (AT-SPI2)
	url := u.getURLFromAccessibility("chrome")
	if url != "" {
		return url
	}

	return ""
}

// getFirefoxURL gets URL from Firefox
func (u *URLExtractorUIA) getFirefoxURL() string {
	// Firefox exposes URL through accessibility API
	if u.hasXdotool {
		url := u.getURLFromWindowName("firefox")
		if url != "" {
			return url
		}
	}

	// Try AT-SPI2 accessibility
	url := u.getURLFromAccessibility("firefox")
	if url != "" {
		return url
	}

	return ""
}

// getBraveURL gets URL from Brave browser
func (u *URLExtractorUIA) getBraveURL() string {
	if u.hasXdotool {
		url := u.getURLFromWindowName("brave")
		if url != "" {
			return url
		}
	}
	return u.getURLFromAccessibility("brave")
}

// getOperaURL gets URL from Opera browser
func (u *URLExtractorUIA) getOperaURL() string {
	if u.hasXdotool {
		url := u.getURLFromWindowName("opera")
		if url != "" {
			return url
		}
	}
	return u.getURLFromAccessibility("opera")
}

// getVivaldiURL gets URL from Vivaldi browser
func (u *URLExtractorUIA) getVivaldiURL() string {
	if u.hasXdotool {
		url := u.getURLFromWindowName("vivaldi")
		if url != "" {
			return url
		}
	}
	return u.getURLFromAccessibility("vivaldi")
}

// getEdgeURL gets URL from Microsoft Edge
func (u *URLExtractorUIA) getEdgeURL() string {
	if u.hasXdotool {
		url := u.getURLFromWindowName("msedge")
		if url != "" {
			return url
		}
	}
	return u.getURLFromAccessibility("microsoft-edge")
}

// getGenericBrowserURL tries to get URL from any browser
func (u *URLExtractorUIA) getGenericBrowserURL() string {
	// Try to get from active window using xdotool
	if u.hasXdotool {
		cmd := exec.Command("xdotool", "getactivewindow", "getwindowname")
		output, err := cmd.Output()
		if err == nil {
			windowName := strings.TrimSpace(string(output))
			url := u.extractURLFromWindowTitle(windowName)
			if url != "" {
				return url
			}
		}
	}

	return ""
}

// getURLFromWindowName tries to extract URL from window name
func (u *URLExtractorUIA) getURLFromWindowName(browserName string) string {
	if !u.hasXdotool {
		return ""
	}

	// Get active window name
	cmd := exec.Command("xdotool", "getactivewindow", "getwindowname")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	windowName := strings.TrimSpace(string(output))
	return u.extractURLFromWindowTitle(windowName)
}

// getURLFromAccessibility uses AT-SPI2 to get URL from browser
func (u *URLExtractorUIA) getURLFromAccessibility(browserName string) string {
	// Use python3 with AT-SPI2 bindings if available
	// This is a more reliable method on Linux
	script := `
import gi
gi.require_version('Atspi', '2.0')
from gi.repository import Atspi

def find_url_bar(obj, depth=0):
    if depth > 10:
        return None
    try:
        role = obj.get_role()
        # Look for entry/text field (URL bar)
        if role == Atspi.Role.ENTRY or role == Atspi.Role.TEXT:
            text = obj.get_text(0, -1) if hasattr(obj, 'get_text') else None
            if text and ('http://' in text or 'https://' in text or '.' in text):
                return text
        # Recurse into children
        for i in range(obj.get_child_count()):
            child = obj.get_child_at_index(i)
            if child:
                result = find_url_bar(child, depth + 1)
                if result:
                    return result
    except:
        pass
    return None

desktop = Atspi.get_desktop(0)
for i in range(desktop.get_child_count()):
    app = desktop.get_child_at_index(i)
    if app:
        name = app.get_name().lower()
        if '` + browserName + `' in name:
            for j in range(app.get_child_count()):
                window = app.get_child_at_index(j)
                if window:
                    url = find_url_bar(window)
                    if url:
                        print(url)
                        exit(0)
`

	cmd := exec.Command("python3", "-c", script)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

// extractURLFromWindowTitle tries to extract URL from window title
func (u *URLExtractorUIA) extractURLFromWindowTitle(title string) string {
	// Some browsers show URL in title
	// Format: "Page Title - URL - Browser Name" or "URL - Page Title - Browser"

	// Look for http:// or https://
	if idx := strings.Index(title, "http://"); idx != -1 {
		url := title[idx:]
		// Find end of URL (space or common separators)
		if endIdx := strings.IndexAny(url, " \t|—–-"); endIdx != -1 {
			return strings.TrimSpace(url[:endIdx])
		}
		return strings.TrimSpace(url)
	}

	if idx := strings.Index(title, "https://"); idx != -1 {
		url := title[idx:]
		if endIdx := strings.IndexAny(url, " \t|—–-"); endIdx != -1 {
			return strings.TrimSpace(url[:endIdx])
		}
		return strings.TrimSpace(url)
	}

	return ""
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
