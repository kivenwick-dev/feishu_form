$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
Set-Location $projectRoot

function Read-SecretText {
    param([string]$Prompt)
    $secure = Read-Host $Prompt -AsSecureString
    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try {
        return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
    }
    finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
    }
}

if ([string]::IsNullOrWhiteSpace($env:FEISHU_APP_ID)) {
    $env:FEISHU_APP_ID = Read-Host "FEISHU_APP_ID"
}
if ([string]::IsNullOrWhiteSpace($env:FEISHU_APP_SECRET)) {
    $env:FEISHU_APP_SECRET = Read-SecretText "FEISHU_APP_SECRET"
}

$bundledWeb = Join-Path $scriptDir "feishu-web.exe"
$bundledProbe = Join-Path $scriptDir "feishu-probe.exe"
if ((Test-Path $bundledWeb) -and (Test-Path $bundledProbe)) {
    & $bundledWeb
    exit $LASTEXITCODE
}

if (-not (Get-Command go.exe -ErrorAction SilentlyContinue)) {
    throw "The Windows executables are incomplete and Go is not installed. Run windows\build.ps1 on a machine with Go."
}

& go run .\cmd\feishu-web
exit $LASTEXITCODE
