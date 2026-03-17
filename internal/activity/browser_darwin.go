//go:build darwin
// +build darwin

package activity

import (
	"strings"

	"ktracker/internal/config"
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
		// macOS browser process names
		"google chrome":       true,
		"safari":              true,
		"firefox":             true,
		"microsoft edge":      true,
		"opera":               true,
		"brave browser":       true,
		"vivaldi":             true,
		"arc":                 true,
		"chromium":            true,
		"tor browser":         true,
		"google chrome canary": true,
		"firefox developer edition": true,
	}

	// Create AppleScript-based URL extractor
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

	// Check partial match for macOS app names
	for browser := range bm.browserProcesses {
		if strings.Contains(processName, browser) {
			return true
		}
	}

	return false
}

// GetBrowserInfo extracts URL and title from browser window using AppleScript
func (bm *BrowserManager) GetBrowserInfo(hwnd uintptr, pid int32, processName, windowTitle string) (url, title string) {
	processName = strings.ToLower(processName)

	// Try AppleScript first (gets real URL from browser)
	if bm.uiaExtractor != nil {
		url = bm.uiaExtractor.GetBrowserURL(pid, processName)
		if url != "" {
			title = bm.extractCleanTitle(processName, windowTitle)
			return url, title
		}
	}

	// Fallback to title parsing if AppleScript fails
	return bm.fallbackGetBrowserInfo(processName, windowTitle)
}
