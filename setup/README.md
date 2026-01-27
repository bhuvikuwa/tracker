# DeskTime Tracker Installer

This folder contains installer scripts for DeskTime Tracker.

## Installation Options

### Option 1: Professional Installer (Recommended)
Use the Inno Setup script to create a professional Windows installer.

**Steps:**
1. Download and install [Inno Setup](https://jrsoftware.org/isdl.php)
2. Build the tracker: `go build -o tracker.exe ./cmd/tracker`
3. Open `installer.iss` in Inno Setup Compiler
4. Click **Build > Compile**
5. The installer will be created in `../dist/DeskTime_Tracker_Setup.exe`
6. Distribute `DeskTime_Tracker_Setup.exe` to users

**Features:**
- Professional installation wizard
- Automatic service installation
- Start menu shortcuts
- Clean uninstallation
- Admin privilege handling

### Option 2: Simple Batch Installer
Use the batch scripts for a simpler installation.

**Steps:**
1. Build the tracker: `go build -o tracker.exe ./cmd/tracker`
2. Right-click `install.bat` and select **Run as Administrator**
3. Follow the on-screen instructions

**To Uninstall:**
- Right-click `uninstall.bat` and select **Run as Administrator**

## What Gets Installed

The installer will:
1. Copy `tracker.exe` to `C:\Program Files\DeskTime Tracker\`
2. Copy `config.yaml` to the config folder
3. Create directories for screenshots and logs
4. Install the Windows service named "KTracker"
5. Configure the service to start automatically
6. Start the service immediately

## Service Management

After installation, you can manage the service:

**Using Services Manager:**
```
1. Press Win+R
2. Type: services.msc
3. Find "KTracker Activity Tracker"
4. Right-click to Start/Stop/Restart
```

**Using Command Line (as Administrator):**
```cmd
REM Start service
sc start KTracker

REM Stop service
sc stop KTracker

REM Check service status
sc query KTracker

REM Change startup type to automatic
sc config KTracker start= auto

REM Change startup type to manual
sc config KTracker start= demand
```

## Configuration

The configuration file is located at:
```
C:\Program Files\DeskTime Tracker\config\config.yaml
```

Edit this file to customize:
- Screenshot interval
- Activity tracking settings
- Browser tracking
- Idle detection

**After changing config, restart the service:**
```cmd
sc stop KTracker
sc start KTracker
```

## Logs and Data

- **Logs:** `C:\Program Files\DeskTime Tracker\logs\`
- **Screenshots:** `C:\Program Files\DeskTime Tracker\screenshots\`
- **Config:** `C:\Program Files\DeskTime Tracker\config\`

## Troubleshooting

### Service won't start
1. Check logs in the logs folder
2. Verify config.yaml is valid
3. Ensure database API URL is correct
4. Check Windows Event Viewer for errors

### Permission Issues
- Ensure you ran the installer as Administrator
- Check folder permissions for logs and screenshots folders

### Uninstall Issues
1. Stop the service first: `sc stop KTracker`
2. Run uninstall.bat as Administrator
3. If service still exists: `sc delete KTracker`
4. Manually delete folder: `C:\Program Files\DeskTime Tracker`

## Building from Source

To build the installer yourself:

```bash
# Navigate to tracker directory
cd D:\xampp\htdocs\desktime\tracker

# Build the executable
go build -o tracker.exe ./cmd/tracker

# For Inno Setup installer:
# - Open installer.iss in Inno Setup
# - Click Compile

# For batch installer:
# - Run setup\install.bat as Administrator
```

## System Requirements

- Windows 7 or later (64-bit)
- Administrator privileges for installation
- .NET Framework (for some features)

## Support

For issues or questions, refer to the main DeskTime documentation or contact support.
