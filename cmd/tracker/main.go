package main

import (
	"context"
	"desktime-tracker/internal/config"
	"desktime-tracker/internal/service"
	"desktime-tracker/internal/storage"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

var (
	configPath = flag.String("config", "./config/config.yaml", "Path to configuration file")
	debug      = flag.Bool("debug", false, "Enable debug mode")
	version    = flag.Bool("version", false, "Show version information")
	install    = flag.Bool("install", false, "Install as Windows service")
	uninstall  = flag.Bool("uninstall", false, "Uninstall Windows service")
)

const (
	ServiceName        = "KTracker"
	ServiceDisplayName = "KTracker Activity Tracker"
	ServiceDescription = "Tracks user activity, mouse movements, and application usage"
	Version            = "2.0.0"
	MutexName          = "Global\\KTrackerSingleInstance"
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

	// Check for single instance (skip for service install/uninstall operations)
	if !*install && !*uninstall {
		mutexHandle, err := checkSingleInstance()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer windows.CloseHandle(mutexHandle)
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

		// Start the tracker
		go func() {
			if err := tracker.Start(); err != nil {
				logrus.Errorf("Tracker error: %v", err)
				cancel()
			}
		}()

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
	// Set log level
	if debug {
		logrus.SetLevel(logrus.DebugLevel)
	} else {
		level, err := logrus.ParseLevel(cfg.Service.LogLevel)
		if err != nil {
			level = logrus.InfoLevel
		}
		logrus.SetLevel(level)
	}

	// Set log format
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
		ForceColors:   false,
	})

	// Setup log file in AppData
	appDataDir, err := config.GetAppDataDir()
	if err != nil {
		logrus.Warnf("Failed to get AppData dir for logging: %v", err)
		return
	}

	logFile := appDataDir + "/ktracker.log"
	file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		logrus.Warnf("Failed to open log file: %v", err)
		return
	}

	logrus.SetOutput(file)
	logrus.Infof("Logging to file: %s", logFile)
}
