package activity

import (
	"strings"
)

// extractCleanTitle extracts a clean page title from the window title
func (bm *BrowserManager) extractCleanTitle(processName, windowTitle string) string {
	processName = strings.ToLower(processName)

	suffixes := map[string]string{
		// Windows
		"chrome.exe":  " - Google Chrome",
		"firefox.exe": " — Mozilla Firefox",
		"msedge.exe":  " - Microsoft Edge",
		"opera.exe":   " - Opera",
		"brave.exe":   " - Brave",
		"vivaldi.exe": " - Vivaldi",
		// macOS/Linux
		"google chrome":   " - Google Chrome",
		"firefox":         " — Mozilla Firefox",
		"microsoft edge":  " - Microsoft Edge",
		"opera":           " - Opera",
		"brave browser":   " - Brave",
		"vivaldi":         " - Vivaldi",
		"safari":          " — Safari",
		"chromium":        " - Chromium",
		"arc":             " - Arc",
	}

	for proc, suffix := range suffixes {
		if strings.Contains(processName, proc) {
			if strings.HasSuffix(windowTitle, suffix) {
				return strings.TrimSuffix(windowTitle, suffix)
			}
		}
	}

	return windowTitle
}

// extractChromeInfoSimple - simple and effective Chrome URL/title extraction
func (bm *BrowserManager) extractChromeInfoSimple(windowTitle string) (url, title string) {
	// Chrome window titles: "Page Title - Google Chrome"
	if !strings.HasSuffix(windowTitle, " - Google Chrome") {
		return "", windowTitle
	}

	pageTitle := strings.TrimSuffix(windowTitle, " - Google Chrome")

	// Handle common patterns directly
	lowerTitle := strings.ToLower(pageTitle)

	// Direct URL extraction for localhost/phpMyAdmin
	if strings.Contains(pageTitle, "localhost") || strings.Contains(pageTitle, "127.0.0.1") {
		if strings.Contains(pageTitle, " | ") {
			parts := strings.Split(pageTitle, " | ")
			if len(parts) >= 2 {
				urlPart := strings.TrimSpace(parts[0])
				title = strings.TrimSpace(parts[1])

				// Convert to proper URL
				if strings.Contains(urlPart, " / ") {
					urlPaths := strings.Split(urlPart, " / ")
					if len(urlPaths) >= 3 {
						url = "http://localhost/" + strings.Join(urlPaths[2:], "/")
					} else {
						url = "http://localhost"
					}
				} else {
					url = "http://localhost"
				}
				return url, title
			}
		}
		url = "http://localhost"
		title = pageTitle
		return url, title
	}

	// Google Search
	if strings.Contains(lowerTitle, "google search") || strings.Contains(lowerTitle, "- google search") {
		url = "https://www.google.com"
		title = strings.Replace(pageTitle, " - Google Search", "", 1)
		return url, title
	}

	// YouTube
	if strings.Contains(lowerTitle, "youtube") || strings.Contains(pageTitle, "•") {
		url = "https://www.youtube.com"
		if strings.Contains(pageTitle, "•") {
			parts := strings.Split(pageTitle, "•")
			title = strings.TrimSpace(parts[0])
		} else {
			title = pageTitle
		}
		return url, title
	}

	// Other common sites - simple keyword matching
	sites := map[string]string{
		"chatgpt":       "https://chat.openai.com",
		"github":        "https://github.com",
		"stackoverflow": "https://stackoverflow.com",
		"google keep":   "https://keep.google.com",
		"gmail":         "https://mail.google.com",
		"netflix":       "https://www.netflix.com",
		"phpmyadmin":    "http://localhost/phpmyadmin",
	}

	for keyword, siteURL := range sites {
		if strings.Contains(lowerTitle, keyword) {
			url = siteURL
			title = pageTitle
			return url, title
		}
	}

	// Default: no URL, just return clean title
	return "", pageTitle
}

// extractChromeInfo extracts information from Chrome window title
func (bm *BrowserManager) extractChromeInfo(windowTitle string) (url, title string) {
	suffix := " - Google Chrome"
	if strings.HasSuffix(windowTitle, suffix) {
		pageTitle := strings.TrimSuffix(windowTitle, suffix)
		return bm.parseChromeTitle(pageTitle)
	}
	return "", windowTitle
}

