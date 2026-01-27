//go:build windows
// +build windows

package activity

import (
	"strings"

	"desktime-tracker/internal/config"

	"github.com/lxn/win"
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
		"chrome.exe":                true,
		"firefox.exe":               true,
		"msedge.exe":                true,
		"opera.exe":                 true,
		"brave.exe":                 true,
		"vivaldi.exe":               true,
		"iexplore.exe":              true,
		"applicationframehost.exe": true,
	}

	// Create UI Automation extractor
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
	return bm.browserProcesses[strings.ToLower(processName)]
}

// GetBrowserInfo extracts URL and title from browser window using UI Automation
func (bm *BrowserManager) GetBrowserInfo(hwnd uintptr, pid int32, processName, windowTitle string) (url, title string) {
	processName = strings.ToLower(processName)

	// Try UI Automation first (gets real URL from address bar)
	if bm.uiaExtractor != nil {
		url = bm.uiaExtractor.GetBrowserURL(win.HWND(hwnd), processName)
		if url != "" {
			title = bm.extractCleanTitle(processName, windowTitle)
			return url, title
		}
	}

	// Fallback to title parsing if UI Automation fails
	return bm.fallbackGetBrowserInfo(processName, windowTitle)
}
