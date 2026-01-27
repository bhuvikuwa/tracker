@echo off
REM DeskTime Tracker Installer Batch Script
REM This script installs the DeskTime Tracker as a Windows service

echo ========================================
echo DeskTime Tracker Installer
echo ========================================
echo.

REM Check for administrator privileges
net session >nul 2>&1
if %errorLevel% neq 0 (
    echo ERROR: This installer requires Administrator privileges.
    echo Please right-click and select "Run as Administrator"
    echo.
    pause
    exit /b 1
)

echo [1/6] Checking installation directory...
set INSTALL_DIR=%ProgramFiles%\DeskTime Tracker
if not exist "%INSTALL_DIR%" (
    echo Creating directory: %INSTALL_DIR%
    mkdir "%INSTALL_DIR%"
    mkdir "%INSTALL_DIR%\config"
    mkdir "%INSTALL_DIR%\screenshots"
    mkdir "%INSTALL_DIR%\logs"
)

echo.
echo [2/6] Copying files...
copy /Y "..\tracker.exe" "%INSTALL_DIR%\" >nul
if %errorLevel% neq 0 (
    echo ERROR: Failed to copy tracker.exe
    pause
    exit /b 1
)

copy /Y "..\config\config.yaml" "%INSTALL_DIR%\config\" >nul
if %errorLevel% neq 0 (
    echo ERROR: Failed to copy config.yaml
    pause
    exit /b 1
)

echo Files copied successfully.

echo.
echo [3/6] Checking if service already exists...
sc query KTracker >nul 2>&1
if %errorLevel% equ 0 (
    echo Service already exists. Stopping and uninstalling...
    sc stop KTracker >nul 2>&1
    timeout /t 2 /nobreak >nul
    "%INSTALL_DIR%\tracker.exe" -uninstall >nul 2>&1
    timeout /t 1 /nobreak >nul
)

echo.
echo [4/6] Installing Windows service...
cd /d "%INSTALL_DIR%"
tracker.exe -install
if %errorLevel% neq 0 (
    echo ERROR: Failed to install service
    pause
    exit /b 1
)
echo Service installed successfully.

echo.
echo [5/6] Configuring service...
sc config KTracker start= auto
sc description KTracker "Tracks user activity, mouse/keyboard input, and application usage for DeskTime"

echo.
echo [6/6] Starting service...
sc start KTracker
if %errorLevel% neq 0 (
    echo WARNING: Service installed but failed to start
    echo You can start it manually from Services (services.msc)
) else (
    echo Service started successfully!
)

echo.
echo ========================================
echo Installation Complete!
echo ========================================
echo.
echo Installation directory: %INSTALL_DIR%
echo Service name: KTracker
echo.
echo To manage the service:
echo - Open Services: Press Win+R, type "services.msc"
echo - Find "KTracker Activity Tracker"
echo.
echo To uninstall, run: uninstall.bat
echo.
pause
