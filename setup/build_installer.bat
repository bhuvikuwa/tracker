@echo off
REM Build script for DeskTime Tracker installer

echo ========================================
echo DeskTime Tracker - Build Installer
echo ========================================
echo.

REM Navigate to tracker directory
cd /d "%~dp0\.."

echo [1/3] Building tracker executable...
go build -ldflags="-s -w" -o tracker.exe ./cmd/tracker
if %errorLevel% neq 0 (
    echo ERROR: Failed to build tracker.exe
    pause
    exit /b 1
)
echo Build successful: tracker.exe

echo.
echo [2/3] Checking for Inno Setup...
set INNO_PATH=C:\Program Files (x86)\Inno Setup 6\ISCC.exe
if not exist "%INNO_PATH%" (
    echo Inno Setup not found at: %INNO_PATH%
    echo.
    echo Please install Inno Setup from: https://jrsoftware.org/isdl.php
    echo Or use the batch installer instead: setup\install.bat
    echo.
    pause
    exit /b 1
)

echo.
echo [3/3] Compiling installer with Inno Setup...
"%INNO_PATH%" "setup\installer.iss"
if %errorLevel% neq 0 (
    echo ERROR: Failed to compile installer
    pause
    exit /b 1
)

echo.
echo ========================================
echo Build Complete!
echo ========================================
echo.
echo Installer created: dist\DeskTime_Tracker_Setup.exe
echo.
echo You can now distribute this installer to users.
echo.
pause
