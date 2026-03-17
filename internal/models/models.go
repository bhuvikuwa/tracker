package models

import (
	"time"
)

// Activity represents a user activity session
type Activity struct {
	ID              int64     `json:"id" db:"id"`
	UserID          int       `json:"user_id" db:"user_id"`
	AppName         string    `json:"app_name" db:"app_name"`
	WindowTitle     string    `json:"window_title" db:"window_title"`
	BrowserURL      *string   `json:"browser_url" db:"browser_url"`
	BrowserTitle    *string   `json:"browser_title" db:"browser_title"`
	StartTime       time.Time `json:"start_time" db:"start_time"`
	EndTime         time.Time `json:"end_time" db:"end_time"`
	DurationSeconds int       `json:"duration_seconds" db:"duration_seconds"`
	IsActive        bool      `json:"is_active" db:"is_active"`
	MouseClicks     int       `json:"mouse_clicks" db:"mouse_clicks"`
	Keystrokes      int       `json:"keystrokes" db:"keystrokes"`
	MouseDistance   int       `json:"mouse_distance" db:"mouse_distance"`
	IsMeeting       bool      `json:"is_meeting" db:"is_meeting"`
	AppIcon         string    `json:"app_icon" db:"app_icon"` // Base64-encoded PNG icon
	CreatedAt       time.Time `json:"created_at" db:"created_at"`
}

// Screenshot represents a captured screenshot
type Screenshot struct {
	ID         int64     `json:"id" db:"id"`
	UserID     int       `json:"user_id" db:"user_id"`
	Filename   string    `json:"filename" db:"filename"`
	FilePath   string    `json:"file_path" db:"file_path"`
	FileSize   int       `json:"file_size" db:"file_size"`
	CapturedAt time.Time `json:"captured_at" db:"captured_at"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

// WindowInfo represents information about a window
type WindowInfo struct {
	Handle         uintptr
	ProcessID      uint32
	ProcessName    string
	ExecutablePath string
	WindowTitle    string
	ClassName      string
	IsVisible      bool
	Rectangle      Rectangle
	BrowserURL     string
	BrowserTitle   string
	AppIcon        string // Base64-encoded PNG icon
}

// Rectangle represents window coordinates
type Rectangle struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// MouseInfo represents mouse state (simplified - only for detecting activity)
type MouseInfo struct {
	X             int32
	Y             int32
	LastX         int32
	LastY         int32
	ClickCount    int
	DistanceMoved int
}

// KeyboardInfo represents keyboard state (simplified - only for detecting activity)
type KeyboardInfo struct {
	KeystrokeCount int
	LastActivity   time.Time
}

