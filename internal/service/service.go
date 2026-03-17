package service

import (
	"context"
	"fmt"
	"os"

	"ktracker/internal/config"
	"ktracker/internal/storage"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
	"github.com/sirupsen/logrus"
)

// windowsService implements the Windows service interface
type windowsService struct {
	tracker *Tracker
}

// Execute is called when the service is started
func (s *windowsService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.StartPending}
	
	// Start the tracker
	go func() {
		if err := s.tracker.Start(); err != nil {
			logrus.Errorf("Tracker error: %v", err)
		}
	}()
	
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				logrus.Info("Service stop/shutdown requested")
				changes <- svc.Status{State: svc.StopPending}
				s.tracker.Stop()
				changes <- svc.Status{State: svc.Stopped}
				return
			default:
				logrus.Errorf("Unexpected control request #%d", c)
			}
		}
	}
}

// IsWindowsService checks if running as a Windows service
func IsWindowsService() bool {
	inService, err := svc.IsWindowsService()
	if err != nil {
		logrus.Errorf("Failed to check if running as service: %v", err)
		return false
	}
	return inService
}

// RunAsService runs the application as a Windows service
func RunAsService(ctx context.Context, cfg *config.Config, db *storage.Database) error {
	tracker, err := NewTracker(ctx, cfg, db)
	if err != nil {
		return fmt.Errorf("failed to create tracker: %w", err)
	}
	
	service := &windowsService{
		tracker: tracker,
	}
	
	logrus.Info("Starting Windows service")
	return svc.Run("KTracker", service)
}

// InstallService installs the service
func InstallService(name, displayName, description string) error {
	exePath, err := getExecutablePath()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()
	
	s, err := m.OpenService(name)
	if err == nil {
		s.Close()
		return fmt.Errorf("service %s already exists", name)
	}
	
	s, err = m.CreateService(name, exePath, mgr.Config{
		DisplayName:      displayName,
		Description:      description,
		StartType:        mgr.StartAutomatic,
		ServiceStartName: "",
	})
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}
	defer s.Close()
	
	logrus.Infof("Service %s installed successfully", name)
	return nil
}

// UninstallService uninstalls the service
func UninstallService(name string) error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect to service manager: %w", err)
	}
	defer m.Disconnect()
	
	s, err := m.OpenService(name)
	if err != nil {
		return fmt.Errorf("failed to open service: %w", err)
	}
	defer s.Close()
	
	err = s.Delete()
	if err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}
	
	logrus.Infof("Service %s uninstalled successfully", name)
	return nil
}

// getExecutablePath gets the current executable path
func getExecutablePath() (string, error) {
	return os.Executable()
}