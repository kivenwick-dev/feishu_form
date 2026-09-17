$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$projectRoot = Split-Path -Parent $scriptDir
Set-Location $projectRoot

if (-not (Get-Command go.exe -ErrorAction SilentlyContinue)) {
    throw "需要安装 Go 才能构建。"
}

$builtWindowsExe = Join-Path $projectRoot "dist\windows-amd64\feishu-web.exe"

if ($args.Count -eq 0) {
    go run .\cmd\build -targets windows/amd64
    if ($LASTEXITCODE -ne 0) {
        throw "构建失败（退出码 $LASTEXITCODE）。"
    }
    if (Test-Path $builtWindowsExe) {
        Copy-Item $builtWindowsExe (Join-Path $scriptDir "feishu-web.exe") -Force
        Write-Host "已复制到 $scriptDir\feishu-web.exe，可直接双击 start.bat 或该文件启动。"
        $staleProbe = Join-Path $scriptDir "feishu-probe.exe"
        if (Test-Path $staleProbe) {
            Remove-Item $staleProbe -Force
            Write-Host "已移除旧的 feishu-probe.exe（改用单文件内置 probe）。"
        }
    }
} else {
    go run .\cmd\build @args
    if ($LASTEXITCODE -ne 0) {
        throw "构建失败（退出码 $LASTEXITCODE）。"
    }
}
