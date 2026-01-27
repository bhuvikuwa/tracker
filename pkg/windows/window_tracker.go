package windows

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"syscall"
	"unsafe"

	"desktime-tracker/internal/models"

	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	psapi    = windows.NewLazySystemDLL("psapi.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")

	procGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW     = user32.NewProc("GetWindowTextLengthW")
	procGetWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
	procGetClassNameW            = user32.NewProc("GetClassNameW")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procDestroyIcon              = user32.NewProc("DestroyIcon")
	procGetIconInfo              = user32.NewProc("GetIconInfo")

	procOpenProcess          = kernel32.NewProc("OpenProcess")
	procCloseHandle          = kernel32.NewProc("CloseHandle")
	procGetModuleFileNameExW = psapi.NewProc("GetModuleFileNameExW")
	procGetModuleBaseNameW   = psapi.NewProc("GetModuleBaseNameW")

	procExtractIconExW = shell32.NewProc("ExtractIconExW")

	procGetObjectW        = gdi32.NewProc("GetObjectW")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC          = gdi32.NewProc("DeleteDC")
	procSelectObject      = gdi32.NewProc("SelectObject")
	procGetDIBits         = gdi32.NewProc("GetDIBits")
	procDeleteObject      = gdi32.NewProc("DeleteObject")
)

// WindowTracker tracks active windows
type WindowTracker struct{}

// NewWindowTracker creates a new window tracker
func NewWindowTracker() (*WindowTracker, error) {
	return &WindowTracker{}, nil
}

// utf16ToString converts UTF16 to string
func utf16ToString(s []uint16) string {
	return syscall.UTF16ToString(s)
}

// GetActiveWindow gets information about the currently active window
func (wt *WindowTracker) GetActiveWindow() (*models.WindowInfo, error) {
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd == 0 {
		return nil, fmt.Errorf("no active window")
	}

	windowInfo := &models.WindowInfo{
		Handle: uintptr(hwnd),
	}

	// Get process ID
	var processID uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&processID)))
	windowInfo.ProcessID = processID

	// Get window title
	titleLength, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if titleLength > 0 {
		titleBuf := make([]uint16, titleLength+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&titleBuf[0])), titleLength+1)
		windowInfo.WindowTitle = utf16ToString(titleBuf)
	}

	// Get class name
	classBuf := make([]uint16, 256)
	procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&classBuf[0])), 256)
	windowInfo.ClassName = utf16ToString(classBuf)

	// Check if window is visible
	visible, _, _ := procIsWindowVisible.Call(hwnd)
	windowInfo.IsVisible = visible != 0

	// Get window rectangle
	var rect win.RECT
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	windowInfo.Rectangle = models.Rectangle{
		Left:   rect.Left,
		Top:    rect.Top,
		Right:  rect.Right,
		Bottom: rect.Bottom,
	}

	// Get process information
	const processQueryInformation = 0x0400
	const processVMRead = 0x0010
	
	hProcess, _, _ := procOpenProcess.Call(
		processQueryInformation|processVMRead,
		0,
		uintptr(processID))
	
	if hProcess != 0 {
		defer procCloseHandle.Call(hProcess)

		// Get process name
		processNameBuf := make([]uint16, 260)
		procGetModuleBaseNameW.Call(
			hProcess,
			0,
			uintptr(unsafe.Pointer(&processNameBuf[0])),
			260)
		windowInfo.ProcessName = utf16ToString(processNameBuf)

		// Get executable path
		executablePathBuf := make([]uint16, 260)
		procGetModuleFileNameExW.Call(
			hProcess,
			0,
			uintptr(unsafe.Pointer(&executablePathBuf[0])),
			260)
		windowInfo.ExecutablePath = utf16ToString(executablePathBuf)

		// Extract app icon as base64 PNG
		windowInfo.AppIcon = wt.ExtractIconBase64(windowInfo.ExecutablePath)
	}

	return windowInfo, nil
}

