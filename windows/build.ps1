$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
Set-Location $projectRoot

if (-not (Get-Command go.exe -ErrorAction SilentlyContinue)) {
    throw "需要安装 Go 才能构建。"
}

if ($args.Count -eq 0) {
    go run .\cmd\build -targets windows/amd64
} else {
    go run .\cmd\build @args
}

$built = Join-Path $projectRoot "dist\windows-amd64\feishu-web.exe"
if (Test-Path $built) {
    Copy-Item $built (Join-Path $scriptDir "feishu-web.exe") -Force
    Write-Host "已复制到 $scriptDir\feishu-web.exe，可直接双击 start.bat 或该文件启动。"
}
