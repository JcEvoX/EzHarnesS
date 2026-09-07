@echo off
rem release: wails3 task package (icon/version info embedded, user-scope installer)
rem version is parsed from build\config.yml (info.version)
rem output: build\dist\<version>\ezharness.exe + bin\ezharness-amd64-installer-<version>.exe
setlocal
cd /d "%~dp0.."

set "VERSION="
for /f "tokens=2 delims=: " %%v in ('findstr /r /c:"^  version:" build\config.yml') do set "VERSION=%%~v"
if not defined VERSION (
    echo release.bat: cannot parse info.version from build\config.yml
    exit /b 1
)
echo version: %VERSION%

wails3 task package || goto :err

if not exist "build\dist\%VERSION%" mkdir "build\dist\%VERSION%"
move /Y "build\dist\ezharness.exe" "build\dist\%VERSION%\ezharness.exe" || goto :err
move /Y "bin\ezharness-amd64-installer.exe" "bin\ezharness-amd64-installer-%VERSION%.exe" || goto :err

echo.
echo exe:       build\dist\%VERSION%\ezharness.exe
echo installer: bin\ezharness-amd64-installer-%VERSION%.exe
goto :eof

:err
echo release.bat failed
exit /b 1
