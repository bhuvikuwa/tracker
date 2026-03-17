package protocol

import (
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows/registry"
)

const (
	protocolName = "kt-tracker"
	registryPath = `Software\Classes\` + protocolName
)

// LoginData holds the parsed data from a kt-tracker:// protocol URL
type LoginData struct {
	Email          string
	AppCode        string
	Screenshots    int
	ScreenshotTime int
	Logout         int
	ExitApp        int
}

// RegisterProtocol registers the kt-tracker:// protocol handler in Windows Registry
func RegisterProtocol() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Create kt-tracker protocol key
	key, _, err := registry.CreateKey(registry.CURRENT_USER, registryPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to create registry key: %w", err)
	}
	defer key.Close()

	// Set default value and URL Protocol
	if err := key.SetStringValue("", "URL:KTracker Protocol"); err != nil {
		return fmt.Errorf("failed to set default value: %w", err)
	}
	if err := key.SetStringValue("URL Protocol", ""); err != nil {
		return fmt.Errorf("failed to set URL Protocol value: %w", err)
	}

	// Create shell\open\command key
	cmdPath := registryPath + `\shell\open\command`
	cmdKey, _, err := registry.CreateKey(registry.CURRENT_USER, cmdPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("failed to create command registry key: %w", err)
	}
	defer cmdKey.Close()

	// Set command to launch KTracker with the URL as argument
	command := fmt.Sprintf(`"%s" "%%1"`, exePath)
	if err := cmdKey.SetStringValue("", command); err != nil {
		return fmt.Errorf("failed to set command value: %w", err)
	}

	logrus.Infof("Registered kt-tracker:// protocol handler: %s", command)
	return nil
}

// ParseProtocolURL parses a kt-tracker://login?email=...&app_code=... URL into LoginData
func ParseProtocolURL(rawURL string) (*LoginData, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %w", err)
	}

	if u.Scheme != protocolName {
		return nil, fmt.Errorf("unexpected scheme: %s (expected %s)", u.Scheme, protocolName)
	}

	params := u.Query()

	email := params.Get("email")
	appCode := params.Get("app_code")
	if email == "" || appCode == "" {
		return nil, fmt.Errorf("missing required parameters: email and app_code")
	}

	data := &LoginData{
		Email:   email,
		AppCode: appCode,
	}

	if v := params.Get("screenshots"); v != "" {
		data.Screenshots, _ = strconv.Atoi(v)
	}
	if v := params.Get("screenshots_timing"); v != "" {
		data.ScreenshotTime, _ = strconv.Atoi(v)
	}
	if v := params.Get("logout"); v != "" {
		data.Logout, _ = strconv.Atoi(v)
	}
	if v := params.Get("exit_app"); v != "" {
		data.ExitApp, _ = strconv.Atoi(v)
	}

	return data, nil
}

// IsProtocolURL checks if a string looks like a kt-tracker:// URL
func IsProtocolURL(s string) bool {
	return len(s) > len(protocolName)+3 && s[:len(protocolName)+3] == protocolName+"://"
}
