@echo off
REM KTracker Build Script for Windows
REM Prerequisites: Go, windres (from MinGW/MSYS2 or TDM-GCC)

setlocal EnableDelayedExpansion

echo ========================================
echo KTracker Build Script
echo ========================================
echo.

REM Set variables
set APP_NAME=KTracker
set VERSION=2.0.0
set ICON_DIR=icons

REM Check if Go is installed
where go >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo ERROR: Go is not installed or not in PATH
    echo Please install Go from https://golang.org/dl/
    exit /b 1
)

REM Get dependencies
echo Getting dependencies...
go mod tidy
if %ERRORLEVEL% neq 0 (
    echo WARNING: go mod tidy failed, continuing anyway...
)

REM Check if windres is available
where windres >nul 2>nul
if %ERRORLEVEL% neq 0 (
    echo WARNING: windres not found. Icon embedding will be skipped.
    echo To embed icon, install MinGW-w64 or TDM-GCC
    set SKIP_RESOURCES=1
) else (
    set SKIP_RESOURCES=0
)

REM Convert SVG to ICO if needed (manual step reminder)
if not exist "%ICON_DIR%\ktracker.ico" (
    echo.
    echo NOTE: ktracker.ico not found!
    echo Please convert icons\ktracker.svg to icons\ktracker.ico
    echo You can use online tools like:
    echo   - https://convertio.co/svg-ico/
    echo   - https://cloudconvert.com/svg-to-ico
    echo Or use ImageMagick: magick convert ktracker.svg -define icon:auto-resize=256,128,64,48,32,16 ktracker.ico
    echo.
)

REM Compile resources if windres is available and ICO exists
if %SKIP_RESOURCES%==0 (
    if exist "%ICON_DIR%\ktracker.ico" (
        echo Compiling Windows resources...
        cd cmd\tracker
        windres -o ktracker.syso ktracker.rc
        if %ERRORLEVEL% neq 0 (
            echo WARNING: Failed to compile resources. Building without icon...
        ) else (
            echo Resources compiled successfully!
        )
        cd ..\..
    )
)

REM Build the application
echo.
echo Building %APP_NAME% v%VERSION%...
echo.

REM Build with optimizations and without debug info (output to root folder)
go build -ldflags="-s -w -H windowsgui" -o "%APP_NAME%.exe" ./cmd/tracker

if %ERRORLEVEL% neq 0 (
    echo.
    echo ERROR: Build failed!
    exit /b 1
)

echo.
echo ========================================
echo Build Complete!
echo ========================================
echo.
echo Output: %APP_NAME%.exe
echo.
echo Next steps:
echo 1. Test the application: %APP_NAME%.exe
echo 2. Create installer: Open installer\ktracker_setup.iss with Inno Setup
echo.

endlocal
