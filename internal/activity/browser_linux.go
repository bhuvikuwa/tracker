//go:build linux
// +build linux

package activity

import (
	"strings"

	"desktime-tracker/internal/config"
)

// BrowserManager manages browser-specific functionality
type BrowserManager struct {
	cfg              *config.Config
	browserProcesses map[string]bool
	uiaExtractor     *URLExtractorUIA
}

// NewBrowserManager creates a new browser manager
func NewBrowserManager(cfg *config.Config) *BrowserManager {
	browserProcesses := map[string]bool{
		// Linux browser process names
		"google-chrome":        true,
		"google-chrome-stable": true,
		"chrome":               true,
		"chromium":             true,
		"chromium-browser":     true,
		"firefox":              true,
		"firefox-esr":          true,
		"microsoft-edge":       true,
		"microsoft-edge-stable": true,
		"opera":                true,
		"brave":                true,
		"brave-browser":        true,
		"vivaldi":              true,
		"vivaldi-stable":       true,
		"epiphany":             true, // GNOME Web
		"konqueror":            true,
		"midori":               true,
		"falkon":               true,
		"qutebrowser":          true,
		"tor-browser":          true,
	}

	// Create Linux-based URL extractor
	uiaExtractor := NewURLExtractorUIA()
	if err := uiaExtractor.Initialize(); err != nil {
		// Log error but continue - will fall back to title parsing
	}

	return &BrowserManager{
		cfg:              cfg,
		browserProcesses: browserProcesses,
		uiaExtractor:     uiaExtractor,
	}
}

// Cleanup releases resources
func (bm *BrowserManager) Cleanup() {
	if bm.uiaExtractor != nil {
		bm.uiaExtractor.Cleanup()
	}
}

// IsBrowser checks if a process is a browser
func (bm *BrowserManager) IsBrowser(processName string) bool {
	processName = strings.ToLower(processName)

	// Check direct match
	if bm.browserProcesses[processName] {
		return true
	}

	// Check partial match for common browser names
	browserKeywords := []string{
		"chrome", "chromium", "firefox", "edge", "opera",
		"brave", "vivaldi", "safari", "epiphany", "konqueror",
	}

	for _, keyword := range browserKeywords {
		if strings.Contains(processName, keyword) {
			return true
		}
	}

	return false
}

// GetBrowserInfo extracts URL and title from browser window using Linux tools
func (bm *BrowserManager) GetBrowserInfo(hwnd uintptr, pid int32, processName, windowTitle string) (url, title string) {
	processName = strings.ToLower(processName)

	// Try accessibility/xdotool first (gets real URL from browser)
	if bm.uiaExtractor != nil {
		url = bm.uiaExtractor.GetBrowserURL(pid, processName)
		if url != "" {
			title = bm.extractCleanTitle(processName, windowTitle)
			return url, title
		}
	}

	// Fallback to title parsing if accessibility fails
	return bm.fallbackGetBrowserInfo(processName, windowTitle)
}
