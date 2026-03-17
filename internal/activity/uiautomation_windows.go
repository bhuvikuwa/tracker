//go:build windows
// +build windows

package activity

import (
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/lxn/win"
)

// UIAutomation COM interface IDs
var (
	CLSID_CUIAutomation = ole.NewGUID("{FF48DBA4-60EF-4201-AA87-54103EEF594E}")
	IID_IUIAutomation   = ole.NewGUID("{30CBE57D-D9D0-452A-AB13-7AC5AC4825EE}")
)

// TreeScope constants
const (
	TreeScope_Element     = 0x1
	TreeScope_Children    = 0x2
	TreeScope_Descendants = 0x4
	TreeScope_Subtree     = 0x7
)

// Property IDs
const (
	UIA_NamePropertyId         = 30005
	UIA_ControlTypePropertyId  = 30003
	UIA_AutomationIdPropertyId = 30011
	UIA_ClassNamePropertyId    = 30012
	UIA_ValueValuePropertyId   = 30045
	UIA_LegacyAccessibleValuePropertyId = 30091
)

// Control Type IDs
const (
	UIA_EditControlTypeId    = 50004
	UIA_PaneControlTypeId    = 50033
	UIA_ToolBarControlTypeId = 50021
)

// IUIAutomation interface vtable indices
const (
	QueryInterface = 0
	AddRef         = 1
	Release        = 2
	// IUIAutomation methods
	uia_CompareElements                      = 3
	uia_CompareRuntimeIds                    = 4
	uia_GetRootElement                       = 5
	uia_ElementFromHandle                    = 6
	uia_ElementFromPoint                     = 7
	uia_GetFocusedElement                    = 8
	uia_GetRootElementBuildCache             = 9
	uia_ElementFromHandleBuildCache          = 10
	uia_ElementFromPointBuildCache           = 11
	uia_GetFocusedElementBuildCache          = 12
	uia_CreateTreeWalker                     = 13
	uia_ControlViewWalker                    = 14
	uia_ContentViewWalker                    = 15
	uia_RawViewWalker                        = 16
	uia_RawViewCondition                     = 17
	uia_ControlViewCondition                 = 18
	uia_ContentViewCondition                 = 19
	uia_CreateCacheRequest                   = 20
	uia_CreateTrueCondition                  = 21
	uia_CreateFalseCondition                 = 22
	uia_CreatePropertyCondition              = 23
	uia_CreatePropertyConditionEx            = 24
	uia_CreateAndCondition                   = 25
	uia_CreateAndConditionFromArray          = 26
	uia_CreateAndConditionFromNativeArray    = 27
	uia_CreateOrCondition                    = 28
	uia_CreateOrConditionFromArray           = 29
	uia_CreateOrConditionFromNativeArray     = 30
	uia_CreateNotCondition                   = 31
)

