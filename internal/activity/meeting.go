package activity

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var meetingLogFile *os.File

func init() {
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		appData = os.Getenv("APPDATA")
	}
	if appData != "" {
		logPath := filepath.Join(appData, "KTracker", "meeting_debug.log")
		os.MkdirAll(filepath.Dir(logPath), 0755)
		var err error
		meetingLogFile, err = os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			// silently ignore — debug log is optional
			meetingLogFile = nil
		}
	}
}

func logMeeting(format string, args ...interface{}) {
	if meetingLogFile != nil {
		msg := fmt.Sprintf(format, args...)
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		meetingLogFile.WriteString(fmt.Sprintf("%s | %s\n", timestamp, msg))
	}
}

// MeetingDetector detects when the user is in an active call or meeting
// by checking hardware signals: camera in use, microphone in use, or
// audio playing through speakers. No app list is needed — if the hardware
// is active, idle detection is skipped regardless of which application
// is in the foreground.
type MeetingDetector struct {
	hardware  *HardwareDetector
	wasMeeting bool // track state transitions for notifications
}

// NewMeetingDetector creates a new MeetingDetector.
func NewMeetingDetector() *MeetingDetector {
	return &MeetingDetector{
		hardware: NewHardwareDetector(),
	}
}

// IsMeeting returns true if camera, microphone, or audio output is active.
func (md *MeetingDetector) IsMeeting(processName, windowTitle, browserURL string) bool {
	camera := md.hardware.IsCameraInUse()
	mic := md.hardware.IsMicrophoneInUse()
	audio := md.hardware.IsAudioPlaying()
	isMeeting := camera || mic || audio

	logMeeting("app=%s | camera=%v | mic=%v | audio=%v | meeting=%v",
		processName, camera, mic, audio, isMeeting)

	return isMeeting
}

// WasMeeting returns the previous meeting state (for detecting transitions).
func (md *MeetingDetector) WasMeeting() bool {
	return md.wasMeeting
}

// SetWasMeeting updates the previous meeting state.
func (md *MeetingDetector) SetWasMeeting(val bool) {
	md.wasMeeting = val
}
