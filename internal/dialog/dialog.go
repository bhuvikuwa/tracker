package dialog

import (
	"sync"

	"github.com/ncruces/zenity"
)

// Dialog state - prevent multiple dialogs
var (
	dialogOpen  bool
	dialogMutex sync.Mutex
)

// IsDialogOpen returns true if dialog is currently open
func IsDialogOpen() bool {
	dialogMutex.Lock()
	defer dialogMutex.Unlock()
	return dialogOpen
}

// ShowEmailInputDialog shows a native input dialog for email
func ShowEmailInputDialog() (string, bool) {
	dialogMutex.Lock()
	if dialogOpen {
		dialogMutex.Unlock()
		return "", false
	}
	dialogOpen = true
	dialogMutex.Unlock()

	defer func() {
		dialogMutex.Lock()
		dialogOpen = false
		dialogMutex.Unlock()
	}()

	email, err := zenity.Entry(
		"Enter your email address:",
		zenity.Title("KTracker Login"),
		zenity.Width(300),
		zenity.Modal(),
	)

	if err != nil || email == "" {
		return "", false
	}

	return email, true
}

// ShowMessage shows a simple message box
func ShowMessage(title, message string) {
	zenity.Info(message, zenity.Title(title))
}

// ShowError shows an error message box
func ShowError(title, message string) {
	zenity.Error(message, zenity.Title(title))
}

// SetProcessing is a no-op for this dialog
func SetProcessing(processing bool) {}

// CloseDialog is a no-op for this dialog
func CloseDialog() {}
