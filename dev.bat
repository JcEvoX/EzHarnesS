@echo off
rem dev: npm run build -> go build (frontend embedded) -> run exe
rem release: wails3 task build / wails3 package (icon, version info, installer)
setlocal
cd /d "%~dp0"

cd frontend
call npm run build || goto :err
cd ..

go build -o build\dist\ezharness.exe . || goto :err

set EZHARNESS_ROOT=%~dp0
build\dist\ezharness.exe
goto :eof

:err
echo dev.bat failed
exit /b 1
