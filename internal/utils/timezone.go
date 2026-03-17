package utils

import (
	"fmt"
	"time"
)

// GetSystemTimezone returns the current system timezone
// Format: "TZ_NAME (UTC+/-offset)" e.g., "PST (UTC-8)" or "IST (UTC+5)"
func GetSystemTimezone() string {
	tzName, tzOffset := time.Now().Zone()
	return fmt.Sprintf("%s (UTC%+d)", tzName, tzOffset/3600)
}

