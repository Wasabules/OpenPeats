Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####
## Include the wails tools
####
!include "wails_tools.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer
# The application ships English and French; so does its installer. NSIS picks
# the one matching the Windows display language, English otherwise.
!insertmacro MUI_LANGUAGE "French"

# wails_tools.nsh only defines its messages when they are not defined yet:
# defining them here as language strings translates them too.
!define WAILS_WIN10_REQUIRED "$(WailsWin10Required)"
!define WAILS_ARCHITECTURE_NOT_SUPPORTED "$(WailsArchitectureNotSupported)"
!define WAILS_INSTALL_WEBVIEW_DETAILPRINT "$(WailsInstallWebView)"

LangString WailsWin10Required ${LANG_ENGLISH} "This product is only supported on Windows 10 (Server 2016) and later."
LangString WailsWin10Required ${LANG_FRENCH} "Ce produit nécessite Windows 10 (Server 2016) ou une version ultérieure."
LangString WailsArchitectureNotSupported ${LANG_ENGLISH} "This product can't be installed on the current Windows architecture. Supports: ${ARCH}"
LangString WailsArchitectureNotSupported ${LANG_FRENCH} "Ce produit ne peut pas être installé sur cette architecture Windows. Architectures prises en charge : ${ARCH}"
LangString WailsInstallWebView ${LANG_ENGLISH} "Installing: WebView2 Runtime"
LangString WailsInstallWebView ${LANG_FRENCH} "Installation : WebView2 Runtime"
LangString RemovingLegacy ${LANG_ENGLISH} "Removing the previous version, installed as PeatsAudio"
LangString RemovingLegacy ${LANG_FRENCH} "Suppression de la version précédente, installée sous le nom PeatsAudio"

# Up to 0.3.0 the desktop app was installed as "PeatsAudio" — the name of the
# manufacturer's own app — under this uninstall key. See RemoveLegacyInstall.
!define LEGACY_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\OpenPeatsPeatsAudio"

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}" # Default installing folder ($PROGRAMFILES is Program Files folder).
ShowInstDetails show # This will always show the installation details.

Function .onInit
   !insertmacro wails.checkArchitecture
FunctionEnd

# Runs the legacy uninstaller silently, so the renamed app does not sit beside
# its old copy in the Start menu and in Installed apps. `_?=` makes it run in
# place and wait; it cannot delete itself that way, so that is done here.
Function RemoveLegacyInstall
    SetRegView 64
    ReadRegStr $0 HKLM "${LEGACY_UNINST_KEY}" "UninstallString"
    StrCmp $0 "" done

    # The value is quoted: "C:\...\uninstall.exe"
    StrCpy $1 $0 1
    StrCmp $1 '"' 0 +3
        StrCpy $0 $0 "" 1
        StrCpy $0 $0 -1
    IfFileExists "$0" 0 done
    ${GetParent} "$0" $1

    DetailPrint "$(RemovingLegacy)"
    ExecWait '"$0" /S _?=$1'
    Delete "$0"
    RMDir "$1"
    DeleteRegKey HKLM "${LEGACY_UNINST_KEY}"
  done:
FunctionEnd

Section
    !insertmacro wails.setShellContext

    Call RemoveLegacyInstall

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