// IUIAutomationElement interface vtable indices
const (
	elem_SetFocus                         = 3
	elem_GetRuntimeId                     = 4
	elem_FindFirst                        = 5
	elem_FindAll                          = 6
	elem_FindFirstBuildCache              = 7
	elem_FindAllBuildCache                = 8
	elem_BuildUpdatedCache                = 9
	elem_GetCurrentPropertyValue          = 10
	elem_GetCurrentPropertyValueEx        = 11
	elem_GetCachedPropertyValue           = 12
	elem_GetCachedPropertyValueEx         = 13
	elem_GetCurrentPatternAs              = 14
	elem_GetCachedPatternAs               = 15
	elem_GetCurrentPattern                = 16
	elem_GetCachedPattern                 = 17
	elem_GetCachedParent                  = 18
	elem_GetCachedChildren                = 19
	elem_CurrentProcessId                 = 20
	elem_CurrentControlType               = 21
	elem_CurrentLocalizedControlType      = 22
	elem_CurrentName                      = 23
	elem_CurrentAcceleratorKey            = 24
	elem_CurrentAccessKey                 = 25
	elem_CurrentHasKeyboardFocus          = 26
	elem_CurrentIsKeyboardFocusable       = 27
	elem_CurrentIsEnabled                 = 28
	elem_CurrentAutomationId              = 29
	elem_CurrentClassName                 = 30
	elem_CurrentHelpText                  = 31
	elem_CurrentCulture                   = 32
	elem_CurrentIsControlElement          = 33
	elem_CurrentIsContentElement          = 34
	elem_CurrentIsPassword                = 35
	elem_CurrentNativeWindowHandle        = 36
	elem_CurrentItemType                  = 37
	elem_CurrentIsOffscreen               = 38
	elem_CurrentOrientation               = 39
	elem_CurrentFrameworkId               = 40
	elem_CurrentIsRequiredForForm         = 41
	elem_CurrentItemStatus                = 42
	elem_CurrentBoundingRectangle         = 43
	elem_CurrentLabeledBy                 = 44
	elem_CurrentAriaRole                  = 45
	elem_CurrentAriaProperties            = 46
	elem_CurrentIsDataValidForForm        = 47
	elem_CurrentControllerFor             = 48
	elem_CurrentDescribedBy               = 49
	elem_CurrentFlowsTo                   = 50
	elem_CurrentProviderDescription       = 51
)

// URLExtractorUIA uses Windows UI Automation to get browser URLs
type URLExtractorUIA struct {
	sem         chan struct{} // 1-buffered semaphore: prevents goroutine pileup while allowing context-aware waiting
	initialized bool
	automation  *ole.IUnknown

	// Cache address bar element to avoid tree search on every call
	cachedHwnd       win.HWND
	cachedProcess    string
	cachedAddressBar *ole.IUnknown
}

// NewURLExtractorUIA creates a new UI Automation based URL extractor
func NewURLExtractorUIA() *URLExtractorUIA {
	u := &URLExtractorUIA{
		sem: make(chan struct{}, 1),
	}
	u.sem <- struct{}{} // pre-fill: ready to acquire
	return u
}

// Initialize initializes COM and UI Automation (public method with serialization)
func (u *URLExtractorUIA) Initialize() error {
	<-u.sem // acquire semaphore (blocks until available)
	defer func() { u.sem <- struct{}{} }()
	return u.initializeInternal()
}

// initializeInternal initializes COM and UI Automation (must be called with lock held)
func (u *URLExtractorUIA) initializeInternal() error {
	if u.initialized {
		return nil
	}

	// Initialize COM
	err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	if err != nil {
		// Try apartment threaded
		ole.CoUninitialize()
		err = ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
		if err != nil {
			return err
		}
	}

	// Create UI Automation instance
	unknown, err := ole.CreateInstance(CLSID_CUIAutomation, IID_IUIAutomation)
	if err != nil {
		return err
	}

	u.automation = unknown
	u.initialized = true
	return nil
}

// Cleanup releases COM resources
func (u *URLExtractorUIA) Cleanup() {
	<-u.sem // acquire semaphore (waits for any in-flight URL extraction to finish)
	defer func() { u.sem <- struct{}{} }()

	// Clear cached address bar element
	if u.cachedAddressBar != nil {
		u.cachedAddressBar.Release()
		u.cachedAddressBar = nil
	}
	u.cachedHwnd = 0
	u.cachedProcess = ""

	if u.automation != nil {
		u.automation.Release()
		u.automation = nil
	}
	if u.initialized {
		ole.CoUninitialize()
		u.initialized = false
	}
}

