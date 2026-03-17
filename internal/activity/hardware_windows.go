//go:build windows
// +build windows

package activity

import (
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows/registry"
)

// WASAPI COM GUIDs
var (
	CLSID_MMDeviceEnumerator   = ole.NewGUID("{BCDE0395-E876-11D9-A292-0080C74C7E97}")
	IID_IMMDeviceEnumerator    = ole.NewGUID("{A95664D2-9614-4F35-A746-DE8DB63617E6}")
	IID_IAudioMeterInformation = ole.NewGUID("{C02216F6-8C67-4B5B-9D00-D008E73E0064}")
)

// WASAPI constants
const (
	eRender    = 0
	eConsole   = 0
	clsctxAll  = 0x17
)

// IMMDeviceEnumerator vtable indices
const (
	mmde_GetDefaultAudioEndpoint = 4
)

// IMMDevice vtable indices
const (
	mmd_Activate = 3
)

// IAudioMeterInformation vtable indices
const (
	ami_GetPeakValue = 3
)

// HardwareDetector checks whether camera, microphone, or audio output
// are currently active. Used to confirm that a meeting app is in an
// active call (not just sitting open).
type HardwareDetector struct{}

// NewHardwareDetector creates a new HardwareDetector.
func NewHardwareDetector() *HardwareDetector {
	return &HardwareDetector{}
}

// IsMediaDeviceActive returns true if camera, microphone, or audio output is active.
func (hd *HardwareDetector) IsMediaDeviceActive() bool {
	if hd.IsCameraInUse() {
		logrus.Debug("Hardware: camera is in use")
		return true
	}
	if hd.IsMicrophoneInUse() {
		logrus.Debug("Hardware: microphone is in use")
		return true
	}
	if hd.IsAudioPlaying() {
		logrus.Debug("Hardware: audio is playing on speakers")
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// Camera / Microphone detection via Windows CapabilityAccessManager registry
// ---------------------------------------------------------------------------

// IsCameraInUse checks if any application currently has the camera open.
func (hd *HardwareDetector) IsCameraInUse() bool {
	return hd.isDeviceInUse("webcam")
}

// IsMicrophoneInUse checks if any application currently has the microphone open.
func (hd *HardwareDetector) IsMicrophoneInUse() bool {
	return hd.isDeviceInUse("microphone")
}

// isDeviceInUse checks the CapabilityAccessManager registry keys to determine
// whether a privacy-sensitive device (webcam or microphone) is currently in use.
//
// Windows tracks usage per-app with LastUsedTimeStart / LastUsedTimeStop
// FILETIME values. If Start > Stop (or Stop is missing/zero), the device
// is currently open.
func (hd *HardwareDetector) isDeviceInUse(deviceType string) bool {
	basePath := `SOFTWARE\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\` + deviceType

	// Desktop apps (Zoom, Chrome, etc.)
	if hd.checkDeviceSubKeys(basePath + `\NonPackaged`) {
		return true
	}
	// UWP / packaged apps (Teams from MS Store, etc.)
	return hd.checkDeviceSubKeys(basePath)
}

func (hd *HardwareDetector) checkDeviceSubKeys(path string) bool {
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.READ)
	if err != nil {
		return false
	}
	defer key.Close()

	names, err := key.ReadSubKeyNames(-1)
	if err != nil {
		return false
	}

	for _, name := range names {
		if name == "NonPackaged" {
			continue
		}
		sk, err := registry.OpenKey(key, name, registry.READ)
		if err != nil {
			continue
		}
		start, _, errStart := sk.GetIntegerValue("LastUsedTimeStart")
		stop, _, errStop := sk.GetIntegerValue("LastUsedTimeStop")
		sk.Close()

		if errStart != nil || start == 0 {
			continue
		}
		// Device is currently in use if it was started and either:
		//   - never stopped (stop missing or zero)
		//   - started after the last stop
		if errStop != nil || stop == 0 || start > stop {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Audio output detection via WASAPI IAudioMeterInformation
// ---------------------------------------------------------------------------

// IsAudioPlaying checks if audio is currently being output to the default
// render device (speakers/headphones) using the WASAPI peak meter.
func (hd *HardwareDetector) IsAudioPlaying() bool {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Initialize COM — handle already-initialized cases gracefully
	mustUninit := false
	comErr := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	if comErr == nil {
		mustUninit = true
	} else if oleErr, ok := comErr.(*ole.OleError); ok {
		switch uint32(oleErr.Code()) {
		case 1: // S_FALSE — already initialized with compatible threading model
			mustUninit = true
		case 0x80010106: // RPC_E_CHANGED_MODE — different threading model, COM still usable
			// don't uninitialize
		default:
			logrus.Debugf("COM init failed for audio detection: %v", comErr)
			return false
		}
	} else {
		return false
	}
	if mustUninit {
		defer ole.CoUninitialize()
	}

	// Create MMDeviceEnumerator
	enumerator, err := ole.CreateInstance(CLSID_MMDeviceEnumerator, IID_IMMDeviceEnumerator)
	if err != nil {
		logrus.Debugf("Failed to create MMDeviceEnumerator: %v", err)
		return false
	}
	defer enumerator.Release()

	// GetDefaultAudioEndpoint(eRender, eConsole) → default speakers
	enumVt := (*[8]uintptr)(unsafe.Pointer(enumerator.RawVTable))
	var device *ole.IUnknown
	ret, _, _ := syscall.SyscallN(
		enumVt[mmde_GetDefaultAudioEndpoint],
		uintptr(unsafe.Pointer(enumerator)),
		eRender,
		eConsole,
		uintptr(unsafe.Pointer(&device)),
	)
	if ret != 0 || device == nil {
		return false
	}
	defer device.Release()

	// Activate IAudioMeterInformation on the device
	deviceVt := (*[7]uintptr)(unsafe.Pointer(device.RawVTable))
	var meter *ole.IUnknown
	ret, _, _ = syscall.SyscallN(
		deviceVt[mmd_Activate],
		uintptr(unsafe.Pointer(device)),
		uintptr(unsafe.Pointer(IID_IAudioMeterInformation)),
		clsctxAll,
		0,
		uintptr(unsafe.Pointer(&meter)),
	)
	if ret != 0 || meter == nil {
		return false
	}
	defer meter.Release()

	// GetPeakValue — returns 0.0 (silent) to 1.0 (max)
	meterVt := (*[7]uintptr)(unsafe.Pointer(meter.RawVTable))
	var peak float32
	ret, _, _ = syscall.SyscallN(
		meterVt[ami_GetPeakValue],
		uintptr(unsafe.Pointer(meter)),
		uintptr(unsafe.Pointer(&peak)),
	)
	if ret != 0 {
		return false
	}

	// Use a small threshold to ignore ambient electrical noise
	return peak > 0.01
}
