$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
Set-Location $projectRoot

if (-not (Get-Command go.exe -ErrorAction SilentlyContinue)) {
    throw "Go is required to build the Windows executables."
}

$env:CGO_ENABLED = "0"
$env:GOOS = "windows"
if ([string]::IsNullOrWhiteSpace($env:GOARCH)) {
    $env:GOARCH = "amd64"
}

go build -trimpath -ldflags "-s -w" -o (Join-Path $scriptDir "feishu-web.exe") .\cmd\feishu-web
go build -trimpath -ldflags "-s -w" -o (Join-Path $scriptDir "feishu-probe.exe") .\cmd\feishu-probe

Write-Host "Built Windows $env:GOARCH binaries in $scriptDir"