// GetBrowserURL gets the URL from a browser window using UI Automation
// Uses a channel-based semaphore to:
//   - Wait for any previous slow call to finish (instead of immediately giving up)
//   - Prevent goroutine pileup (no goroutine spawned until semaphore acquired)
//   - Enforce a 500ms total deadline across wait + work
func (u *URLExtractorUIA) GetBrowserURL(hwnd win.HWND, processName string) string {
	deadline := time.After(500 * time.Millisecond)

	// Wait for semaphore: if a previous call is still running (e.g. slow COM call),
	// we wait here instead of spawning blocked goroutines. Once the previous call
	// finishes, we acquire and run a fresh query.
	select {
	case <-u.sem:
		// Acquired - previous call is done
	case <-deadline:
		// Previous call truly stuck (browser frozen) - fall back to title parsing
		return ""
	}

	// We hold the semaphore. Spawn goroutine to do COM work.
	// No pileup possible: only one goroutine runs at a time.
	resultChan := make(chan string, 1)

	go func() {
		defer func() { u.sem <- struct{}{} }() // release semaphore when done
		result := u.getBrowserURLInternal(hwnd, processName)
		select {
		case resultChan <- result:
		default:
		}
	}()

	// Wait for result with remaining time from the same 500ms deadline
	select {
	case url := <-resultChan:
		return url
	case <-deadline:
		// COM call took too long - goroutine will finish and release semaphore
		return ""
	}
}

// getBrowserURLInternal performs the actual UI Automation query
// Must be called with semaphore held (serialized access)
func (u *URLExtractorUIA) getBrowserURLInternal(hwnd win.HWND, processName string) string {
	// Lazy initialization - initialize on first use to avoid startup delay
	if !u.initialized {
		if err := u.initializeInternal(); err != nil {
			return ""
		}
	}

	if u.automation == nil {
		return ""
	}

	processName = strings.ToLower(processName)

	// Check if we have a cached address bar element for this window
	if u.cachedAddressBar != nil && u.cachedHwnd == hwnd && u.cachedProcess == processName {
		// Use cached element - just read the URL value (no tree search!)
		url := u.getPropertyString(u.cachedAddressBar, UIA_ValueValuePropertyId)
		if url == "" {
			url = u.getPropertyString(u.cachedAddressBar, UIA_LegacyAccessibleValuePropertyId)
		}
		if url != "" && (strings.HasPrefix(url, "http") || strings.Contains(url, ".")) {
			return normalizeURL(url)
		}
		// Cached element no longer valid, clear cache and search again
		u.clearCache()
	}

	// Different window or no cache - need to search for address bar
	element := u.elementFromHandle(hwnd)
	if element == nil {
		return ""
	}
	defer element.Release()

	// Find and cache the address bar element
	var addressBar *ole.IUnknown
	switch processName {
	case "chrome.exe", "brave.exe", "vivaldi.exe", "msedge.exe", "opera.exe":
		addressBar = u.findAddressBarDirect(element, processName)
	case "firefox.exe":
		addressBar = u.findAddressBarDirect(element, "firefox.exe")
	default:
		return ""
	}

	if addressBar == nil {
		return ""
	}

	// Cache the address bar element for future calls
	u.clearCache() // Clear old cache first
	u.cachedHwnd = hwnd
	u.cachedProcess = processName
	u.cachedAddressBar = addressBar // Don't release - we're caching it

	// Read URL from the newly found address bar
	url := u.getPropertyString(addressBar, UIA_ValueValuePropertyId)
	if url == "" {
		url = u.getPropertyString(addressBar, UIA_LegacyAccessibleValuePropertyId)
	}
	if url != "" && (strings.HasPrefix(url, "http") || strings.Contains(url, ".")) {
		return normalizeURL(url)
	}

	return ""
}

// clearCache releases the cached address bar element
func (u *URLExtractorUIA) clearCache() {
	if u.cachedAddressBar != nil {
		u.cachedAddressBar.Release()
		u.cachedAddressBar = nil
	}
	u.cachedHwnd = 0
	u.cachedProcess = ""
}

// elementFromHandle gets UI Automation element from window handle
func (u *URLExtractorUIA) elementFromHandle(hwnd win.HWND) *ole.IUnknown {
	if u.automation == nil {
		return nil
	}

	vt := (*[100]uintptr)(unsafe.Pointer(u.automation.RawVTable))

	var element *ole.IUnknown
	ret, _, _ := syscall.SyscallN(
		vt[uia_ElementFromHandle],
		uintptr(unsafe.Pointer(u.automation)),
		uintptr(hwnd),
		uintptr(unsafe.Pointer(&element)),
	)

	if ret != 0 || element == nil {
		return nil
	}

	return element
}

