@echo off
setlocal
cd /d "%~dp0"
if not exist "feishu-web.exe" (
  echo 未找到 feishu-web.exe。请先运行 build.ps1 生成，或在项目根目录运行 go run ./cmd/feishu-web。
  pause
  exit /b 1
)
"feishu-web.exe"
if errorlevel 1 pause
endlocal