// EnumWindows enumerates all visible windows
func (wt *WindowTracker) EnumWindows() ([]*models.WindowInfo, error) {
	var windowList []*models.WindowInfo

	enumProc := syscall.NewCallback(func(hwnd syscall.Handle, lParam uintptr) uintptr {
		visible, _, _ := procIsWindowVisible.Call(uintptr(hwnd))
		if visible == 0 {
			return 1 // Continue enumeration
		}

		// Get window title length
		titleLength, _, _ := procGetWindowTextLengthW.Call(uintptr(hwnd))
		if titleLength == 0 {
			return 1 // Skip windows without titles
		}

		windowInfo := &models.WindowInfo{
			Handle: uintptr(hwnd),
		}

		// Get window title
		titleBuf := make([]uint16, titleLength+1)
		procGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&titleBuf[0])), titleLength+1)
		windowInfo.WindowTitle = utf16ToString(titleBuf)

		// Get process ID
		var processID uint32
		procGetWindowThreadProcessId.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&processID)))
		windowInfo.ProcessID = processID

		windowList = append(windowList, windowInfo)
		return 1 // Continue enumeration
	})

	user32.NewProc("EnumWindows").Call(enumProc, 0)
	return windowList, nil
}

// ICONINFO structure for GetIconInfo
type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

// BITMAP structure for GetObject
type bitmap struct {
	BmType       int32
	BmWidth      int32
	BmHeight     int32
	BmWidthBytes int32
	BmPlanes     uint16
	BmBitsPixel  uint16
	BmBits       uintptr
}

// BITMAPINFOHEADER structure for GetDIBits
type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// ExtractIconBase64 extracts the icon from an executable and returns it as base64-encoded PNG
func (wt *WindowTracker) ExtractIconBase64(executablePath string) string {
	if executablePath == "" {
		return ""
	}

	// Convert path to UTF16
	pathPtr, err := syscall.UTF16PtrFromString(executablePath)
	if err != nil {
		return ""
	}

	// Extract large icon from executable
	var largeIcon uintptr
	ret, _, _ := procExtractIconExW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,                                  // Icon index
		uintptr(unsafe.Pointer(&largeIcon)), // Large icon
		0, // Small icon (not needed)
		1, // Number of icons to extract
	)

	if ret == 0 || largeIcon == 0 {
		return ""
	}
	defer procDestroyIcon.Call(largeIcon)

	// Get icon info to access the bitmap
	var ii iconInfo
	ret, _, _ = procGetIconInfo.Call(largeIcon, uintptr(unsafe.Pointer(&ii)))
	if ret == 0 {
		return ""
	}

	// Clean up bitmaps when done
	if ii.HbmMask != 0 {
		defer procDeleteObject.Call(ii.HbmMask)
	}
	if ii.HbmColor != 0 {
		defer procDeleteObject.Call(ii.HbmColor)
	}

	// If no color bitmap, icon might be monochrome - skip it
	if ii.HbmColor == 0 {
		return ""
	}

	// Get bitmap info
	var bm bitmap
	ret, _, _ = procGetObjectW.Call(ii.HbmColor, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm)))
	if ret == 0 {
		return ""
	}

	width := int(bm.BmWidth)
	height := int(bm.BmHeight)
	if width <= 0 || height <= 0 {
		return ""
	}

	// Create compatible DC
	hdc, _, _ := procCreateCompatibleDC.Call(0)
	if hdc == 0 {
		return ""
	}
	defer procDeleteDC.Call(hdc)

	// Select bitmap into DC
	oldBitmap, _, _ := procSelectObject.Call(hdc, ii.HbmColor)
	defer procSelectObject.Call(hdc, oldBitmap)

	// Prepare BITMAPINFOHEADER for GetDIBits
	bmi := bitmapInfoHeader{
		BiSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		BiWidth:       int32(width),
		BiHeight:      -int32(height), // Negative for top-down DIB
		BiPlanes:      1,
		BiBitCount:    32,
		BiCompression: 0, // BI_RGB
	}

	// Allocate buffer for pixel data (BGRA format)
	pixelData := make([]byte, width*height*4)

	// Get the bits
	ret, _, _ = procGetDIBits.Call(
		hdc,
		ii.HbmColor,
		0,
		uintptr(height),
		uintptr(unsafe.Pointer(&pixelData[0])),
		uintptr(unsafe.Pointer(&bmi)),
		0, // DIB_RGB_COLORS
	)
	if ret == 0 {
		return ""
	}

	// Create Go image from pixel data (convert BGRA to RGBA)
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := (y*width + x) * 4
			// BGRA to RGBA
			img.Pix[idx+0] = pixelData[idx+2] // R <- B
			img.Pix[idx+1] = pixelData[idx+1] // G <- G
			img.Pix[idx+2] = pixelData[idx+0] // B <- R
			img.Pix[idx+3] = pixelData[idx+3] // A <- A
		}
	}

	// Encode as PNG
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}

	// Return as base64
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}