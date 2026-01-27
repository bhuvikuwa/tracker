; Inno Setup Script for DeskTime Tracker
; Download Inno Setup from: https://jrsoftware.org/isdl.php
; Compile this script with Inno Setup to create setup.exe

#define MyAppName "DeskTime Tracker"
#define MyAppVersion "1.0.0"
#define MyAppPublisher "DeskTime"
#define MyAppExeName "tracker.exe"
#define MyAppServiceName "KTracker"

[Setup]
; Basic application info
AppId={{A1B2C3D4-E5F6-7890-ABCD-EF1234567890}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={autopf}\DeskTime Tracker
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
; Privileges required to install as service
PrivilegesRequired=admin
PrivilegesRequiredOverridesAllowed=dialog
OutputDir=..\dist
OutputBaseFilename=DeskTime_Tracker_Setup
SetupIconFile=..\assets\icon.ico
Compression=lzma
SolidCompression=yes
WizardStyle=modern

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "autostart"; Description: "Start tracker automatically when Windows starts"; GroupDescription: "Additional options:"; Flags: checkedonce

[Files]
; Main executable
Source: "..\tracker.exe"; DestDir: "{app}"; Flags: ignoreversion

; Configuration files
Source: "..\config\config.yaml"; DestDir: "{app}\config"; Flags: ignoreversion

; Create directories for data
Source: "..\README.md"; DestDir: "{app}"; Flags: ignoreversion isreadme; DestName: "README.txt"

[Dirs]
Name: "{app}\screenshots"; Permissions: users-modify
Name: "{app}\logs"; Permissions: users-modify
Name: "{app}\config"; Permissions: users-modify

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyAppExeName}"
Name: "{group}\Uninstall {#MyAppName}"; Filename: "{uninstallexe}"

[Run]
; Install and start the service
Filename: "{app}\{#MyAppExeName}"; Parameters: "-install"; StatusMsg: "Installing Windows service..."; Flags: runhidden waituntilterminated
Filename: "sc"; Parameters: "start {#MyAppServiceName}"; StatusMsg: "Starting service..."; Flags: runhidden waituntilterminated; Tasks: autostart

[UninstallRun]
; Stop and uninstall the service
Filename: "sc"; Parameters: "stop {#MyAppServiceName}"; Flags: runhidden waituntilterminated
Filename: "{app}\{#MyAppExeName}"; Parameters: "-uninstall"; Flags: runhidden waituntilterminated

[Code]
function InitializeSetup(): Boolean;
var
  ResultCode: Integer;
begin
  // Check if service is already installed and stop it
  Exec('sc', 'query ' + '{#MyAppServiceName}', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  if ResultCode = 0 then
  begin
    // Service exists, stop it
    Exec('sc', 'stop ' + '{#MyAppServiceName}', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
    Sleep(2000); // Wait for service to stop
  end;
  Result := True;
end;

procedure CurStepChanged(CurStep: TSetupStep);
begin
  if CurStep = ssPostInstall then
  begin
    // Update config file with correct paths
    // This could be enhanced to modify the config.yaml with actual install paths
  end;
end;

function InitializeUninstall(): Boolean;
var
  ResultCode: Integer;
begin
  // Stop the service before uninstalling
  Exec('sc', 'stop ' + '{#MyAppServiceName}', '', SW_HIDE, ewWaitUntilTerminated, ResultCode);
  Sleep(2000);
  Result := True;
end;
