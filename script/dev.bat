@echo off
rem dev: npm run build -> go build (frontend embedded) -> run exe
rem release: script\release.bat (exe + installer via wails3 task)
setlocal
cd /d "%~dp0.."

cd frontend
call npm run build || goto :err
cd ..

go build -o build\dist\ezharness.exe . || goto :err

build\dist\ezharness.exe
goto :eof

:err
echo dev.bat failed
exit /b 1
