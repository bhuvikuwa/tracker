@echo off
REM DeskTime Tracker Uninstaller Batch Script

echo ========================================
echo DeskTime Tracker Uninstaller
echo ========================================
echo.

REM Check for administrator privileges
net session >nul 2>&1
if %errorLevel% neq 0 (
    echo ERROR: This uninstaller requires Administrator privileges.
    echo Please right-click and select "Run as Administrator"
    echo.
    pause
    exit /b 1
)

set INSTALL_DIR=%ProgramFiles%\DeskTime Tracker

echo [1/4] Checking if service exists...
sc query KTracker >nul 2>&1
if %errorLevel% neq 0 (
    echo Service not found. May already be uninstalled.
) else (
    echo.
    echo [2/4] Stopping service...
    sc stop KTracker
    timeout /t 3 /nobreak >nul

    echo.
    echo [3/4] Uninstalling service...
    cd /d "%INSTALL_DIR%"
    tracker.exe -uninstall
    if %errorLevel% neq 0 (
        echo WARNING: Failed to uninstall service cleanly
        echo Attempting manual removal...
        sc delete KTracker
    )
)

echo.
echo [4/4] Removing files...
echo.
choice /C YN /M "Do you want to delete all tracker files including logs and screenshots"
if %errorLevel% equ 1 (
    echo Removing directory: %INSTALL_DIR%
    rd /s /q "%INSTALL_DIR%"
    echo Files removed.
) else (
    echo Keeping data files. You can manually delete: %INSTALL_DIR%
)

echo.
echo ========================================
echo Uninstallation Complete!
echo ========================================
echo.
pause
