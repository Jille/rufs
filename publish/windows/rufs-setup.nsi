!include LogicLib.nsh
!include Sections.nsh

Name "RUFS ${RUFS_VERSION}"

OutFile rufs-setup.exe
InstallDir "$ProgramFiles64\RUFS"

Page components
Page directory
Page instfiles

UninstPage uninstConfirm
UninstPage instfiles

Section "WinFSP" "winfsp"

SectionIn RO

GetTempFileName $R0
File /oname=$R0 winfsp-1.9.21096.msi
ExecWait 'msiexec /i "$R0" /passive '
Delete $R0

SectionEnd

Function .OnInit
  ; Check if WinFSP is installed
  ClearErrors
  ReadRegStr $0 HKLM "Software\WinFsp" "InstallDir"
  ${If} $0 == ""
    !insertmacro SelectSection ${winfsp}
  ${Else}
    !insertmacro UnselectSection ${winfsp}
  ${Endif}
FunctionEnd

Section "RUFS" "rufs"

SectionIn RO

SetOutPath $INSTDIR

# If you add files here, add them under Delete in the uninstaller as well
File rufs.exe

WriteUninstaller $INSTDIR\uninstall.exe
WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\RUFS" \
                 "DisplayName" "RUFS"
WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\RUFS" \
                 "Publisher" "RUFS"
WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\RUFS" \
                 "DisplayVersion" "${RUFS_VERSION}"
WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\RUFS" \
                 "InstallLocation" "$INSTDIR"
WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\RUFS" \
                 "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
WriteRegStr HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\RUFS" \
                 "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"

WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "RUFS" "$\"$INSTDIR\rufs.exe$\""

SectionEnd

Function .onInstSuccess
  SectionGetFlags "${winfsp}" $0
  ${If} $0 & ${SF_SELECTED}
    MessageBox MB_YESNO|MB_ICONQUESTION "Because WinFSP was installed, a reboot is required before using RUFS. Would you like to reboot now?" IDNO +2
    Reboot
  ${Else}
    # Launch rufs.exe without elevation
    Exec '"$WINDIR\explorer.exe" "$INSTDIR\rufs.exe"'
  ${EndIf}
FunctionEnd

Section "Uninstall"

DeleteRegKey HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\RUFS"
DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "RUFS"

Delete $INSTDIR\uninstall.exe
Delete $INSTDIR\rufs.exe
RMDir $INSTDIR

SectionEnd