// findFirst finds the first element matching the condition
func (u *URLExtractorUIA) findFirst(element *ole.IUnknown, scope int, condition *ole.IUnknown) *ole.IUnknown {
	if element == nil || condition == nil {
		return nil
	}

	vt := (*[100]uintptr)(unsafe.Pointer(element.RawVTable))

	var result *ole.IUnknown
	ret, _, _ := syscall.SyscallN(
		vt[elem_FindFirst],
		uintptr(unsafe.Pointer(element)),
		uintptr(scope),
		uintptr(unsafe.Pointer(condition)),
		uintptr(unsafe.Pointer(&result)),
	)

	if ret != 0 {
		return nil
	}

	return result
}

// createAutomationIdCondition creates a condition for AutomationId property
func (u *URLExtractorUIA) createAutomationIdCondition(automationId string) *ole.IUnknown {
	if u.automation == nil {
		return nil
	}

	vt := (*[100]uintptr)(unsafe.Pointer(u.automation.RawVTable))

	var condition *ole.IUnknown
	var variant ole.VARIANT
	variant.VT = ole.VT_BSTR
	bstr := ole.SysAllocString(automationId)
	*(**int16)(unsafe.Pointer(&variant.Val)) = bstr

	ret, _, _ := syscall.SyscallN(
		vt[uia_CreatePropertyCondition],
		uintptr(unsafe.Pointer(u.automation)),
		uintptr(UIA_AutomationIdPropertyId),
		uintptr(unsafe.Pointer(&variant)),
		uintptr(unsafe.Pointer(&condition)),
	)

	ole.VariantClear(&variant)

	if ret != 0 {
		return nil
	}

	return condition
}

// createClassNameCondition creates a condition for ClassName property
func (u *URLExtractorUIA) createClassNameCondition(className string) *ole.IUnknown {
	if u.automation == nil {
		return nil
	}

	vt := (*[100]uintptr)(unsafe.Pointer(u.automation.RawVTable))

	var condition *ole.IUnknown
	var variant ole.VARIANT
	variant.VT = ole.VT_BSTR
	bstr := ole.SysAllocString(className)
	*(**int16)(unsafe.Pointer(&variant.Val)) = bstr

	ret, _, _ := syscall.SyscallN(
		vt[uia_CreatePropertyCondition],
		uintptr(unsafe.Pointer(u.automation)),
		uintptr(UIA_ClassNamePropertyId),
		uintptr(unsafe.Pointer(&variant)),
		uintptr(unsafe.Pointer(&condition)),
	)

	ole.VariantClear(&variant)

	if ret != 0 {
		return nil
	}

	return condition
}

// createOrCondition creates an OR condition combining two conditions
func (u *URLExtractorUIA) createOrCondition(cond1, cond2 *ole.IUnknown) *ole.IUnknown {
	if u.automation == nil || cond1 == nil || cond2 == nil {
		return nil
	}

	vt := (*[100]uintptr)(unsafe.Pointer(u.automation.RawVTable))

	var condition *ole.IUnknown
	ret, _, _ := syscall.SyscallN(
		vt[uia_CreateOrCondition],
		uintptr(unsafe.Pointer(u.automation)),
		uintptr(unsafe.Pointer(cond1)),
		uintptr(unsafe.Pointer(cond2)),
		uintptr(unsafe.Pointer(&condition)),
	)

	if ret != 0 {
		return nil
	}

	return condition
}

// createAndCondition creates an AND condition combining two conditions
func (u *URLExtractorUIA) createAndCondition(cond1, cond2 *ole.IUnknown) *ole.IUnknown {
	if u.automation == nil || cond1 == nil || cond2 == nil {
		return nil
	}

	vt := (*[100]uintptr)(unsafe.Pointer(u.automation.RawVTable))

	var condition *ole.IUnknown
	ret, _, _ := syscall.SyscallN(
		vt[uia_CreateAndCondition],
		uintptr(unsafe.Pointer(u.automation)),
		uintptr(unsafe.Pointer(cond1)),
		uintptr(unsafe.Pointer(cond2)),
		uintptr(unsafe.Pointer(&condition)),
	)

	if ret != 0 {
		return nil
	}

	return condition
}

