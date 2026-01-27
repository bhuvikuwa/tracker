@echo off
REM KTracker Installer Build Script
REM Prerequisites: Inno Setup 6 (https://jrsoftware.org/isinfo.php)

setlocal EnableDelayedExpansion

echo ========================================
echo KTracker Installer Build Script
echo ========================================
echo.

REM Check for Inno Setup
set ISCC_PATH=

REM Try common installation paths
if exist "C:\Program Files (x86)\Inno Setup 6\ISCC.exe" (
    set "ISCC_PATH=C:\Program Files (x86)\Inno Setup 6\ISCC.exe"
)
if exist "C:\Program Files\Inno Setup 6\ISCC.exe" (
    set "ISCC_PATH=C:\Program Files\Inno Setup 6\ISCC.exe"
)

if "%ISCC_PATH%"=="" (
    echo ERROR: Inno Setup 6 not found!
    echo.
    echo Please install Inno Setup from:
    echo https://jrsoftware.org/isdl.php
    echo.
    exit /b 1
)

REM Check if build exists
if not exist "dist\KTracker.exe" (
    echo ERROR: KTracker.exe not found in dist folder!
    echo.
    echo Please run build.bat first to create the executable.
    exit /b 1
)

REM Check if icon exists
if not exist "icons\ktracker.ico" (
    echo WARNING: ktracker.ico not found!
    echo The installer will be built without a custom icon.
    echo.
)

echo Building installer...
echo Using: %ISCC_PATH%
echo.

"%ISCC_PATH%" "installer\ktracker_setup.iss"

if %ERRORLEVEL% neq 0 (
    echo.
    echo ERROR: Installer build failed!
    exit /b 1
)

echo.
echo ========================================
echo Installer Build Complete!
echo ========================================
echo.
echo Installer created: dist\KTracker_Setup_1.0.0.exe
echo.

endlocal