// parseChromeTitle handles various Chrome window title formats
func (bm *BrowserManager) parseChromeTitle(pageTitle string) (url, title string) {
	// Handle localhost URLs
	if strings.Contains(pageTitle, "localhost") || strings.Contains(pageTitle, "127.0.0.1") {
		parts := strings.Split(pageTitle, " | ")
		if len(parts) >= 2 {
			urlPart := strings.TrimSpace(parts[0])
			title = strings.TrimSpace(parts[1])

			if strings.Contains(urlPart, "/") {
				urlParts := strings.Split(urlPart, " / ")
				if len(urlParts) >= 3 {
					url = "http://localhost/" + strings.Join(urlParts[2:], "/")
				} else if len(urlParts) == 2 {
					url = "http://localhost"
				}
			} else {
				url = "http://localhost"
			}
		} else {
			title = pageTitle
			url = "http://localhost"
		}
		return url, title
	}

	// Handle URLs with " - " separator
	if strings.Contains(pageTitle, " - ") {
		parts := strings.Split(pageTitle, " - ")
		if len(parts) >= 2 {
			title = strings.TrimSpace(parts[0])

			lastPart := strings.TrimSpace(parts[len(parts)-1])
			if strings.Contains(lastPart, ".") && !strings.Contains(lastPart, " ") && len(lastPart) < 50 {
				url = "https://" + lastPart
			}
		} else {
			title = pageTitle
		}
	} else {
		title = pageTitle
	}

	// Enhanced special handling for common sites
	lowerTitle := strings.ToLower(pageTitle)

	if strings.Contains(lowerTitle, "youtube") {
		url = "https://www.youtube.com"
		if strings.Contains(lowerTitle, "•") {
			parts := strings.Split(pageTitle, "•")
			if len(parts) > 0 {
				title = strings.TrimSpace(parts[0])
			}
		}
	} else if strings.Contains(lowerTitle, "google") {
		if strings.Contains(lowerTitle, "search") {
			url = "https://www.google.com"
		} else if strings.Contains(lowerTitle, "mail") {
			url = "https://mail.google.com"
		} else if strings.Contains(lowerTitle, "keep") {
			url = "https://keep.google.com"
		} else {
			url = "https://www.google.com"
		}
	} else if strings.Contains(lowerTitle, "github") {
		url = "https://github.com"
	} else if strings.Contains(lowerTitle, "stackoverflow") {
		url = "https://stackoverflow.com"
	} else if strings.Contains(lowerTitle, "phpmyadmin") {
		if url == "" {
			url = "http://localhost/phpmyadmin"
		}
	} else if strings.Contains(lowerTitle, "chatgpt") {
		url = "https://chat.openai.com"
	} else if strings.Contains(lowerTitle, "netflix") {
		url = "https://www.netflix.com"
	}

	// Try to detect from window title patterns
	if url == "" {
		domainPatterns := []string{
			".com", ".org", ".net", ".edu", ".gov", ".co.uk", ".de", ".fr",
		}

		for _, pattern := range domainPatterns {
			if strings.Contains(lowerTitle, pattern) {
				words := strings.Fields(pageTitle)
				for _, word := range words {
					if strings.Contains(strings.ToLower(word), pattern) && !strings.Contains(word, " ") {
						url = "https://" + strings.Trim(word, ".,!?;:")
						break
					}
				}
				if url != "" {
					break
				}
			}
		}
	}

	return url, title
}

// extractFirefoxInfo extracts information from Firefox window title
func (bm *BrowserManager) extractFirefoxInfo(windowTitle string) (url, title string) {
	suffix := " — Mozilla Firefox"
	if strings.HasSuffix(windowTitle, suffix) {
		pageTitle := strings.TrimSuffix(windowTitle, suffix)
		return bm.parsePageTitle(pageTitle)
	}
	return "", windowTitle
}

// extractEdgeInfo extracts information from Edge window title
func (bm *BrowserManager) extractEdgeInfo(windowTitle string) (url, title string) {
	suffix := " - Microsoft Edge"
	if strings.HasSuffix(windowTitle, suffix) {
		pageTitle := strings.TrimSuffix(windowTitle, suffix)
		return bm.parsePageTitle(pageTitle)
	}
	return "", windowTitle
}

