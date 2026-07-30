; ClipLocal Inno Setup Script
; Bundles: Go backend, Tauri UI, OBS portable, ffmpeg

#define MyAppName "ClipLocal"
#define MyAppVersion "0.1.0"
#define MyAppPublisher "ClipLocal"
#define MyAppURL "https://github.com/Krovikan-Vamp/ClipLocal"
#define MyAppExeName "cliplocal.exe"
#define MyTauriExeName "ClipLocal.exe"

[Setup]
AppId={{8F4A2B3C-1D2E-4F5A-8B9C-0D1E2F3A4B5C}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
AppSupportURL={#MyAppURL}
AppUpdatesURL={#MyAppURL}
DefaultDirName={autopf}\{#MyAppName}
DefaultGroupName={#MyAppName}
DisableProgramGroupPage=yes
LicenseFile=
OutputDir=..\dist
OutputBaseFilename=ClipLocal-Setup-{#MyAppVersion}
Compression=lzma2/ultra64
SolidCompression=yes
WizardStyle=modern
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "startupentry"; Description: "Start {#MyAppName} with Windows"; GroupDescription: "Windows integration:"; Flags: checked

[Files]
; Go backend binary
Source: "..\backend\cliplocal.exe"; DestDir: "{app}"; Flags: ignoreversion

; Tauri UI binary (compiled SolidStart assets embedded)
Source: "..\ui\src-tauri\target\release\{#MyTauriExeName}"; DestDir: "{app}"; Flags: ignoreversion

; OBS portable (bundled, pre-configured)
Source: "obs-portable\*"; DestDir: "{app}\obs-portable"; Flags: ignoreversion recursesubdirs createallsubdirs

; ffmpeg binary
Source: "ffmpeg\ffmpeg.exe"; DestDir: "{app}\ffmpeg"; Flags: ignoreversion

; Default config (will not overwrite existing)
Source: "..\config.example.yaml"; DestDir: "{app}"; DestName: "config.yaml"; Flags: onlyifdoesntexist

[Icons]
Name: "{group}\{#MyAppName}"; Filename: "{app}\{#MyTauriExeName}"
Name: "{group}\{cm:UninstallProgram,{#MyAppName}}"; Filename: "{uninstallexe}"
Name: "{commondesktop}\{#MyAppName}"; Filename: "{app}\{#MyTauriExeName}"; Tasks: 

[Run]
; Start the backend after installation
Filename: "{app}\{#MyAppExeName}"; Description: "{cm:LaunchProgram,{#StringChange(MyAppName, '&', '&&')}}"; Flags: nowait postinstall skipifsilent

[UninstallRun]
; Stop backend before uninstall
Filename: "taskkill"; Parameters: "/f /im {#MyAppExeName}"; Flags: runhidden waituntilterminated
Filename: "taskkill"; Parameters: "/f /im obs64.exe"; Flags: runhidden waituntilterminated

[Registry]
; Startup entry (optional, only if task selected)
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; \
  ValueName: "{#MyAppName}"; ValueData: """{app}\{#MyAppExeName}"""; \
  Flags: uninsdeletevalue; Tasks: startupentry

[Code]
procedure InitializeWizard;
begin
  // Future: detect GPU type for NVENC vs x264 recommendation
end;
