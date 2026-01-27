//go:build windows
// +build windows

package activity

import (
	"strings"
	"sync"
	"syscall"
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
	mu          sync.Mutex
	initialized bool
	automation  *ole.IUnknown
}

// NewURLExtractorUIA creates a new UI Automation based URL extractor
func NewURLExtractorUIA() *URLExtractorUIA {
	return &URLExtractorUIA{}
}

// Initialize initializes COM and UI Automation
func (u *URLExtractorUIA) Initialize() error {
	u.mu.Lock()
	defer u.mu.Unlock()

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
	u.mu.Lock()
	defer u.mu.Unlock()

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
func (u *URLExtractorUIA) GetBrowserURL(hwnd win.HWND, processName string) string {
	u.mu.Lock()
	defer u.mu.Unlock()

	if !u.initialized || u.automation == nil {
		return ""
	}

	processName = strings.ToLower(processName)

	// Get element from window handle
	element := u.elementFromHandle(hwnd)
	if element == nil {
		return ""
	}
	defer element.Release()

	// Extract URL based on browser type
	var url string
	switch processName {
	case "chrome.exe", "brave.exe", "vivaldi.exe", "msedge.exe":
		url = u.getChromeBasedURLWithProcess(element, processName)
	case "firefox.exe":
		url = u.getFirefoxURL(element)
	case "opera.exe":
		url = u.getOperaURL(element)
	default:
		url = u.getGenericURL(element)
	}

	return url
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

// getChromeBasedURL extracts URL from Chrome-based browsers (Chrome, Edge, Brave, Vivaldi)
func (u *URLExtractorUIA) getChromeBasedURL(element *ole.IUnknown) string {
	return u.getChromeBasedURLWithProcess(element, "chrome.exe")
}

// getChromeBasedURLWithProcess extracts URL from Chrome-based browsers with process name
func (u *URLExtractorUIA) getChromeBasedURLWithProcess(element *ole.IUnknown, processName string) string {
	// Direct address bar lookup only - no slow fallback
	addressBar := u.findAddressBarDirect(element, processName)
	if addressBar == nil {
		return ""
	}
	defer addressBar.Release()

	url := u.getPropertyString(addressBar, UIA_ValueValuePropertyId)
	if url == "" {
		url = u.getPropertyString(addressBar, UIA_LegacyAccessibleValuePropertyId)
	}

	if url != "" && (strings.HasPrefix(url, "http") || strings.Contains(url, ".")) {
		return normalizeURL(url)
	}

	return ""
}

// getFirefoxURL extracts URL from Firefox
func (u *URLExtractorUIA) getFirefoxURL(element *ole.IUnknown) string {
	// Direct address bar lookup only - no slow fallback
	addressBar := u.findAddressBarDirect(element, "firefox.exe")
	if addressBar == nil {
		return ""
	}
	defer addressBar.Release()

	url := u.getPropertyString(addressBar, UIA_ValueValuePropertyId)
	if url == "" {
		url = u.getPropertyString(addressBar, UIA_LegacyAccessibleValuePropertyId)
	}

	if url != "" && (strings.HasPrefix(url, "http") || strings.Contains(url, ".")) {
		return normalizeURL(url)
	}

	return ""
}

// getOperaURL extracts URL from Opera browser
func (u *URLExtractorUIA) getOperaURL(element *ole.IUnknown) string {
	// Opera is Chromium-based, similar to Chrome
	return u.getChromeBasedURLWithProcess(element, "opera.exe")
}

// getGenericURL tries to find URL in any browser
// Returns empty - let title parsing handle unknown browsers to avoid slow element iteration
func (u *URLExtractorUIA) getGenericURL(element *ole.IUnknown) string {
	return ""
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

// findAll finds all elements matching the condition
func (u *URLExtractorUIA) findAll(element *ole.IUnknown, scope int, condition *ole.IUnknown) *ole.IUnknown {
	if element == nil || condition == nil {
		return nil
	}

	vt := (*[100]uintptr)(unsafe.Pointer(element.RawVTable))

	var result *ole.IUnknown
	ret, _, _ := syscall.SyscallN(
		vt[elem_FindAll],
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

// getElementArrayLength gets the number of elements in an element array
func (u *URLExtractorUIA) getElementArrayLength(array *ole.IUnknown) int {
	if array == nil {
		return 0
	}

	vt := (*[10]uintptr)(unsafe.Pointer(array.RawVTable))

	var length int32
	// IUIAutomationElementArray::get_Length is at index 3
	ret, _, _ := syscall.SyscallN(
		vt[3],
		uintptr(unsafe.Pointer(array)),
		uintptr(unsafe.Pointer(&length)),
	)

	if ret != 0 {
		return 0
	}

	return int(length)
}

// getElementAtIndex gets an element at the specified index
func (u *URLExtractorUIA) getElementAtIndex(array *ole.IUnknown, index int) *ole.IUnknown {
	if array == nil {
		return nil
	}

	vt := (*[10]uintptr)(unsafe.Pointer(array.RawVTable))

	var element *ole.IUnknown
	// IUIAutomationElementArray::GetElement is at index 4
	ret, _, _ := syscall.SyscallN(
		vt[4],
		uintptr(unsafe.Pointer(array)),
		uintptr(index),
		uintptr(unsafe.Pointer(&element)),
	)

	if ret != 0 {
		return nil
	}

	return element
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
