$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
Set-Location $projectRoot

if (-not (Get-Command go.exe -ErrorAction SilentlyContinue)) {
    throw "需要安装 Go 才能构建。"
}

go run .\cmd\build @args
