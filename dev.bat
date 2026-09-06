@echo off
rem 开发直跑：编译前端 -> 编译后端（内嵌前端产物）-> 启动 exe
rem 发布构建走 wails3 task build / wails3 package（图标、版本信息、安装包）。
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
echo dev.bat 失败
exit /b 1