// extractOperaInfo extracts information from Opera window title
func (bm *BrowserManager) extractOperaInfo(windowTitle string) (url, title string) {
	suffix := " - Opera"
	if strings.HasSuffix(windowTitle, suffix) {
		pageTitle := strings.TrimSuffix(windowTitle, suffix)
		return bm.parsePageTitle(pageTitle)
	}
	return "", windowTitle
}

// extractSafariInfo extracts information from Safari window title
func (bm *BrowserManager) extractSafariInfo(windowTitle string) (url, title string) {
	suffix := " — Safari"
	if strings.HasSuffix(windowTitle, suffix) {
		pageTitle := strings.TrimSuffix(windowTitle, suffix)
		return bm.parsePageTitle(pageTitle)
	}
	return "", windowTitle
}

// extractGenericInfo extracts information from generic browser
func (bm *BrowserManager) extractGenericInfo(windowTitle string) (url, title string) {
	return bm.parsePageTitle(windowTitle)
}

// parsePageTitle parses a page title to extract URL and clean title
func (bm *BrowserManager) parsePageTitle(pageTitle string) (url, title string) {
	title = pageTitle

	urlIndicators := map[string]string{
		"youtube":       "https://www.youtube.com",
		"google":        "https://www.google.com",
		"github":        "https://github.com",
		"stackoverflow": "https://stackoverflow.com",
		"reddit":        "https://www.reddit.com",
		"twitter":       "https://twitter.com",
		"facebook":      "https://www.facebook.com",
		"linkedin":      "https://www.linkedin.com",
		"microsoft":     "https://www.microsoft.com",
		"amazon":        "https://www.amazon.com",
	}

	lowerTitle := strings.ToLower(pageTitle)
	for keyword, baseURL := range urlIndicators {
		if strings.Contains(lowerTitle, keyword) {
			url = baseURL
			break
		}
	}

	cleanPatterns := []string{
		" - YouTube",
		" - Google Search",
		" - Stack Overflow",
		" - Reddit",
		" - Twitter",
		" - Facebook",
		" - LinkedIn",
	}

	for _, pattern := range cleanPatterns {
		if strings.HasSuffix(title, pattern) {
			title = strings.TrimSuffix(title, pattern)
			break
		}
	}

	return url, title
}

// IsProductiveSite determines if a URL/title indicates productive activity
func (bm *BrowserManager) IsProductiveSite(url, title string) bool {
	if url == "" && title == "" {
		return true
	}

	nonProductiveSites := []string{
		"youtube.com", "facebook.com", "twitter.com", "instagram.com",
		"reddit.com", "tiktok.com", "netflix.com", "twitch.tv",
		"spotify.com", "gaming", "game",
	}

	lowerURL := strings.ToLower(url)
	lowerTitle := strings.ToLower(title)

	for _, site := range nonProductiveSites {
		if strings.Contains(lowerURL, site) || strings.Contains(lowerTitle, site) {
			return false
		}
	}

	productiveSites := []string{
		"stackoverflow.com", "github.com", "docs.microsoft.com",
		"developer.mozilla.org", "w3schools.com", "coursera.org",
		"udemy.com", "linkedin.com/learning", "pluralsight.com",
	}

	for _, site := range productiveSites {
		if strings.Contains(lowerURL, site) || strings.Contains(lowerTitle, site) {
			return true
		}
	}

	return true
}

// fallbackGetBrowserInfo is the fallback URL extraction using window title parsing
func (bm *BrowserManager) fallbackGetBrowserInfo(processName, windowTitle string) (url, title string) {
	processName = strings.ToLower(processName)

	// Try different browser patterns
	if strings.Contains(processName, "chrome") || strings.Contains(processName, "chromium") {
		return bm.extractChromeInfoSimple(windowTitle)
	}
	if strings.Contains(processName, "firefox") {
		return bm.extractFirefoxInfo(windowTitle)
	}
	if strings.Contains(processName, "edge") || strings.Contains(processName, "msedge") {
		return bm.extractEdgeInfo(windowTitle)
	}
	if strings.Contains(processName, "opera") {
		return bm.extractOperaInfo(windowTitle)
	}
	if strings.Contains(processName, "safari") {
		return bm.extractSafariInfo(windowTitle)
	}

	return bm.extractGenericInfo(windowTitle)
}
