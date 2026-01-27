; KTracker Inno Setup Script
; Download Inno Setup from: https://jrsoftware.org/isinfo.php

#define MyAppName "KTracker"
#define MyAppVersion "2.0.0"
#define MyAppPublisher "KuWare"
#define MyAppURL "https://desktime.kuware.com"
#define MyAppExeName "KTracker.exe"
#define MyAppMutex "Global\KTrackerSingleInstance"

[Setup]
; Application info
AppId={{8F3C5E2A-9D4B-4C1E-A8F7-6B2D9E4A5C3F}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}

; Installation directories
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes

; Output settings
OutputDir=..
OutputBaseFilename=KTracker_Setup_{#MyAppVersion}
SetupIconFile=..\icons\ktracker.ico
UninstallDisplayIcon={app}\{#MyAppExeName}

; Compression
Compression=lzma2/ultra64
SolidCompression=yes
LZMAUseSeparateProcess=yes

; UI settings
WizardStyle=modern

; Privileges (run as admin for proper installation)
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=dialog

; Minimum Windows version (Windows 7 SP1+)
MinVersion=6.1sp1

; Architecture
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible

; Single instance - check mutex to prevent install while app is running
AppMutex={#MyAppMutex}

; Close running applications
CloseApplications=force
CloseApplicationsFilter=*.exe
RestartApplications=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "{cm:CreateDesktopIcon}"; GroupDescription: "{cm:AdditionalIcons}"
Name: "startupicon"; Description: "Start KTracker automatically with Windows"; GroupDescription: "Startup options:"

[Files]
; Main executable (config and icons are embedded - no external files needed)
Source: "..\KTracker.exe"; DestDir: "{app}"; Flags: ignoreversion

[Dirs]
; Create AppData directory with proper permissions
Name: "{localappdata}\KTracker"; Permissions: users-modify

[Icons]
; Start menu
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"

; Desktop icon (checked by default)
Name: "{autodesktop}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: desktopicon

; Startup folder (runs at Windows startup)
Name: "{userstartup}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"; Tasks: startupicon

[Run]
; Option to launch after install
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent

[UninstallRun]
; Stop the application before uninstall
Filename: "taskkill"; Parameters: "/F /IM KTracker.exe"; Flags: runhidden; RunOnceId: "StopApp"

[UninstallDelete]
; Clean up AppData folder on uninstall (user data, logs, etc.)
Type: filesandordirs; Name: "{localappdata}\KTracker"

; Clean up any leftover files in install directory
Type: filesandordirs; Name: "{app}\logs"
Type: dirifempty; Name: "{app}"

[Code]
// Pascal script for custom actions

var
  AlreadyRunningMsg: String;

function IsAppRunning(): Boolean;
var
  ResultCode: Integer;
begin
  // Check if the process is running using tasklist
  Result := False;
  if Exec('cmd.exe', '/c tasklist /FI "IMAGENAME eq KTracker.exe" | find /i "KTracker.exe"',
          '', SW_HIDE, ewWaitUntilTerminated, ResultCode) then
  begin
    Result := (ResultCode = 0);
  end;
end;

function KillRunningApp(): Boolean;
var
  ResultCode: Integer;
begin
  Result := Exec('taskkill', '/F /IM KTracker.exe', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(1000); // Wait for process to terminate
end;

function InitializeSetup(): Boolean;
begin
  Result := True;
  AlreadyRunningMsg := 'KTracker is currently running. The installer will close it to continue.';

  // Check if app is running and kill it
  if IsAppRunning() then
  begin
    if MsgBox(AlreadyRunningMsg, mbConfirmation, MB_OKCANCEL) = IDOK then
    begin
      KillRunningApp();
    end
    else
    begin
      Result := False;
    end;
  end;
end;

procedure CurStepChanged(CurStep: TSetupStep);
var
  AppDataDir: String;
begin
  if CurStep = ssPostInstall then
  begin
    // Ensure AppData directory exists with proper structure
    AppDataDir := ExpandConstant('{localappdata}\KTracker');
    if not DirExists(AppDataDir) then
    begin
      CreateDir(AppDataDir);
    end;
  end;
end;

function InitializeUninstall(): Boolean;
var
  ResultCode: Integer;
begin
  Result := True;

  // Stop the application if running
  if IsAppRunning() then
  begin
    Exec('taskkill', '/F /IM KTracker.exe', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    Sleep(1000); // Wait for process to fully terminate
  end;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  AppDataDir: String;
begin
  if CurUninstallStep = usPostUninstall then
  begin
    // Remove AppData directory
    AppDataDir := ExpandConstant('{localappdata}\KTracker');
    if DirExists(AppDataDir) then
    begin
      DelTree(AppDataDir, True, True, True);
    end;
  end;
end;
