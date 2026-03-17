package main

import (
	"context"
	"ktracker/internal/autostart"
	"ktracker/internal/config"
	"ktracker/internal/ipc"
	"ktracker/internal/protocol"
	"ktracker/internal/service"
	"ktracker/internal/storage"
	"ktracker/internal/updater"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

var (
	configPath  = flag.String("config", "./config/config.yaml", "Path to configuration file")
	debug       = flag.Bool("debug", false, "Enable debug mode")
	version     = flag.Bool("version", false, "Show version information")
	install     = flag.Bool("install", false, "Install as Windows service")
	uninstall   = flag.Bool("uninstall", false, "Uninstall Windows service")
	mutexHandle windows.Handle // stored globally so restartApp can release it
)

const (
	ServiceName        = "KTracker"
	ServiceDisplayName = "KTracker Activity Tracker"
	ServiceDescription = "Tracks user activity, mouse movements, and application usage"
	Version            = "3.22.0"
	MutexName          = "Global\\KTrackerSingleInstance"
	UpdateConfigURL    = "https://desktime.kuware.com/app_config.php"
)

// checkSingleInstance ensures only one instance of the application is running
// Returns a handle that must be kept alive for the duration of the program
func checkSingleInstance() (windows.Handle, error) {
	mutexNamePtr, err := windows.UTF16PtrFromString(MutexName)
	if err != nil {
		return 0, fmt.Errorf("failed to create mutex name: %w", err)
	}

	handle, err := windows.CreateMutex(nil, false, mutexNamePtr)
	if err != nil {
		return 0, fmt.Errorf("failed to create mutex: %w", err)
	}

	lastErr := windows.GetLastError()
	if lastErr == windows.ERROR_ALREADY_EXISTS {
		windows.CloseHandle(handle)
		return 0, fmt.Errorf("another instance of KTracker is already running")
	}

	return handle, nil
}

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("KTracker Activity Tracker v%s\n", Version)
		os.Exit(0)
	}

	// Check if launched with a kt-tracker:// protocol URL (e.g. from browser redirect)
	var protocolLoginData *protocol.LoginData
	for _, arg := range os.Args[1:] {
		if protocol.IsProtocolURL(arg) {
			parsed, err := protocol.ParseProtocolURL(arg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to parse protocol URL: %v\n", err)
				os.Exit(1)
			}
			protocolLoginData = parsed
			break
		}
	}

	// If we have protocol login data, try to send it to the running instance via named pipe
	if protocolLoginData != nil {
		if err := ipc.SendLoginData(protocolLoginData); err == nil {
			// Successfully sent to running instance, exit this 2nd instance
			fmt.Println("Login data sent to running KTracker instance")
			os.Exit(0)
		}
		// Pipe not found means no running instance — fall through to normal startup
		// and apply login data after tracker is initialized
		logrus.Infof("No running instance found, starting fresh with protocol login data")
	}

	// Check for single instance (skip for service install/uninstall operations)
	if !*install && !*uninstall {
		var err error
		mutexHandle, err = checkSingleInstance()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer windows.CloseHandle(mutexHandle)

		// Clean up old exe from a previous update
		updater.CleanupOldExe()

		// Always register auto-start (updates exe path if app was updated to a new location)
		if err := autostart.Enable(); err != nil {
			logrus.Warnf("Failed to register auto-start: %v", err)
		}

		// Register/update kt-tracker:// protocol handler
		if err := protocol.RegisterProtocol(); err != nil {
			logrus.Warnf("Failed to register protocol handler: %v", err)
		}
	}

	// Resolve config path relative to executable if not absolute
	cfgPath := *configPath
	if !filepath.IsAbs(cfgPath) {
		exePath, err := os.Executable()
		if err == nil {
			exeDir := filepath.Dir(exePath)
			cfgPath = filepath.Join(exeDir, cfgPath)
		}
	}

	// Load configuration
	cfg, err := config.Load(cfgPath)
	if err != nil {
		logrus.Fatalf("Failed to load configuration: %v", err)
	}

	// Load user data from AppData folder (persisted login state)
	if err := cfg.LoadUserData(); err != nil {
		logrus.Warnf("Failed to load user data: %v", err)
	}

	// Setup logging
	setupLogging(cfg, *debug)

	logrus.Infof("Starting KTracker Activity Tracker v%s", Version)

	// Check for updates and fetch remote config (skip for service install/uninstall)
	var appUpdater *updater.Updater
	if !*install && !*uninstall {
		appUpdater = updater.NewUpdater(UpdateConfigURL, Version)
		if cfg.User.Email != "" {
			appUpdater.SetEmail(cfg.User.Email)
		}
		shouldExit, err := appUpdater.PerformUpdate()
		if err != nil {
			logrus.Warnf("Update check failed: %v", err)
		}
		if shouldExit {
			logrus.Info("Exiting for update...")
			os.Exit(0)
		}
	}

	// Handle service installation/uninstallation
	if *install {
		err := service.InstallService(ServiceName, ServiceDisplayName, ServiceDescription)
		if err != nil {
			logrus.Fatalf("Failed to install service: %v", err)
		}
		logrus.Info("Service installed successfully")
		return
	}

	if *uninstall {
		err := service.UninstallService(ServiceName)
		if err != nil {
			logrus.Fatalf("Failed to uninstall service: %v", err)
		}
		logrus.Info("Service uninstalled successfully")
		return
	}

	// Initialize database
	db, err := storage.NewDatabase(cfg.API)
	if err != nil {
		logrus.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()

	// Set app version for API requests
	db.SetAppVersion(Version)

	// Apply remote config (API URL, idle timeout) before creating tracker
	// Pass nil for tracker since it's not created yet
	if appUpdater != nil {
		applyRemoteConfig(appUpdater, db, cfg, nil)
	}

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Check if running as Windows service
	if service.IsWindowsService() {
		// Run as Windows service
		logrus.Info("Running as Windows service")
		err := service.RunAsService(ctx, cfg, db)
		if err != nil {
			logrus.Fatalf("Service failed: %v", err)
		}
	} else {
		// Run as console application
		logrus.Info("Running as console application")
		tracker, err := service.NewTracker(ctx, cfg, db)
		if err != nil {
			logrus.Fatalf("Failed to create tracker: %v", err)
		}

		// Set version for system tray display
		tracker.SetVersion(Version)

		// Set restart callback for system tray Restart menu
		tracker.SetRestartCallback(restartApp)

		// Start periodic config check (every 15 minutes) with tracker for runtime updates
		if appUpdater != nil {
			go periodicConfigCheck(ctx, appUpdater, db, cfg, tracker)
		}

		// Start the tracker
		go func() {
			if err := tracker.Start(); err != nil {
				logrus.Errorf("Tracker error: %v", err)
				cancel()
			}
		}()

		// If we have protocol login data from a cold start (no running instance),
		// apply it now that the tracker is initialized
		if protocolLoginData != nil {
			tracker.ApplyProtocolLogin(protocolLoginData)
		}

		// Start hourly auto-restart to clear stale state
		go startHourlyRestart(ctx)

		// Wait for shutdown signal
		select {
		case <-sigChan:
			logrus.Info("Received shutdown signal")
		case <-ctx.Done():
			logrus.Info("Context cancelled")
		}

		logrus.Info("Shutting down...")
		cancel()
		tracker.Stop()
	}

	logrus.Info("KTracker Activity Tracker stopped")
}

