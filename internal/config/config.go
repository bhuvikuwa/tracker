package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds the application configuration
type Config struct {
	Service  ServiceConfig  `yaml:"service"`
	Tracking TrackingConfig `yaml:"tracking"`
	API      APIConfig      `yaml:"api"`
	Website  WebsiteConfig  `yaml:"website"`
	User     UserConfig     `yaml:"user"`
}

// ServiceConfig holds service-related configuration
type ServiceConfig struct {
	LogLevel string `yaml:"log_level"`
}

// TrackingConfig holds tracking-related configuration
type TrackingConfig struct {
	ScreenshotInterval int `yaml:"screenshot_interval"`
	ActivityInterval   int `yaml:"activity_interval"`
	IdleTimeout        int `yaml:"idle_timeout"`
}

// APIConfig holds API-related configuration
type APIConfig struct {
	BaseURL string `yaml:"base_url"`
}

// WebsiteConfig holds website-related configuration
type WebsiteConfig struct {
	BaseURL string `yaml:"base_url"`
	Port    int    `yaml:"port"`
}

// UserConfig holds user authentication state
type UserConfig struct {
	Username   string `yaml:"username"`
	AppCode    string `yaml:"app_code"`
	IsLoggedIn bool   `yaml:"is_logged_in"`
	Email      string `yaml:"email"`
	// Feature settings from server
	Screenshots    bool `yaml:"screenshots"`     // Whether to capture and send screenshots
	ScreenshotTime int  `yaml:"screenshot_time"` // Screenshot interval in seconds (from server)
	Logout         bool `yaml:"logout"`          // Whether to show logout menu
	ExitApp        bool `yaml:"exit_app"`        // Whether to show exit menu
	// Server URLs (fetched from app_config.php, persisted locally)
	APIURL     string `yaml:"api_url"`     // API endpoint for data collection
	WebsiteURL string `yaml:"website_url"` // Website base URL for login/dashboard
}

// Load loads configuration from file
func Load(configPath string) (*Config, error) {
	// Set defaults first
	config := &Config{}
	setDefaults(config)

	// Determine config file path
	if configPath == "" {
		configPath = "./config/config.yaml"
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file not found, use defaults
			if err := validateConfig(config); err != nil {
				return nil, fmt.Errorf("config validation failed: %w", err)
			}
			return config, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Parse YAML
	if err := yaml.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Validate and set derived values
	if err := validateConfig(config); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return config, nil
}

func setDefaults(config *Config) {
	// Service defaults
	config.Service.LogLevel = "debug"

	// Tracking defaults
	config.Tracking.ScreenshotInterval = 30 // 30 seconds
	config.Tracking.ActivityInterval = 1    // 1 second
	config.Tracking.IdleTimeout = 180       // 3 minutes

	// User defaults
	config.User.IsLoggedIn = false
}

func validateConfig(config *Config) error {
	// Validate intervals
	if config.Tracking.ActivityInterval < 1 {
		config.Tracking.ActivityInterval = 1
	}
	if config.Tracking.ScreenshotInterval < 30 {
		config.Tracking.ScreenshotInterval = 30
	}
	if config.Tracking.IdleTimeout < 30 {
		config.Tracking.IdleTimeout = 30
	}

	return nil
}

// Save saves the configuration to file (excludes user data - use SaveUserData for that)
func (c *Config) Save(configPath string) error {
	if configPath == "" {
		configPath = "./config/config.yaml"
	}

	// Create a yaml encoder with custom settings
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(4)

	if err := encoder.Encode(c); err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	encoder.Close()

	// Write to file
	if err := os.WriteFile(configPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetAppDataDir returns the KTracker AppData directory
func GetAppDataDir() (string, error) {
	// Get AppData\Local folder
	appData := os.Getenv("LOCALAPPDATA")
	if appData == "" {
		// Fallback to APPDATA if LOCALAPPDATA is not set
		appData = os.Getenv("APPDATA")
	}
	if appData == "" {
		return "", fmt.Errorf("could not find AppData folder")
	}

	// Create KTracker folder in AppData
	appDataDir := filepath.Join(appData, "KTracker")
	if err := os.MkdirAll(appDataDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create app data directory: %w", err)
	}

	return appDataDir, nil
}

// GetUserDataPath returns the path to the user data file in AppData
func GetUserDataPath() (string, error) {
	appDataDir, err := GetAppDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDataDir, "user_data.yaml"), nil
}


// SaveUserData saves user credentials to AppData folder (persists on same system, not distributed with exe)
func (c *Config) SaveUserData() error {
	userDataPath, err := GetUserDataPath()
	if err != nil {
		return err
	}

	// Only save user-related fields
	userData := UserConfig{
		Username:       c.User.Username,
		AppCode:        c.User.AppCode,
		IsLoggedIn:     c.User.IsLoggedIn,
		Email:          c.User.Email,
		Screenshots:    c.User.Screenshots,
		ScreenshotTime: c.User.ScreenshotTime,
		Logout:         c.User.Logout,
		ExitApp:        c.User.ExitApp,
		APIURL:         c.User.APIURL,
		WebsiteURL:     c.User.WebsiteURL,
	}

	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(4)

	if err := encoder.Encode(userData); err != nil {
		return fmt.Errorf("failed to marshal user data: %w", err)
	}
	encoder.Close()

	if err := os.WriteFile(userDataPath, buf.Bytes(), 0644); err != nil {
		return fmt.Errorf("failed to write user data file: %w", err)
	}

	return nil
}

// LoadUserData loads user credentials from AppData folder
func (c *Config) LoadUserData() error {
	userDataPath, err := GetUserDataPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(userDataPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No user data file exists yet, use defaults (not logged in)
			return nil
		}
		return fmt.Errorf("failed to read user data file: %w", err)
	}

	var userData UserConfig
	if err := yaml.Unmarshal(data, &userData); err != nil {
		return fmt.Errorf("failed to parse user data file: %w", err)
	}

	// Apply loaded user data to config
	c.User = userData

	// Apply persisted URLs to runtime config
	if c.User.APIURL != "" {
		c.API.BaseURL = c.User.APIURL
	}
	if c.User.WebsiteURL != "" {
		c.Website.BaseURL = c.User.WebsiteURL
	}

	return nil
}
