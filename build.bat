@echo off
setlocal
cd /d "%~dp0"

echo [1/3] Building frontend...
pushd frontend
call npm run build
if errorlevel 1 (
  popd
  echo.
  echo [x] Frontend build failed.
  pause
  exit /b 1
)
popd

echo [2/3] Building backend...
go build -tags release -o ezharness.exe .
if errorlevel 1 (
  echo.
  echo [x] Backend build failed.
  pause
  exit /b 1
)

echo [3/3] Starting ezharness...
start "" ezharness.exe
exit /b 0