func setupLogging(cfg *config.Config, debug bool) {
	// Disable all logging - discard output
	logrus.SetOutput(io.Discard)
	logrus.SetLevel(logrus.PanicLevel)
}

// applyRemoteConfig fetches and applies remote configuration
// tracker can be nil for initial config (before tracker is created)
func applyRemoteConfig(appUpdater *updater.Updater, db *storage.Database, cfg *config.Config, tracker *service.Tracker) {
	remoteConfig, err := appUpdater.FetchRemoteConfig()
	if err != nil {
		logrus.Warnf("Failed to fetch remote config: %v", err)
		return
	}

	urlsChanged := false

	// Apply API URL from remote config
	if remoteConfig.APIBaseURL != "" {
		cfg.API.BaseURL = remoteConfig.APIBaseURL
		cfg.User.APIURL = remoteConfig.APIBaseURL
		db.SetAPIURL(remoteConfig.APIBaseURL)
		urlsChanged = true
	}

	// Apply Website URL from remote config
	if remoteConfig.BaseURL != "" {
		cfg.Website.BaseURL = remoteConfig.BaseURL
		cfg.User.WebsiteURL = remoteConfig.BaseURL
		urlsChanged = true
	}

	// Persist URLs to user_data.yaml so they survive restarts
	if urlsChanged {
		if err := cfg.SaveUserData(); err != nil {
			logrus.Warnf("Failed to save URLs to user data: %v", err)
		}
	}

	// Apply idle timeout from remote config
	if remoteConfig.IdleTimeout > 0 {
		cfg.Tracking.IdleTimeout = remoteConfig.IdleTimeout
		// If tracker is already running, update it directly
		if tracker != nil {
			tracker.SetIdleTimeout(remoteConfig.IdleTimeout)
		}
		logrus.Infof("Idle timeout set to %d seconds from remote config", remoteConfig.IdleTimeout)
	}

	logrus.Info("Remote config applied successfully")
}

// startHourlyRestart schedules an automatic app restart every hour
func startHourlyRestart(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			logrus.Info("Hourly restart triggered - restarting app...")
			restartApp()
		}
	}
}

// restartApp launches a new instance of the app and exits the current process
func restartApp() {
	exePath, err := os.Executable()
	if err != nil {
		logrus.Errorf("Failed to get executable path for restart: %v", err)
		return
	}

	// Release the single-instance mutex BEFORE starting the new instance,
	// otherwise the new process will see the mutex and exit immediately.
	if mutexHandle != 0 {
		windows.CloseHandle(mutexHandle)
		mutexHandle = 0
	}

	cmd := exec.Command(exePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		logrus.Errorf("Failed to start new instance: %v", err)
		return
	}
	logrus.Info("New instance started, exiting current process")
	os.Exit(0)
}

// periodicConfigCheck checks for config updates periodically
func periodicConfigCheck(ctx context.Context, appUpdater *updater.Updater, db *storage.Database, cfg *config.Config, tracker *service.Tracker) {
	ticker := time.NewTicker(15 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logrus.Info("Stopping periodic config check")
			return
		case <-ticker.C:
			logrus.Info("Periodic config check...")
			applyRemoteConfig(appUpdater, db, cfg, tracker)

			// Also check for updates
			shouldExit, err := appUpdater.PerformUpdate()
			if err != nil {
				logrus.Warnf("Periodic update check failed: %v", err)
			}
			if shouldExit {
				logrus.Info("Update available, app will restart...")
				os.Exit(0)
			}
		}
	}
}
