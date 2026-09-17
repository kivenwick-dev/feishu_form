$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
Set-Location $projectRoot

$bundledWeb = Join-Path $scriptDir "feishu-web.exe"
if (Test-Path $bundledWeb) {
    & $bundledWeb
    exit $LASTEXITCODE
}

if (-not (Get-Command go.exe -ErrorAction SilentlyContinue)) {
    throw "未找到 feishu-web.exe，且未安装 Go。请在有 Go 的机器上运行 windows\build.ps1 生成可执行文件。"
}

& go run .\cmd\feishu-web
exit $LASTEXITCODE