// findAddressBarDirect finds the browser address bar directly using specific conditions
func (u *URLExtractorUIA) findAddressBarDirect(element *ole.IUnknown, processName string) *ole.IUnknown {
	if element == nil {
		return nil
	}

	processName = strings.ToLower(processName)

	// Create control type condition for Edit controls
	editCondition := u.createControlTypeCondition(UIA_EditControlTypeId)
	if editCondition == nil {
		return nil
	}
	defer editCondition.Release()

	var addressBarCondition *ole.IUnknown

	switch processName {
	case "chrome.exe", "brave.exe", "vivaldi.exe", "msedge.exe", "opera.exe":
		// Chromium-based browsers: AutomationId="addressEditBox" OR ClassName="OmniboxViewViews"
		automationIdCond := u.createAutomationIdCondition("addressEditBox")
		classNameCond := u.createClassNameCondition("OmniboxViewViews")

		if automationIdCond != nil && classNameCond != nil {
			addressBarCondition = u.createOrCondition(automationIdCond, classNameCond)
			automationIdCond.Release()
			classNameCond.Release()
		} else if automationIdCond != nil {
			addressBarCondition = automationIdCond
		} else if classNameCond != nil {
			addressBarCondition = classNameCond
		}

	case "firefox.exe":
		// Firefox: AutomationId contains "urlbar" - we'll try exact match first
		addressBarCondition = u.createAutomationIdCondition("urlbar-input")
		if addressBarCondition == nil {
			// Try alternative
			addressBarCondition = u.createAutomationIdCondition("urlbar")
		}

	default:
		// For unknown browsers, fall back to generic approach
		return nil
	}

	if addressBarCondition == nil {
		return nil
	}
	defer addressBarCondition.Release()

	// Combine with Edit control type condition
	finalCondition := u.createAndCondition(editCondition, addressBarCondition)
	if finalCondition == nil {
		return nil
	}
	defer finalCondition.Release()

	// Find first matching element
	return u.findFirst(element, TreeScope_Descendants, finalCondition)
}

// createControlTypeCondition creates a condition for control type
func (u *URLExtractorUIA) createControlTypeCondition(controlType int) *ole.IUnknown {
	if u.automation == nil {
		return nil
	}

	vt := (*[100]uintptr)(unsafe.Pointer(u.automation.RawVTable))

	var condition *ole.IUnknown
	var variant ole.VARIANT
	variant.VT = ole.VT_I4
	*(*int32)(unsafe.Pointer(&variant.Val)) = int32(controlType)

	ret, _, _ := syscall.SyscallN(
		vt[uia_CreatePropertyCondition],
		uintptr(unsafe.Pointer(u.automation)),
		uintptr(UIA_ControlTypePropertyId),
		uintptr(unsafe.Pointer(&variant)),
		uintptr(unsafe.Pointer(&condition)),
	)

	if ret != 0 {
		return nil
	}

	return condition
}

// getPropertyString gets a string property from an element
func (u *URLExtractorUIA) getPropertyString(element *ole.IUnknown, propertyId int) string {
	if element == nil {
		return ""
	}

	vt := (*[100]uintptr)(unsafe.Pointer(element.RawVTable))

	var variant ole.VARIANT
	ret, _, _ := syscall.SyscallN(
		vt[elem_GetCurrentPropertyValue],
		uintptr(unsafe.Pointer(element)),
		uintptr(propertyId),
		uintptr(unsafe.Pointer(&variant)),
	)

	if ret != 0 {
		return ""
	}

	defer ole.VariantClear(&variant)

	if variant.VT == ole.VT_BSTR {
		return ole.BstrToString(*(**uint16)(unsafe.Pointer(&variant.Val)))
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
