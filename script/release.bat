@echo off
rem release: wails3 task package (icon/version info embedded, user-scope installer)
rem output: build\dist\ezharness.exe + bin\ezharness-amd64-installer.exe
setlocal
cd /d "%~dp0.."

wails3 task package || goto :err

echo.
echo exe:       build\dist\ezharness.exe
echo installer: bin\ezharness-amd64-installer.exe
goto :eof

:err
echo release.bat failed
exit /b 1
